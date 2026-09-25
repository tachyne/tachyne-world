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
	dragonSpeed   = 0.7
	dragonContact = 10 // EnderDragon.hurt: the body and head
	// The wings reach further and hit softer, but they throw you (knockBack).
	dragonBodyReach  = 4.0
	dragonWingReach  = 8.0
	dragonWingDamage = 5
	dragonWingPush   = 4.0
	dragonHealRate   = 2 // HP/s while it has a crystal to heal from (one every 10 ticks)
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
	// spawnMobIn's broadcast is radius-culled; the boss must reach the whole
	// island. Skip the arriving player: the dimension-switch view swap sends
	// them every dim-2 entity exactly once — a duplicate spawn for the same
	// entity id is undefined client behavior.
	for _, t := range players {
		if t.dim == 2 && t != arriving {
			t.p.trySendEv(entAdd(m.eid, m.etype, m.uuid, m.x, m.y, m.z, 0, 0))
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

// updateDragon flies the circuit, swoops, heals, and bites.
func (h *hub) updateDragon(players map[int32]*tracked) {
	m := h.dragon
	if m == nil || m.dying > 0 {
		return
	}
	// Mirror the fight's progress into the settings the world saves, so a
	// restart resumes where the fight got to rather than healing the dragon.
	h.rules.DragonHealth = m.health
	now := h.tick.Load()
	// The phase machine picks where it is going and whether it is sitting;
	// the movement below is unchanged, it just follows a smarter target.
	tx, ty, tz, sitting := h.updateDragonPhase(players, m)
	speed := dragonSpeed
	if sitting {
		speed = 0 // perched and breathing: it stays put
	}
	dx, dy, dz := tx-m.x, ty-m.y, tz-m.z
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if h.dragonInWall {
		speed *= 0.8 // EnderDragon.aiStep: move(deltaMovement × 0.8) while inWall
	}
	if d > 1e-6 && speed > 0 {
		step := math.Min(speed, d)
		m.x += dx / d * step
		m.y += dy / d * step
		m.z += dz / d * step
		m.yaw = float32(math.Atan2(dz, dx)*180/math.Pi) - 90
	}
	h.dragonInWall = h.dragonCheckWalls(players, m, sitting)
	m.recordDragonFlight() // DragonFlightHistory: where the tail follows
	// Contact damage to End players in reach — only while it is flying. A
	// perched dragon is the fight's one safe window to hit its head, so it
	// must not still be grinding anyone who stands next to it. Vanilla has
	// two contacts: the body and head deal 10, and the WINGS deal 5 with a
	// hard sideways shove (EnderDragon.hurt and knockBack).
	for _, t := range players {
		if sitting {
			break
		}
		if t.dim != 2 || t.dead || !isSurvival(t.gamemode) || now < t.graceUntil {
			continue
		}
		d := dist3(t.x, t.y, t.z, m.x, m.y, m.z)
		switch {
		case d < dragonBodyReach:
			h.hurtFrom(players, t, dragonContact, dtMobAttack,
				deathCause{by: "Ender Dragon"}, fromMob(m.x, m.z))
			h.knockback(t, m.x, m.z)
		case d < dragonWingReach:
			// knockBack: push = (dx/d², 0.2, dz/d²)·4, then five damage.
			dx, dz := t.x-m.x, t.z-m.z
			d2 := math.Max(dx*dx+dz*dz, 0.1)
			t.p.trySendEv(attachproto.Velocity{EID: t.p.eid,
				VX: dx / d2 * dragonWingPush, VY: 0.2, VZ: dz / d2 * dragonWingPush})
			t.spinUntil = now + windBurstGrace
			h.hurtFrom(players, t, dragonWingDamage, dtMobAttack,
				deathCause{by: "Ender Dragon"}, fromMob(m.x, m.z))
		}
	}
	// Crystal healing (checkCrystals): only from its nearest crystal, one
	// point every 10 ticks — this runs once a second, so two at a time —
	// and it looks again for the nearest crystal around it.
	if h.crystals[h.dragonCrystal] == nil {
		h.dragonCrystal = 0
	} else if now%20 == 0 && m.health < dragonHealth {
		m.health = min(dragonHealth, m.health+dragonHealRate)
	}
	h.dragonCrystal = h.nearestDragonCrystal(m)
	if now%200 == 0 { // ~10s heartbeat while the fight is being debugged
		log.Printf("end: dragon at (%.1f,%.1f,%.1f) hp=%d players-in-end=%d", m.x, m.y, m.z, m.health, func() (n int) {
			for _, t := range players {
				if t.dim == 2 {
					n++
				}
			}
			return
		}())
	}
	// Broadcast: the same relative-move packets every other mob uses (the only
	// entity-movement encoding real clients have verified — sync_entity_position
	// was our sole use of that packet and 26.x clients never rendered a dragon
	// driven by it). NoSync carries that constraint into the event: renderers
	// stay relative forever, saturating + converging on any oversized delta.
	if m.x != m.sx || m.y != m.sy || m.z != m.sz {
		m.sx, m.sy, m.sz = m.x, m.y, m.z
		mv := entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, false)
		mv.NoSync = true
		h.toDimEv(players, 2, mv)
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
	if m == nil || m.dying > 0 || c.eid != h.dragonCrystal {
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
	m.hurtKind(dragonCrystalLoss, dt)
	m.lastDirect = entityEndCrystal
	h.mobDamageEv(players, m, dtExplosion, 0) // the crystal blast
	if m.health <= 0 {
		h.killMob(players, m)
	}
}

// endCrystalBlastPower is EndCrystal's level.explode radius.
const endCrystalBlastPower = 6

// dragonDefeated: XP shower, exit portal, the first kill's egg, a gateway.
func (h *hub) dragonDefeated(players map[int32]*tracked) {
	if m := h.dragon; m != nil {
		// EnderDragon.tickDeath: level event 1028, heard across the End (and
		// everywhere with global_sound_events on).
		h.playSoundGlobal(players, dimEnd, "minecraft:entity.ender_dragon.death", sndHostile, m.x, m.y, m.z, 5, 1)
	}
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
	firstKill := h.endGatewaysOpen() == 0
	xp := 500
	if firstKill {
		h.setBlockIn(players, 2, blockPos{0, cy + 1, 0}, worldgen.DragonEgg)
		xp = 12000
	}
	h.spawnXPOrbIn(players, 2, xp, 0.5, float64(cy+1), 0.5)
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
	}
	for _, c := range h.crystals {
		if c.dim != dimEnd {
			continue
		}
		t.p.sendEv(entGone(c.eid))
		t.p.sendEv(entAdd(c.eid, entityEndCrystal, c.uuid, c.x, c.y, c.z, 0, 0))
	}
}
