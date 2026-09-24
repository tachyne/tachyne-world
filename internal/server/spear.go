package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
	"github.com/tachyne/tachyne-world/plugin"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// The spear (Item.Properties.spear). Two attacks, neither of them the
// ordinary swing:
//
//   - The STAB (PiercingWeapon): a left-click is a jab that hits EVERY entity
//     on a short line in front of the eyes, from 2 to 4.5 blocks out (6.5 in
//     creative), for the weapon's modest attack damage. It needs a full
//     charge (MINIMUM_ATTACK_CHARGE 1.0, five ticks of tolerance) and resets
//     the cooldown like a swing. The client sends it as the STAB player
//     action; an attack on an entity with a spear in hand is taken as the
//     same jab (see spearStab).
//
//   - The CHARGE (KineticWeapon): holding use lowers the spear. After the
//     weapon's delay, every tick the same line is scanned, and anything on it
//     not struck in the last ten ticks is tested against three conditions,
//     each with its own time window: DISMOUNT and KNOCKBACK need the wielder
//     moving along their look fast enough, DAMAGE needs the CLOSING speed
//     (wielder's speed along the look minus the target's) fast enough. The
//     damage is the wielder's base attack damage plus floor(closing speed ×
//     the weapon's multiplier), in blocks per second — a charge on horseback
//     is what the weapon is for.
//
// Mobs use the charge too (SpearUseGoal on zombies and zombified piglins),
// with a fifth of a player's speed thresholds and half the reach.

// kineticCond is KineticWeapon.Condition: a window in ticks after the delay,
// a minimum wielder speed and a minimum closing speed (blocks per second).
type kineticCond struct {
	maxTicks       int
	minSpeed, minR float64
}

// test is Condition.test; factor is 1 for a player, 0.2 for a mob.
func (c kineticCond) test(used int, attacker, relative, factor float64) bool {
	return used <= c.maxTicks && attacker >= c.minSpeed*factor && relative >= c.minR*factor
}

// spearSpec is one spear's numbers.
type spearSpec struct {
	item   int32
	wood   bool    // the wooden spear has its own sounds
	period int     // full-charge recovery in ticks: attack speed is 1/attack_duration
	mult   float64 // charge damage per block/second of closing speed
	delay  int     // ticks after lowering before the charge can connect
	// The three charge conditions (dismount and knockback read the wielder's
	// own speed, damage the closing speed).
	dismount, knock, damage kineticCond
}

// window is the last tick (after the delay) at which any condition can still
// hold; past it a lowered spear does nothing.
func (s *spearSpec) window() int {
	return max(s.dismount.maxTicks, s.knock.maxTicks, s.damage.maxTicks)
}

// useTicks is KineticWeapon.computeDamageUseDuration: the delay plus the
// damage window — how long a mob keeps its spear lowered.
func (s *spearSpec) useTicks() int { return s.delay + s.damage.maxTicks }

const (
	spearContactCooldown = 10    // KineticWeapon contact_cooldown_ticks: one hit per target per half second
	spearMinReach        = 2.0   // AttackRange min_reach
	spearMaxReach        = 4.5   // …max_reach
	spearMinReachCreat   = 2.0   // …min_creative_reach
	spearMaxReachCreat   = 6.5   // …max_creative_reach
	spearHitboxMargin    = 0.125 // …hitbox_margin
	spearMobReach        = 0.5   // …mob_factor: a mob's reach is half a player's
	spearMobAction       = 0.2   // KineticWeapon: a mob's speed thresholds are a fifth of a player's
	spearMinCharge       = 1.0   // MINIMUM_ATTACK_CHARGE
	spearChargeTolerance = 5     // cannotAttackWithItem(stack, 5)
	spearStabKnock       = 0.4   // stabAttack's own knockback, before the weapon's Knockback

	// entityStatusKineticHit is LivingEntity entity event 2 in 26.x: the
	// wielder's client plays the spear's hit sound (onKineticHit).
	entityStatusKineticHit = 2
)

// spearSpecs is keyed by item id; built from the arguments Items passes to
// Item.Properties.spear (attack duration, damage multiplier, delay, then
// time/threshold pairs for dismount, knockback and damage — seconds and
// blocks per second).
var spearSpecs = map[int32]*spearSpec{}

