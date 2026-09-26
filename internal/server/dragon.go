package server

import (
	"encoding/binary"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"log"
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The ender dragon fight. The dragon is a hub-driven flyer (it skips the
// standard grounded mob physics): it circles the pillar ring, periodically
// swoops at a player, and heals while any end crystal survives. Crystals sit
// on the pillar tops and detonate when hit. Killing the dragon showers XP,
// opens the exit portal (with the egg on the first kill) and a gateway.

const (
	dragonHealth  = 200
	dragonContact = 10 // EnderDragon.hurt: the head and neck
	// The wings reach further and hit softer, but they throw you (knockBack).
	dragonWingDamage = 5
	dragonWingPush   = 4.0
	// dragonCrystalReach is how far past its 16×8 box the dragon looks for a
	// crystal (checkCrystals: the bounding box inflated by 32).
	dragonCrystalReach = 32.0
	// dragonCrystalLoss is what the dragon takes when the crystal healing it
	// is destroyed (onCrystalDestroyed: 10 to the head).
	dragonCrystalLoss = 10
)

var (
	entityEnderDragon    = entityID("ender_dragon")
	entityEndCrystal     = entityID("end_crystal")
	entityDragonFireball = entityID("dragon_fireball")

	itemElytra = itemByName["elytra"]
)

type crystal struct {
	eid     int32
	uuid    [16]byte
	dim     int // placed crystals live in any dimension; the fight's are the End's
	x, y, z float64
}

// enterEnd is called when a player arrives in dim 2: first arrival stages
// the fight (unless the dragon is already defeated).
func (h *hub) enterEnd(players map[int32]*tracked, arriving *tracked) {
	if h.rules.DragonDefeated || h.dragon != nil || h.end == nil {
		return
	}
	// Built by hand (not spawnMobIn): its radius broadcast could reach the
	// arriving player, whose copy must come from the view swap alone.
	eid := h.allocEID()
	h.reserveDragonPartEIDs()
	m := &mob{eid: eid, etype: entityEnderDragon, dim: 2, health: dragonHealth,
		behavior: idleBehavior{},
		x:        0.5, y: float64(worldgen.EndSurfaceY + 25), z: 0.5,
		sx: 0.5, sy: float64(worldgen.EndSurfaceY + 25), sz: 0.5}
	binary.BigEndian.PutUint32(m.uuid[12:], uint32(eid))
	h.mobs[eid] = m
	h.gridDirty()
	m.hostile = true
	m.health = dragonHealth
	if left := h.rules.DragonHealth; left > 0 && left < dragonHealth {
		m.health = left // resume a fight a restart interrupted
	}
	m.behavior = idleBehavior{} // movement is updateDragon's, not steer()'s
	h.dragon = m
	h.setDragonPhase(nil, m, phaseHoldingPattern) // EndDragonFight.createNewDragon
	m.dragon().lastHealth = m.health
	// spawnMobIn's broadcast is radius-culled; the boss must reach the whole
	// island. Skip the arriving player: the dimension-switch view swap sends
	// them every dim-2 entity exactly once — a duplicate spawn for the same
	// entity id is undefined client behavior.
	for _, t := range players {
		if t.dim == 2 && t != arriving {
			t.p.trySendEv(entAdd(m.eid, m.etype, m.uuid, m.x, m.y, m.z, 0, 0))
			t.p.trySendEv(metaEv(dragonPhaseMeta(m.eid, m.dragon().phase)))
		}
	}
	log.Printf("end: dragon staged eid=%d at (%.0f,%.0f,%.0f)", m.eid, m.x, m.y, m.z)
	for i := 0; i < worldgen.EndPillars; i++ {
		px := worldgen.EndPillarRing * cosTurn(float64(i)/worldgen.EndPillars)
		pz := worldgen.EndPillarRing * sinTurn(float64(i)/worldgen.EndPillars)
		c := &crystal{eid: h.allocEID(), dim: dimEnd, x: px + 0.5, y: float64(worldgen.EndPillarTop(i)), z: pz + 0.5}
		binary.BigEndian.PutUint32(c.uuid[12:], uint32(c.eid))
		h.crystals[c.eid] = c
		for _, t := range players {
			if t.dim == 2 && t != arriving {
				t.p.trySendEv(entAdd(c.eid, entityEndCrystal, c.uuid, c.x, c.y, c.z, 0, 0))
			}
		}
	}
}

// updateDragon is EnderDragon.aiStep, run every tick: the crystals, the
// flight record, the phase's tick, the flight toward the phase's target,
// the parts' contact (the wings shove and nick, the head and neck bite), the
// terrain the head, neck and body plough through, and the broadcast. A dead
// dragon runs tickDeath instead.
func (h *hub) updateDragon(players map[int32]*tracked) {
	m := h.dragon
	if m == nil {
		return
	}
	d := m.dragon()
	if d.dead || m.health <= 0 {
		h.dragonTickDeath(players, m)
		return
	}
	// Mirror the fight's progress into the settings the world saves, so a
	// restart resumes where the fight got to rather than healing the dragon.
	h.rules.DragonHealth = m.health
	now := h.tick.Load()
	// A blow taken while sitting counts toward sittingDamageReceived: past a
	// quarter of its health it takes off (EnderDragon.hurt).
	if d.lastHealth > m.health && dragonSitting(d.phase) {
		if d.sitDamage += float64(d.lastHealth - m.health); d.sitDamage > 0.25*dragonHealth {
			d.sitDamage = 0
			h.setDragonPhase(players, m, phaseTakeoff)
		}
	}
	h.dragonCheckCrystals(m, now)
	m.yaw = float32(wrapDegrees(float64(m.yaw)))
	m.recordDragonFlight()
	before := d.phase
	h.dragonPhaseTick(players, m)
	if d.phase != before {
		h.dragonPhaseTick(players, m)
	}
	if d.hasTarget {
		h.dragonFly(m, d)
	}
	sitting := dragonSitting(d.phase)
	if !(m.invulnTicks > 10) { // !wasHurtRecently
		h.dragonContact(players, m, sitting, now)
	}
	h.dragonInWall = h.dragonCheckWalls(players, m, sitting)
	d.lastHealth = m.health
	if now%200 == 0 { // ~10s heartbeat while the fight is being debugged
		log.Printf("end: dragon at (%.1f,%.1f,%.1f) hp=%d phase=%d", m.x, m.y, m.z, m.health, d.phase)
	}
	h.dragonBroadcast(players, m)
}

// dragonFly is aiStep's flight toward the phase's target: climb or dive
// toward it within the phase's speed, turn toward it with the turn rate
// easing, thrust along the facing (weaker when facing away), move (slowed
// in a wall; the dragon passes through terrain, noPhysics), and slide.
func (h *hub) dragonFly(m *mob, d *dragonState) {
	xdd, ydd, zdd := d.target[0]-m.x, d.target[1]-m.y, d.target[2]-m.z
	distSq := xdd*xdd + ydd*ydd + zdd*zdd
	maxS := dragonFlySpeed(d.phase)
	if hd := math.Sqrt(xdd*xdd + zdd*zdd); hd > 0 {
		ydd = clampF(ydd/hd, -maxS, maxS)
	}
	m.vy += ydd * 0.01
	m.yaw = float32(wrapDegrees(float64(m.yaw)))
	ax, ay, az := d.target[0]-m.x, d.target[1]-m.y, d.target[2]-m.z
	if l := math.Sqrt(ax*ax + ay*ay + az*az); l > 1e-4 {
		ax, ay, az = ax/l, ay/l, az/l
	} else {
		ax, ay, az = 0, 0, 0
	}
	yaw := float64(m.yaw) * math.Pi / 180
	dx, dy, dz := math.Sin(yaw), m.vy, -math.Cos(yaw)
	if l := math.Sqrt(dx*dx + dy*dy + dz*dz); l > 1e-4 {
		dx, dy, dz = dx/l, dy/l, dz/l
	}
	dot := math.Max((dx*ax+dy*ay+dz*az+0.5)/1.5, 0)
	if math.Abs(xdd) > 1e-5 || math.Abs(zdd) > 1e-5 {
		yRotD := clampF(wrapDegrees(180-math.Atan2(xdd, zdd)*180/math.Pi-float64(m.yaw)), -50, 50)
		d.yRotA *= 0.8
		d.yRotA += yRotD * dragonTurnSpeed(m, d.phase)
		m.yaw += float32(d.yRotA * 0.1)
	}
	span := 2 / (distSq + 1)
	speed := 0.06 * (dot*span + (1 - span))
	yaw = float64(m.yaw) * math.Pi / 180
	m.vx += speed * math.Sin(yaw) // moveRelative(speed, (0, 0, -1))
	m.vz -= speed * math.Cos(yaw)
	scale := 1.0
	if h.dragonInWall {
		scale = 0.8
	}
	m.x += m.vx * scale
	m.y += m.vy * scale
	m.z += m.vz * scale
	if l := math.Sqrt(m.vx*m.vx + m.vy*m.vy + m.vz*m.vz); l > 1e-4 {
		slide := 0.8 + 0.15*((m.vx*dx+m.vy*dy+m.vz*dz)/l+1)/2
		m.vx, m.vz = m.vx*slide, m.vz*slide
	}
	m.vy *= 0.91
}

// dragonCheckCrystals is checkCrystals: a point of health every ten ticks
// from the crystal it is bound to, and one tick in ten it looks again for
// the nearest crystal about it.
func (h *hub) dragonCheckCrystals(m *mob, now uint64) {
	if h.dragonCrystal != 0 {
		if h.crystals[h.dragonCrystal] == nil {
			h.dragonCrystal = 0
		} else if now%10 == 0 && m.health < dragonHealth {
			m.health++
		}
	}
	if h.rng.Intn(10) == 0 {
		h.dragonCrystal = h.nearestDragonCrystal(m)
	}
}

// dragonContact is aiStep's part contact: any living thing inside a wing's
// box grown four across and two up, and lowered two, is shoved away from the
// body (and, unless the dragon sits, takes five); anything in the head's or
// neck's box grown by one takes ten. knockBack and hurt take every
// LivingEntity the boxes touch — players, other mobs, armor stands — and
// nothing else, so a painting or an item frame is never struck.
func (h *hub) dragonContact(players map[int32]*tracked, m *mob, sitting bool, now uint64) {
	parts := dragonPartsOf(m)
	body := parts[2]
	touches := func(x, y, z, hw, ht float64, p dragonPart, gx, gyDown, gyUp float64) bool {
		minX, maxX := p.x-p.w/2-gx, p.x+p.w/2+gx
		minZ, maxZ := p.z-p.w/2-gx, p.z+p.w/2+gx
		minY, maxY := p.y-gyDown, p.y+p.h+gyUp
		return x+hw > minX && x-hw < maxX && z+hw > minZ && z-hw < maxZ && y+ht > minY && y < maxY
	}
	inBox := func(t *tracked, p dragonPart, gx, gyDown, gyUp float64) bool {
		return touches(t.x, t.y, t.z, 0.3, 1.8, p, gx, gyDown, gyUp)
	}
	h.dragonContactMobs(players, m, parts, sitting, touches)
	for _, t := range players {
		if t.dim != m.dim || t.dead || !isSurvival(t.gamemode) || now < t.graceUntil {
			continue
		}
		for _, wing := range parts[6:8] {
			if !inBox(t, wing, 4, 4, 0) { // inflate(4, 2, 4).move(0, -2, 0)
				continue
			}
			xd, zd := t.x-body.x, t.z-body.z
			dd := math.Max(xd*xd+zd*zd, 0.1)
			t.p.trySendEv(attachproto.Velocity{EID: t.p.eid, VX: xd / dd * dragonWingPush, VY: 0.2, VZ: zd / dd * dragonWingPush})
			t.spinUntil = now + windBurstGrace
			if !sitting {
				h.hurtFrom(players, t, dragonWingDamage, dtMobAttack, deathCause{by: "Ender Dragon"}, fromMob(m.x, m.z))
			}
		}
		for _, p := range parts[0:2] {
			if inBox(t, p, 1, 1, 1) {
				if h.hurtFrom(players, t, dragonContact, dtMobAttack, deathCause{by: "Ender Dragon"}, fromMob(m.x, m.z)) {
					h.knockback(t, m.x, m.z)
				}
			}
		}
	}
}

// dragonContactMobs is dragonContact for the mobs: a wing pushes one away
// from the body (Entity.push adds to its motion) and, unless the dragon
// sits, hurts it for five; the head and neck hurt it for ten. The other
// living things the boxes can touch take nothing: an armor stand refuses a
// mob's attack (mob_attack is not #can_break_armor_stand), and a painting or
// an item frame is no LivingEntity.
func (h *hub) dragonContactMobs(players map[int32]*tracked, m *mob, parts []dragonPart, sitting bool,
	touches func(x, y, z, hw, ht float64, p dragonPart, gx, gyDown, gyUp float64) bool) {
	body := parts[2]
	type blow struct {
		o   *mob
		dmg float64
	}
	var blows []blow
	for _, o := range h.mobs {
		if o == m || o.dim != m.dim || o.dying > 0 || o.health <= 0 {
			continue
		}
		b := o.box()
		for _, wing := range parts[6:8] {
			if !touches(o.x, o.y, o.z, b.w/2, b.h, wing, 4, 4, 0) {
				continue
			}
			xd, zd := o.x-body.x, o.z-body.z
			dd := math.Max(xd*xd+zd*zd, 0.1)
			o.vx += xd / dd * dragonWingPush
			o.vy += 0.2
			o.vz += zd / dd * dragonWingPush
			if !sitting {
				blows = append(blows, blow{o, dragonWingDamage})
			}
		}
		for _, p := range parts[0:2] {
			if touches(o.x, o.y, o.z, b.w/2, b.h, p, 1, 1, 1) {
				blows = append(blows, blow{o, dragonContact})
			}
		}
	}
	// Hurt after the scan: a kill or a conversion changes h.mobs. The hurt
	// cooldown (invulnerableTime) keeps a mob caught every tick from taking
	// every blow.
	for _, bl := range blows {
		if h.mobs[bl.o.eid] == bl.o && bl.o.dying == 0 {
			h.hurtMobOf(players, bl.o, bl.dmg, dtMobAttack)
		}
	}
}

// dragonBroadcast sends the flight: the same relative-move packets every
// other mob uses (the only entity-movement encoding real clients have
// verified — sync_entity_position was our sole use of that packet and 26.x
// clients never rendered a dragon driven by it). NoSync carries that
// constraint into the event: renderers stay relative forever, saturating +
// converging on any oversized delta.
func (h *hub) dragonBroadcast(players map[int32]*tracked, m *mob) {
	if m.x != m.sx || m.y != m.sy || m.z != m.sz || m.yaw != m.syaw {
		m.sx, m.sy, m.sz, m.syaw = m.x, m.y, m.z, m.yaw
		mv := entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, false)
		mv.NoSync = true
		h.toDimEv(players, m.dim, mv)
	}
}

// dragonKillingBlow is die() for the dragon: the kill is credited at once,
// then handleKillingBlow — a dragon in the air is held at one health and
// flies home to die (DYING); a sitting one dies where it sits.
func (h *hub) dragonKillingBlow(players map[int32]*tracked, m *mob) {
	d := m.dragon()
	if d.dead || d.phase == phaseDying {
		return
	}
	h.settleKillCredit(players, m)
	if !dragonSitting(d.phase) {
		m.health = 1
		h.setDragonPhase(players, m, phaseDying)
		return
	}
	h.dragonStartDeath(players, m)
}

// dragonStartDeath is the moment the dragon's health is gone: the client's
// death sequence starts (entity event 3), and tickDeath runs from here.
func (h *hub) dragonStartDeath(players map[int32]*tracked, m *mob) {
	d := m.dragon()
	if d.dead {
		return
	}
	d.dead, m.health = true, 0
	m.dying = 1 << 30 // not a target for anything any more; updateDragon ends it
	h.toDimEv(players, m.dim, entityStatus(m.eid, entityStatusDeath))
}

// dragonTickDeath is EnderDragon.tickDeath: the death is heard across the
// End (level event 1028); it rises a tenth of a block a tick; from the
// 155th tick eight percent of its experience falls every five ticks, and at
// the 200th the last fifth, the fight is won and it is gone.
func (h *hub) dragonTickDeath(players map[int32]*tracked, m *mob) {
	d := m.dragon()
	if !d.dead {
		h.dragonStartDeath(players, m)
	}
	d.deathTime++
	xp := 500
	if h.endGatewaysOpen() == 0 { // !hasPreviouslyKilledDragon
		xp = 12000
	}
	if d.deathTime > 150 && d.deathTime%5 == 0 && h.rules.DoMobLoot {
		h.spawnXPOrbIn(players, m.dim, int(float64(xp)*0.08), m.x, m.y, m.z)
	}
	if d.deathTime == 1 {
		h.playSoundGlobal(players, dimEnd, "minecraft:entity.ender_dragon.death", sndHostile, m.x, m.y, m.z, 5, 1)
	}
	m.y += 0.1
	h.dragonBroadcast(players, m)
	if d.deathTime >= 200 {
		if h.rules.DoMobLoot {
			h.spawnXPOrbIn(players, m.dim, int(float64(xp)*0.2), m.x, m.y, m.z)
		}
		h.despawnMob(players, m) // setDragonKilled + remove
	}
}

// hitCrystal detonates an end crystal struck by a player (EndCrystal.
// hurtServer): a power-6 blast that breaks blocks, hurts everything in reach
// as any explosion does, and is the player's — explosion(crystal, player) —
// so what it kills is theirs.
func (h *hub) hitCrystal(players map[int32]*tracked, eid int32, by *tracked) bool {
	c := h.crystals[eid]
	if c == nil {
		return false
	}
	delete(h.crystals, eid)
	h.toDimEv(players, c.dim, entGone(eid))
	name, cause := "", func(*blastCfg) {}
	if by != nil {
		name, cause = h.blastCauseOf(players, by.p.eid)
	}
	h.explodeBy(players, c.dim, c.x, c.y, c.z, endCrystalBlastPower, endCrystalBlastPower, blastBlock, name,
		withBlastDirect(entityEndCrystal), cause)
	h.dragonCrystalDestroyed(players, c, by) // onDestroyedBy, after the blast
	return true
}

// nearestDragonCrystal is the closest crystal inside the dragon's box grown
// by 32 blocks (0 when there is none).
func (h *hub) nearestDragonCrystal(m *mob) int32 {
	best, bestD := int32(0), math.MaxFloat64
	for eid, c := range h.crystals {
		if c.dim != m.dim || math.Abs(c.x-m.x) > 8+dragonCrystalReach || math.Abs(c.z-m.z) > 8+dragonCrystalReach ||
			c.y < m.y-dragonCrystalReach || c.y > m.y+8+dragonCrystalReach {
			continue
		}
		if d := dist3sq(c.x, c.y, c.z, m.x, m.y, m.z); d < bestD {
			best, bestD = eid, d
		}
	}
	return best
}

// dragonCrystalDestroyed is EnderDragon.onCrystalDestroyed: losing the
// crystal it is healing from costs the dragon 10 to the head, an explosion
// that is the destroying player's — or, when no player destroyed it, the
// nearest player's within 64.
func (h *hub) dragonCrystalDestroyed(players map[int32]*tracked, c *crystal, by *tracked) {
	m := h.dragon
	if m == nil || m.dying > 0 {
		return
	}
	if c.eid != h.dragonCrystal {
		if by != nil && m.dragon().phase == phaseHoldingPattern {
			h.dragonStrafe(players, m, by) // onCrystalDestroyed, whichever crystal
		}
		return
	}
	h.dragonCrystal = 0
	if by == nil {
		best := 64.0 * 64.0
		for _, t := range players {
			if t.dim != dimEnd || t.dead || !isSurvival(t.gamemode) {
				continue
			}
			if d := dist3sq(t.x, t.y, t.z, c.x, c.y, c.z); d <= best {
				by, best = t, d
			}
		}
	}
	dt := dtExplosion
	if by != nil {
		dt = dtPlayerExplosion
		h.hurtByPlayerOn(m, by)
		m.lastAttacker = by.p.eid
	}
	m.dragonMeleePart = "head" // hurt(level, this.head, …)
	m.hurtKind(dragonCrystalLoss, dt)
	m.dragonMeleePart = ""
	m.lastDirect = entityEndCrystal
	h.mobDamageEv(players, m, dtExplosion, 0) // the crystal blast
	if m.health <= 0 {
		h.killMob(players, m)
		return
	}
	// DragonHoldingPatternPhase.onCrystalDestroyed: it turns on the player
	// who did it.
	if by != nil && m.dragon().phase == phaseHoldingPattern {
		h.dragonStrafe(players, m, by)
	}
}

// endCrystalBlastPower is EndCrystal's level.explode radius.
const endCrystalBlastPower = 6

// dragonDefeated: XP shower, exit portal, the first kill's egg, a gateway.
func (h *hub) dragonDefeated(players map[int32]*tracked) {
	h.dragon = nil
	h.rules.DragonDefeated = true
	h.rules.DragonHealth = 0 // the fight is over; nothing left to resume
	h.saveRules()
	cx, cy := 0, worldgen.EndSurfaceY
	for h.end.At(cx, cy, 0) != worldgen.Air && cy < worldgen.EndSurfaceY+8 {
		cy++
	}
	// Exit portal: a bedrock dais with end-portal blocks and the egg on top.
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			if dx*dx+dz*dz > 5 {
				continue
			}
			h.setBlockIn(players, 2, blockPos{dx, cy - 1, dz}, worldgen.Bedrock)
			if dx*dx+dz*dz <= 2 && !(dx == 0 && dz == 0) {
				h.setBlockIn(players, 2, blockPos{dx, cy, dz}, worldgen.EndPortalBlock)
			}
		}
	}
	h.setBlockIn(players, 2, blockPos{0, cy, 0}, worldgen.Bedrock)
	// EndDragonFight.setDragonKilled / EnderDragon.tickDeath: every kill
	// opens a gateway, so a standing one means the dragon fell before. The
	// egg comes only the first time, and the XP is 12000 then, 500 after.
	// There is no elytra: that is the End ships' loot.
	// The experience fell during tickDeath.
	if h.endGatewaysOpen() == 0 {
		h.setBlockIn(players, 2, blockPos{0, cy + 1, 0}, worldgen.DragonEgg)
	}
	h.spawnNextEndGateway(players)
	for _, t := range players {
		t.p.trySendEv(chatEv("The Ender Dragon has fallen!"))
	}
}