func init() {
	add := func(name string, duration, mult, delay, disT, disV, kbT, kbV, dmgT, dmgV float32) {
		id, ok := itemByName[name]
		if !ok {
			return
		}
		ticks := func(s float32) int { return int(s * 20) } // (int)(seconds * 20.0F), float arithmetic
		spearSpecs[id] = &spearSpec{
			item:     id,
			wood:     name == "wooden_spear",
			period:   int(math.Ceil(20*float64(duration) - 0.5)),
			mult:     float64(mult),
			delay:    ticks(delay),
			dismount: kineticCond{maxTicks: ticks(disT), minSpeed: float64(disV)},
			knock:    kineticCond{maxTicks: ticks(kbT), minSpeed: float64(kbV)},
			damage:   kineticCond{maxTicks: ticks(dmgT), minR: float64(dmgV)},
		}
	}
	add("wooden_spear", 0.65, 0.7, 0.75, 5.0, 14.0, 10.0, 5.1, 15.0, 4.6)
	add("stone_spear", 0.75, 0.82, 0.7, 4.5, 13.0, 9.0, 5.1, 13.75, 4.6)
	add("copper_spear", 0.85, 0.82, 0.65, 4.0, 12.0, 8.25, 5.1, 12.5, 4.6)
	add("iron_spear", 0.95, 0.95, 0.6, 2.5, 11.0, 6.75, 5.1, 11.25, 4.6)
	add("golden_spear", 0.95, 0.7, 0.7, 3.5, 13.0, 8.5, 5.1, 13.75, 4.6)
	add("diamond_spear", 1.05, 1.075, 0.5, 3.0, 10.0, 6.5, 5.1, 10.0, 4.6)
	add("netherite_spear", 1.15, 1.2, 0.4, 2.5, 9.0, 5.5, 5.1, 8.75, 4.6)
}

// spearOf is the item's spear numbers, or nil when it is not a spear.
func spearOf(item int32) *spearSpec { return spearSpecs[item] }

// spearSound is the spear's sound event for kind ("use", "attack", "hit").
func spearSound(sp *spearSpec, kind string) string {
	if sp.wood {
		return "minecraft:item.spear_wood." + kind
	}
	return "minecraft:item.spear." + kind
}

// spearTarget is one entity on the spear's line: a mob or a player.
type spearTarget struct {
	m *mob
	p *tracked
}

func (tg spearTarget) eid() int32 {
	if tg.m != nil {
		return tg.m.eid
	}
	return tg.p.p.eid
}

// motion is the target's known movement in blocks per tick.
func (h *hub) spearTargetMotion(tg spearTarget) (float64, float64, float64) {
	if tg.m != nil {
		return h.mobMotion(tg.m)
	}
	return h.knownMove(tg.p)
}

// mobMotion is a mob's movement in blocks per tick — its root vehicle's when
// it rides one (KineticWeapon.getMotion).
func (h *hub) mobMotion(m *mob) (float64, float64, float64) {
	if m.mount != 0 {
		if v := h.mobs[m.mount]; v != nil {
			m = v
		}
	}
	return m.vx / mobMoveInterval, 0, m.vz / mobMoveInterval
}

// knownMove is ServerPlayer.getKnownMovement: the last movement the client
// reported, in blocks per tick — zero once it has stopped reporting any.
func (h *hub) knownMove(t *tracked) (float64, float64, float64) {
	if t.kvAt == 0 || h.tick.Load()-t.kvAt > 2 {
		return 0, 0, 0
	}
	return t.kvx, t.kvy, t.kvz
}

// noteKnownMove records a reported movement (a move packet's delta, or the
// ridden vehicle's). A teleport-sized jump is not movement.
func (h *hub) noteKnownMove(t *tracked, dx, dy, dz float64) {
	if dx*dx+dy*dy+dz*dz > 100 {
		dx, dy, dz = 0, 0, 0
	}
	t.kvx, t.kvy, t.kvz, t.kvAt = dx, dy, dz, h.tick.Load()
}

// spearReach is the player's AttackRange for a spear: survival or creative.
func spearReach(t *tracked) (float64, float64) {
	if t.gamemode == gmCreative {
		return spearMinReachCreat, spearMaxReachCreat
	}
	return spearMinReach, spearMaxReach
}

// spearLine is ProjectileUtil.getHitEntitiesAlong for an AttackRange: every
// entity whose box the segment from eye+look·minR to eye+look·(maxR +
// along) crosses (a box inflated by the margin counts when nothing solid
// stands between the graze and the box), cut short by the first solid block
// — and nothing at all when that block is nearer than minR. along is the
// wielder's movement along the look, in blocks per tick, when positive.
// self and ride are the wielder and its vehicle, never struck; mobs says
// whether mobs are candidates (a mob's charge strikes players only).
func (h *hub) spearLine(players map[int32]*tracked, dim int, ex, ey, ez, lx, ly, lz, minR, maxR, along float64,
	self, ride int32, mobs bool) []spearTarget {
	far := maxR + math.Max(0, along)
	if d := h.rayBlockDist(dim, ex, ey, ez, lx, ly, lz, far); d < minR {
		return nil
	} else if d < far {
		far = d
	}
	fx, fy, fz := ex+lx*minR, ey+ly*minR, ez+lz*minR
	seg := far - minR
	hits := func(x0, y0, z0, x1, y1, z1 float64) bool {
		if fx >= x0 && fx <= x1 && fy >= y0 && fy <= y1 && fz >= z0 && fz <= z1 {
			return true // the jab starts inside it
		}
		if d, ok := rayBox(fx, fy, fz, lx, ly, lz, x0, y0, z0, x1, y1, z1); ok && d <= seg {
			return true
		}
		m := spearHitboxMargin
		d, ok := rayBox(fx, fy, fz, lx, ly, lz, x0-m, y0-m, z0-m, x1+m, y1+m, z1+m)
		if !ok || d > seg {
			return false
		}
		// A graze: struck only if nothing solid stands between the graze and
		// the box's middle.
		return h.sightClear(dim, fx+lx*d, fy+ly*d, fz+lz*d, (x0+x1)/2, (y0+y1)/2, (z0+z1)/2)
	}
	reach2 := (far + 4) * (far + 4)
	var out []spearTarget
	if mobs {
		for _, m := range h.mobs {
			if m.dim != dim || m.dying > 0 || m.eid == self || m.eid == ride || (ride != 0 && m.mount == ride) {
				continue
			}
			if dx, dz := m.x-ex, m.z-ez; dx*dx+dz*dz > reach2 {
				continue
			}
			b := m.box()
			if hits(m.x-b.w/2, m.y, m.z-b.w/2, m.x+b.w/2, m.y+b.h, m.z+b.w/2) {
				out = append(out, spearTarget{m: m})
			}
		}
	}
	for _, o := range players {
		if o.p.eid == self || o.dim != dim || o.dead || o.gamemode == gmSpectator {
			continue
		}
		if dx, dz := o.x-ex, o.z-ez; dx*dx+dz*dz > reach2 {
			continue
		}
		ht := 1.8
		if o.p.sneaking {
			ht = 1.5
		}
		if hits(o.x-0.3, o.y, o.z-0.3, o.x+0.3, o.y+ht, o.z+0.3) {
			out = append(out, spearTarget{p: o})
		}
	}
	return out
}

// rayBlockDist is the distance along the unit ray to the first colliding
// block, or maxD when none is nearer (Level.clip with COLLIDER, cell-sized).
// The cell the ray starts in is not tested — the eyes are always in one.
func (h *hub) rayBlockDist(dim int, ox, oy, oz, dx, dy, dz, maxD float64) float64 {
	w := h.worldFor(dim)
	if w == nil {
		return maxD
	}
	cx, cy, cz := int(math.Floor(ox)), int(math.Floor(oy)), int(math.Floor(oz))
	sx, tMaxX, tDeltaX := ddaAxis(ox, dx)
	sy, tMaxY, tDeltaY := ddaAxis(oy, dy)
	sz, tMaxZ, tDeltaZ := ddaAxis(oz, dz)
	for steps := 0; steps < 64; steps++ {
		var t float64
		switch {
		case tMaxX < tMaxY && tMaxX < tMaxZ:
			t = tMaxX
			cx += sx
			tMaxX += tDeltaX
		case tMaxY < tMaxZ:
			t = tMaxY
			cy += sy
			tMaxY += tDeltaY
		default:
			t = tMaxZ
			cz += sz
			tMaxZ += tDeltaZ
		}
		if t > maxD {
			return maxD
		}
		if worldgen.Collides(w.At(cx, cy, cz)) {
			return t
		}
	}
	return maxD
}