// setBlockIn writes + broadcasts a block in an explicit dimension.
func (h *hub) setBlockIn(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	h.worldFor(dim).SetBlock(pos.x, pos.y, pos.z, state)
	body := blockSetEv(pos.x, pos.y, pos.z, state)
	for _, t := range players {
		if t.dim == dim {
			t.p.trySendEv(body)
		}
	}
}

// cosTurn/sinTurn: real trig on turn fractions (worldgen's approx is private).
func cosTurn(t float64) float64 { return math.Cos(2 * math.Pi * t) }
func sinTurn(t float64) float64 { return math.Sin(2 * math.Pi * t) }

// evEndRefresh re-sends the End's boss entities to one player (/refresh).
type evEndRefresh struct{ eid int32 }

func (evEndRefresh) isHubEvent() {}

func (h *hub) onEndRefresh(players map[int32]*tracked, eid int32) {
	t := players[eid]
	if t == nil || t.dim != 2 {
		return
	}
	if m := h.dragon; m != nil {
		t.p.sendEv(entGone(m.eid))
		t.p.sendEv(entAdd(m.eid, m.etype, m.uuid, m.x, m.y, m.z, m.yaw, 0))
		t.p.sendEv(metaEv(dragonPhaseMeta(m.eid, m.dragon().phase)))
	}
	for _, c := range h.crystals {
		if c.dim != dimEnd {
			continue
		}
		t.p.sendEv(entGone(c.eid))
		t.p.sendEv(entAdd(c.eid, entityEndCrystal, c.uuid, c.x, c.y, c.z, 0, 0))
	}
}