// spearBlow is one stabAttack: how much, and which of its three effects.
type spearBlow struct {
	dmg                     float64
	damage, knock, dismount bool
}

// evSpearStab is the STAB player action (or an attack with a spear in hand).
type evSpearStab struct{ eid int32 }

// evSpearUse lowers a spear for the charge (use_item with a spear).
type evSpearUse struct{ eid int32 }

func (evSpearStab) isHubEvent() {}
func (evSpearUse) isHubEvent()  {}

// spearStab is PiercingWeapon.attack for a player: the jab.
func (h *hub) spearStab(players map[int32]*tracked, t *tracked) {
	if t == nil || t.dead || t.gamemode == gmSpectator {
		return
	}
	held := t.p.heldItem()
	sp := spearOf(held)
	if sp == nil {
		return
	}
	now := h.tick.Load()
	period := float64(t.attackPeriodTicks(attackPeriod(held)))
	strength := 1.0
	if t.lastAttack != 0 {
		since := float64(now - t.lastAttack)
		if (since+spearChargeTolerance)/period < spearMinCharge {
			return // Player.cannotAttackWithItem: the spear is not ready
		}
		strength = math.Min(1, since/period)
	}
	// ATTACK_DAMAGE: the spear's base, and every modifier on the attribute.
	t.playerAttrs().SetBase(attr.AttackDamage, float64(meleeDamage[held]))
	dmg := math.Max(0, t.playerAttrs().Value(attr.AttackDamage))
	lx, ly, lz := lookVector(t.yaw, t.pitch)
	kx, ky, kz := h.knownMove(t)
	minR, maxR := spearReach(t)
	hit := false
	for _, tg := range h.spearLine(players, t.dim, t.x, playerEyeY(t), t.z, lx, ly, lz, minR, maxR,
		kx*lx+ky*ly+kz*lz, t.p.eid, t.ridingEID, true) {
		if h.playerStab(players, t, tg, spearBlow{dmg: dmg, damage: true, knock: true}, strength) {
			hit = true
		}
	}
	t.lastAttack = now // swingAndResetAttackStrength
	h.lunge(players, t)
	if hit {
		h.playSoundDim(players, t.dim, spearSound(sp, "hit"), sndPlayer, t.x, t.y, t.z, 1, 1)
	}
	h.playSoundExcept(players, t.dim, t.p.eid, spearSound(sp, "attack"), sndPlayer, t.x, t.y, t.z, 1, 1)
}

// startSpearCharge is Item.use for a kinetic weapon: startUsingItem, a fresh
// record of who has been struck, and the sound.
func (h *hub) startSpearCharge(players map[int32]*tracked, t *tracked) {
	sp := spearOf(t.p.heldItem())
	if t.dead || t.gamemode == gmSpectator || sp == nil || t.spearAt != 0 {
		return
	}
	t.spearAt = h.tick.Load()
	t.spearHits = map[int32]uint64{}
	h.playSoundExcept(players, t.dim, t.p.eid, spearSound(sp, "use"), sndPlayer, t.x, t.y, t.z, 1, 1)
}

// stopSpearCharge is stopUsingItem for a lowered spear.
func stopSpearCharge(t *tracked) {
	t.spearAt, t.spearHits = 0, nil
}

// tickSpearCharges runs KineticWeapon.damageEntities for every player holding
// a lowered spear, once per tick.
func (h *hub) tickSpearCharges(players map[int32]*tracked) {
	for _, t := range players {
		if t.spearAt != 0 {
			h.spearChargeTick(players, t)
		}
	}
}

func (h *hub) spearChargeTick(players map[int32]*tracked, t *tracked) {
	sp := spearOf(t.p.heldItem())
	if sp == nil || t.dead || t.gamemode == gmSpectator {
		stopSpearCharge(t)
		return
	}
	now := h.tick.Load()
	used := int(now - t.spearAt)
	if used < sp.delay {
		return
	}
	if used -= sp.delay; used > sp.window() {
		return
	}
	lx, ly, lz := lookVector(t.yaw, t.pitch)
	kx, ky, kz := h.knownMove(t)
	along := kx*lx + ky*ly + kz*lz
	attacker := along * 20 // blocks per second
	minR, maxR := spearReach(t)
	affected := false
	for _, tg := range h.spearLine(players, t.dim, t.x, playerEyeY(t), t.z, lx, ly, lz, minR, maxR,
		along, t.p.eid, t.ridingEID, true) {
		eid := tg.eid()
		if at, ok := t.spearHits[eid]; ok && now-at < spearContactCooldown {
			continue // wasRecentlyStabbed
		}
		t.spearHits[eid] = now
		tx, ty, tz := h.spearTargetMotion(tg)
		rel := math.Max(0, attacker-(tx*lx+ty*ly+tz*lz)*20)
		b := spearBlow{
			dismount: sp.dismount.test(used, attacker, rel, 1),
			knock:    sp.knock.test(used, attacker, rel, 1),
			damage:   sp.damage.test(used, attacker, rel, 1),
		}
		if !b.dismount && !b.knock && !b.damage {
			continue
		}
		// The player's BASE attack damage (1), not the spear's: the charge's
		// damage is all speed.
		b.dmg = float64(fistDamage) + math.Floor(rel*sp.mult)
		if h.playerStab(players, t, tg, b, 1) {
			affected = true
		}
	}
	if affected {
		h.toNearbyEv(players, t.dim, t.x, t.z, entityStatus(t.p.eid, entityStatusKineticHit))
		h.advance(players, t, "spear_mobs", advMatch{count: len(t.spearHits)})
	}
}

// playerStab is Player.stabAttack. strength is the attack-strength scale a
// jab is taken at (1 for the charge, whose held use skips the scaling).
func (h *hub) playerStab(players map[int32]*tracked, t *tracked, tg spearTarget, b spearBlow, strength float64) bool {
	st := heldStack(t)
	magic := 0.0 // getEnchantedDamage − base: Sharpness, and Smite/Bane by the victim's family
	if lvl := st.enchLvl(enchSharpness); lvl > 0 {
		magic = 0.5*float64(lvl) + 0.5
	}
	if tg.m != nil {
		magic += familyMeleeBonus(st, tg.m.etype)
	}
	base := b.dmg
	if strength < 1 {
		magic *= strength
		base *= 0.2 + 0.8*strength*strength
	}
	total := 0.0
	if b.damage {
		total = base + magic
	}
	if tg.m != nil {
		return h.stabMobByPlayer(players, t, tg.m, total, b)
	}
	return h.stabPlayerByPlayer(players, t, tg.p, total, b)
}

// stabKnock is the blow's shove: stabAttack's own 0.4, then the weapon's
// Knockback (half a unit per level), both along the wielder's facing.
func stabKnock(knockbackLvl int) float64 {
	return spearStabKnock + 0.5*float64(knockbackLvl)
}

func (h *hub) stabMobByPlayer(players map[int32]*tracked, t *tracked, m *mob, total float64, b spearBlow) bool {
	st := heldStack(t)
	landed := false
	if b.damage && total > 0 {
		if plugin.Has[*plugin.EntityDamageByEntityEvent](h.plugins) {
			dev := &plugin.EntityDamageByEntityEvent{AttackerEID: t.p.eid, VictimEID: m.eid,
				AttackerIsPlayer: true, Damage: total}
			if !h.plugins.Fire(dev) {
				return false
			}
			total = math.Max(0, dev.Damage)
		}
		t.lastHitMob = m.eid
		m.hitByPlayer, m.lastAttacker = true, t.p.eid
		m.looting = st.enchLvl(enchLooting)
		if m.etype == entityPiglin {
			h.piglinHurtByPlayer(players, m)
		}
		if m.etype == entityArmadillo {
			h.armadilloHurtByLiving(players, m)
		}
		dmg := total
		if m == h.dragon {
			dmg = dragonPartDamage("body", dmg)
		}
		hpBefore := m.health
		m.hurtOf(dmg, 0, dtSpear)
		if landed = m.health < hpBefore; landed {
			h.incCustom(t, "damage_dealt", tenths(float32(total)))
			h.advance(players, t, "player_hurt_entity", advMatch{damageDirect: "player", mainhand: st.item, dealt: total})
			h.applyFireAspect(players, t, m)
			h.applyBaneSlowness(players, st, m)
		}
	}
	if b.knock && m.kbScale() > 0 {
		// LivingEntity.knockback along the wielder's facing: half what the mob
		// had, plus the shove.
		dx, dz := math.Sin(float64(t.yaw)*math.Pi/180), math.Cos(float64(t.yaw)*math.Pi/180)
		step := stabKnock(st.enchLvl(enchKnockback)) * m.kbScale() * mobMoveInterval
		m.vx, m.vz = m.vx/2-dx*step, m.vz/2+dz*step
		m.kb, m.reroute = 3, 0
		h.mobKnockVelocity(players, m)
	}
	dismounted := b.dismount && h.unseatMob(players, m)
	if !landed && !b.knock && !dismounted {
		return false
	}
	if isSurvival(t.gamemode) {
		t.exhaust(attackExhaustion)
		h.applyToolWear(t, t.p.heldSlot(), 1) // Weapon: one point per enemy struck
	}
	if landed {
		h.mobStruck(players, m, t, dtSpear)
	}
	return true
}

func (h *hub) stabPlayerByPlayer(players map[int32]*tracked, t, v *tracked, total float64, b spearBlow) bool {
	if !h.rules.PvP || v.dead || (!isSurvival(t.gamemode) && t.gamemode != gmAdventure) ||
		v.gamemode == gmCreative || v.gamemode == gmSpectator {
		return false // Player.canHarmPlayer, and nothing to hurt
	}
	st := heldStack(t)
	landed := false
	if b.damage && total > 0 {
		if plugin.Has[*plugin.EntityDamageByEntityEvent](h.plugins) {
			dev := &plugin.EntityDamageByEntityEvent{AttackerEID: t.p.eid, VictimEID: v.p.eid,
				AttackerIsPlayer: true, Damage: total}
			if !h.plugins.Fire(dev) {
				return false
			}
			total = math.Max(0, dev.Damage)
		}
		cause := deathCause{by: t.p.name}
		if st.count > 0 {
			cause.weapon = st.name
		}
		hpBefore := v.health + v.absorption
		landed = h.hurtFrom(players, v, float32(total), dtSpear, cause, fromWeapon(t.x, t.z, st.item)) &&
			v.health+v.absorption < hpBefore
		if landed {
			h.incCustom(t, "damage_dealt", tenths(float32(total)))
			if lvl := st.enchLvl(enchFireAspect); lvl > 0 && v.hasEffect(effFireRes) == 0 {
				v.fireSecs = max(v.fireSecs, 4*lvl)
			}
			if v.dead {
				h.incCustom(t, "player_kills", 1)
				h.sbCriteria(players, "playerKillCount", t.p.name, 1, false)
				h.sbCriteria(players, "totalKillCount", t.p.name, 1, false)
			}
		}
	}
	if b.knock {
		dx, dz := -math.Sin(float64(t.yaw)*math.Pi/180), math.Cos(float64(t.yaw)*math.Pi/180)
		h.knockbackScaled(v, v.x-dx, v.z-dz, stabKnock(st.enchLvl(enchKnockback))/0.4)
	}
	dismounted := false
	if b.dismount && v.ridingEID != 0 {
		if !h.dismountMob(players, v) {
			h.dismount(players, v)
		}
		dismounted = true
	}
	if !landed && !b.knock && !dismounted {
		return false
	}
	if isSurvival(t.gamemode) {
		t.exhaust(attackExhaustion)
		h.applyToolWear(t, t.p.heldSlot(), 1)
	}
	return true
}

// unseatMob stops a mob riding another (Entity.stopRiding for the mob-on-mob
// seats). Reports whether it was riding.
func (h *hub) unseatMob(players map[int32]*tracked, m *mob) bool {
	if m.mount == 0 {
		return false
	}
	if v := h.mobs[m.mount]; v != nil {
		h.freeMobSeat(players, v, m.eid)
	}
	m.mount, m.mountDrives, m.navMount = 0, false, nil
	return true
}
