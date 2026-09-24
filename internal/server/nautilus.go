package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// The nautilus as a mount (AbstractNautilus, 1.21.11): tamed with a
// pufferfish (one try in three), saddled, and ridden under water — where
// its rider breathes, on the Breath of the Nautilus the mount grants and
// refreshes — with a dash on the jump key like the camel's. The riding
// client applies the dash impulse itself (executeRidersJump on the local
// authority); the server plays the dash, keeps the forty-tick cooldown and
// signals when it is ready again.

const (
	nautilusDashCooldown = 40 // AbstractNautilus.executeRidersJump: dashCooldown = 40
	nautilusBreathTicks  = 60 // applyEffects: 60 ticks, refreshed every 40
)

// nautilusDashStart is handleStartJump for the nautilus the player rides.
func (h *hub) nautilusDashStart(players map[int32]*tracked, t *tracked) {
	m := h.mobs[t.ridingEID]
	if m == nil || m.etype != entityNautilus || m.dying > 0 || !m.saddled || m.dashCD > 0 {
		return
	}
	m.dashCD, m.dashing = nautilusDashCooldown, true
	snd := "minecraft:entity.nautilus.dash_land"
	if h.inWater(m.dim, m.x, m.y, m.z) {
		snd = "minecraft:entity.nautilus.dash"
	}
	h.playSoundDim(players, m.dim, snd, sndNeutral, m.x, m.y, m.z, 1, 1)
	h.vibAt(m.dim, freqEntityAction, m.x, m.y, m.z, m.eid)
}

// nautilusDashTick runs the cooldown and plays the ready cue when it ends.
func (h *hub) nautilusDashTick(players map[int32]*tracked, m *mob) {
	if m.dashCD == 0 {
		return
	}
	if m.dashCD -= mobMoveInterval; m.dashCD <= 0 {
		m.dashCD, m.dashing = 0, false
		snd := "minecraft:entity.nautilus.dash_ready_land"
		if h.inWater(m.dim, m.x, m.y, m.z) {
			snd = "minecraft:entity.nautilus.dash_ready"
		}
		h.playSoundDim(players, m.dim, snd, sndNeutral, m.x, m.y, m.z, 1, 1)
	}
}

// nautilusBreath is AbstractNautilus.applyEffects, on the survival step:
// the rider carries Breath of the Nautilus while mounted.
func (h *hub) nautilusBreath(players map[int32]*tracked, m *mob) {
	t := players[m.rider]
	if t == nil || t.dead {
		return
	}
	if t.hasEffect(effBreathOfTheNautilus) == 0 || h.tick.Load()%40 == 0 {
		h.applyEffect(players, t, effBreathOfTheNautilus, 0, nautilusBreathTicks/20)
	}
}

// The nautilus brains (NautilusAi and ZombieNautilusAi). Idle, a nautilus
// courts at 0.4 of its speed (AnimalMakeLove, the nautilus only), follows a
// player holding #nautilus_food (FollowTemptation: 1.3, the zombie 0.9,
// stopping 3.5 blocks off, 2.5 for a baby) and otherwise swims about.
// StartAttacking takes whoever hurt it (ANGRY_AT, 400 ticks) if they are in
// the water with it, or — once every 2400-3600 ticks, one time in two — the
// nearest pufferfish it can see in the water. With a target, no tempting
// player, no mate and no charge cooldown the FIGHT activity runs
// ChargeAttack: a straight rush at a fixed speed that hits whatever it
// touches first, gives up twelve blocks from where it began, eleven from the
// target or out of its sight, and then waits eighty ticks. Neither kind
// hunts players on its own: the zombie nautilus is an animal with the same
// target search, not a monster.

const (
	nautilusLoveSpeed      = 0.4  // SPEED_MULTIPLIER_WHEN_MAKING_LOVE
	nautilusLoveClose      = 2.0  // AnimalMakeLove's closeEnoughDistance
	nautilusAngerTicks     = 400  // ANGER_DURATION
	nautilusTargetCDMin    = 2400 // TIME_BETWEEN_NON_PLAYER_ATTACKS: UniformInt.of(2400, 3600)
	nautilusTargetCDSpan   = 1201
	nautilusSenseRange     = 16.0 // NEAREST_LIVING_ENTITIES and the targeting range (FOLLOW_RANGE)
	nautilusChargeCooldown = 80   // TIME_BETWEEN_ATTACKS
	nautilusChargeKB       = 2.0  // ATTACK_KNOCKBACK_FORCE
	nautilusChargeMaxRun   = 12.0 // MAX_CHARGE_DISTANCE
	nautilusChargeMaxSight = 11.0 // MAX_TARGET_DETECTION_DISTANCE
)

// nautilusKind is AbstractNautilus: the nautilus and the zombie nautilus.
func nautilusKind(etype int) bool { return etype == entityNautilus || etype == entityZombieNautilus }

// nautilusChargeSpeed is ChargeAttack's speed in blocks per tick
// (SPEED_WHEN_ATTACKING: 0.6, the zombie's 0.5).
func nautilusChargeSpeed(etype int) float64 {
	if etype == entityZombieNautilus {
		return 0.5
	}
	return 0.6
}

// nautilusSpeedAttr is the vanilla MOVEMENT_SPEED the charge's knockback
// scales by (AbstractNautilus 1.0, ZombieNautilus 1.1).
func nautilusSpeedAttr(etype int) float64 {
	if etype == entityZombieNautilus {
		return 1.1
	}
	return 1.0
}

// nautilusSpawned is finalizeSpawn's NautilusAi.initMemories: a fresh
// nautilus waits out an ATTACK_TARGET_COOLDOWN before its first look for prey.
func (h *hub) nautilusSpawned(m *mob) {
	m.nautTargetCD = nautilusTargetCDMin + h.rng.Intn(nautilusTargetCDSpan)
}

// nautilusAngerAt is NautilusAi.setAngerTarget from hurtServer: whoever
// hurt it, if it could fight them at all, is ANGRY_AT for 400 ticks.
func (h *hub) nautilusAngerAt(m *mob, t *tracked) {
	if !nautilusKind(m.etype) || t == nil || t.dead || t.gamemode == gmCreative || t.gamemode == gmSpectator {
		return
	}
	m.nautAngryAt, m.nautAngryUntil = t.p.eid, h.tick.Load()+nautilusAngerTicks
}

// nautilusQuarry resolves a target eid: a player who can be fought, or a
// living mob, in the nautilus's dimension.
func (h *hub) nautilusQuarry(players map[int32]*tracked, m *mob, eid int32) (quarry, bool) {
	if t := players[eid]; t != nil {
		if t.dead || t.dim != m.dim || t.gamemode == gmCreative || t.gamemode == gmSpectator {
			return quarry{}, false
		}
		return quarry{t: t, x: t.x, y: t.y, z: t.z}, true
	}
	if o := h.mobs[eid]; o != nil && o != m && o.dying == 0 && o.dim == m.dim {
		return quarry{o: o, x: o.x, y: o.y, z: o.z}, true
	}
	return quarry{}, false
}

// nautilusFindTarget is NautilusAi.findNearestValidAttackTarget.
func (h *hub) nautilusFindTarget(players map[int32]*tracked, m *mob) (quarry, bool) {
	if m.loveTicks > 0 || m.baby || m.tamed || !h.inWater(m.dim, m.x, m.y, m.z) {
		return quarry{}, false
	}
	if m.nautAngryAt != 0 && h.tick.Load() < m.nautAngryUntil {
		if q, ok := h.nautilusQuarry(players, m, m.nautAngryAt); ok && h.inWater(m.dim, q.x, q.y, q.z) &&
			dist3(q.x, q.y, q.z, m.x, m.y, m.z) <= nautilusSenseRange {
			return q, true
		}
	}
	if m.nautTargetCD > 0 {
		return quarry{}, false
	}
	m.nautTargetCD = nautilusTargetCDMin + h.rng.Intn(nautilusTargetCDSpan)
	if h.rng.Float64() < 0.5 {
		return quarry{}, false
	}
	// The nearest visible #nautilus_hostiles (the pufferfish) in the water.
	var best *mob
	bestD := nautilusSenseRange
	h.grid().nearby(m.dim, m.x, m.z, nautilusSenseRange, func(o *mob) {
		if o.etype != entityPufferfish || o.dying > 0 || !h.inWater(o.dim, o.x, o.y, o.z) {
			return
		}
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < bestD && h.mobSeesMob(m, o) {
			best, bestD = o, d
		}
	})
	if best == nil {
		return quarry{}, false
	}
	return quarry{o: best, x: best.x, y: best.y, z: best.z}, true
}

// nautilusFightStep runs the target search and the FIGHT activity. Returns
// whether the charge holds the nautilus this update.
func (h *hub) nautilusFightStep(players map[int32]*tracked, m *mob) bool {
	m.chargeCD = max(0, m.chargeCD-mobMoveInterval)
	m.nautTargetCD = max(0, m.nautTargetCD-mobMoveInterval)
	if m.charging {
		return h.nautilusChargeTick(players, m)
	}
	if m.nautTarget == 0 { // StartAttacking only looks while it holds no target
		if q, ok := h.nautilusFindTarget(players, m); ok {
			m.nautTarget = q.eid()
		}
	}
	if m.nautTarget == 0 || m.chargeCD > 0 || m.loveTicks > 0 || h.temptingPlayer(players, m) != nil {
		return false // FIGHT wants ATTACK_TARGET and no TEMPTING_PLAYER, BREED_TARGET or CHARGE_COOLDOWN_TICKS
	}
	// ChargeAttack.start: the rush is aimed once, from here, at where the
	// target is now.
	q, ok := h.nautilusQuarry(players, m, m.nautTarget)
	if !ok {
		h.nautilusChargeStop(m)
		return false
	}
	dx, dy, dz := q.x-m.x, q.y-m.y, q.z-m.z
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		dx, dy, dz, d = 0, 0, 1, 1
	}
	sp := nautilusChargeSpeed(m.etype)
	m.chargeSX, m.chargeSY, m.chargeSZ = m.x, m.y, m.z
	m.chargeVX, m.chargeVY, m.chargeVZ = dx/d*sp, dy/d*sp, dz/d*sp
	m.charging = true
	if h.nautilusChargeHolds(m, q) {
		h.playSoundDim(players, m.dim, "minecraft:entity."+entityNameByID[m.etype]+".dash", sndNeutral, m.x, m.y, m.z, 1, 1)
	}
	return h.nautilusChargeTick(players, m)
}

// nautilusChargeHolds is ChargeAttack.canStillUse.
func (h *hub) nautilusChargeHolds(m *mob, q quarry) bool {
	if m.tamed || dist3(m.x, m.y, m.z, m.chargeSX, m.chargeSY, m.chargeSZ) >= nautilusChargeMaxRun ||
		dist3(q.x, q.y, q.z, m.x, m.y, m.z) >= nautilusChargeMaxSight {
		return false
	}
	if q.t != nil {
		return h.mobSees(m, q.t)
	}
	return h.mobSeesMob(m, q.o)
}

// nautilusChargeStop is ChargeAttack.stop: the cooldown starts and the
// target is dropped.
func (h *hub) nautilusChargeStop(m *mob) {
	m.charging, m.nautTarget, m.chargeCD = false, 0, nautilusChargeCooldown
}

// nautilusChargeTick is ChargeAttack.tick for one update: the fixed velocity
// is held, facing the target, and the first body it touches on either tick
// takes the blow.
func (h *hub) nautilusChargeTick(players map[int32]*tracked, m *mob) bool {
	q, ok := h.nautilusQuarry(players, m, m.nautTarget)
	if !ok || !h.nautilusChargeHolds(m, q) {
		h.nautilusChargeStop(m)
		return false
	}
	m.yaw = yawToward(m.x, m.z, q.x, q.z)
	m.headYaw = m.yaw
	for i := 0; i < mobMoveInterval; i++ {
		f := float64(i)
		if h.nautilusChargeHit(players, m, m.x+m.chargeVX*f, m.y+m.chargeVY*f, m.z+m.chargeVZ*f) {
			h.nautilusChargeStop(m)
			m.vx, m.vy, m.vz = 0, 0, 0
			return true
		}
	}
	m.vx, m.vy, m.vz = m.chargeVX*mobMoveInterval, m.chargeVY*mobMoveInterval, m.chargeVZ*mobMoveInterval
	m.rest = 0
	return true
}

// nautilusChargeHit is the contact test at one tick's position: the first
// fightable body overlapping the nautilus's (never its own rider) takes its
// ATTACK_DAMAGE as a mob attack and a shove along its heading of
// clamp(speed × MOVEMENT_SPEED, 0.2, 2) × 2 (causeExtraKnockback).
func (h *hub) nautilusChargeHit(players map[int32]*tracked, m *mob, x, y, z float64) bool {
	b := m.box()
	dmg := hostileMelee(m)
	power := math.Min(math.Max(nautilusChargeSpeed(m.etype)*nautilusSpeedAttr(m.etype), 0.2), 2.0) * nautilusChargeKB
	yr := float64(m.yaw) * math.Pi / 180
	fx, fz := -math.Sin(yr), math.Cos(yr) // the body's facing
	overlaps := func(ox, oy, oz, w, hgt float64) bool {
		reach := (b.w + w) / 2
		return math.Abs(ox-x) < reach && math.Abs(oz-z) < reach && oy < y+b.h && oy+hgt > y
	}
	for _, t := range players {
		if t.dim != m.dim || t.dead || t.gamemode == gmCreative || t.gamemode == gmSpectator || t.p.eid == m.rider {
			continue
		}
		if !overlaps(t.x, t.y, t.z, 0.6, 1.8) {
			continue
		}
		h.hurtFrom(players, t, dmg, dtMobAttack, deathCause{by: mobDisplayName(m.etype)}, fromMob(m.x, m.z))
		h.nautilusShovePlayer(t, fx, fz, power)
		return true
	}
	var hit *mob
	h.grid().nearby(m.dim, x, z, 2, func(o *mob) {
		if hit != nil || o == m || o.dying > 0 || o.eid == m.mobRider {
			return
		}
		if ob := o.box(); overlaps(o.x, o.y, o.z, ob.w, ob.h) {
			hit = o
		}
	})
	if hit == nil {
		return false
	}
	h.hurtMobOf(players, hit, float64(dmg), dtMobAttack)
	kb := power * hit.kbScale()
	hit.vx, hit.vz, hit.kb, hit.reroute = fx*kb, fz*kb, 3, 0
	h.mobKnockVelocity(players, hit)
	return true
}

// nautilusShovePlayer is LivingEntity.knockback along the nautilus's
// heading: the resistance takes its share, and a player standing on
// something is lifted by at most 0.4.
func (h *hub) nautilusShovePlayer(t *tracked, fx, fz, power float64) {
	if r := t.playerAttrs().Value(attr.KnockbackResistance); r > 0 {
		if power *= 1 - r; power <= 0 {
			return
		}
	}
	vy := 0.0
	if t.p.onGround {
		vy = math.Min(0.4, power)
	}
	t.p.trySendEv(attachproto.Velocity{EID: t.p.eid, VX: fx * power, VY: vy, VZ: fz * power})
}

// nautilusLoveStep is AnimalMakeLove(NAUTILUS, 0.4, 2): a courting nautilus
// swims to the nearest other courting nautilus it can see, stopping two
// blocks off (updateBreeding pairs them once they are within three).
func (h *hub) nautilusLoveStep(m *mob) bool {
	if m.etype != entityNautilus || m.loveTicks <= 0 {
		return false
	}
	var mate *mob
	best := breedRange
	h.grid().nearby(m.dim, m.x, m.z, breedRange, func(o *mob) {
		if o == m || o.etype != entityNautilus || o.loveTicks <= 0 || o.dying > 0 || o.panic > 0 {
			return
		}
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < best && h.mobSeesMob(m, o) {
			mate, best = o, d
		}
	})
	if mate == nil {
		return false
	}
	m.yaw = yawToward(m.x, m.z, mate.x, mate.z)
	if best <= nautilusLoveClose {
		m.vx, m.vy, m.vz = 0, 0, 0
		return true
	}
	h.swimToward(m, mate.x, mate.y, mate.z, nautilusLoveSpeed)
	return true
}

// swimToward steers a swimmer straight at a point in three dimensions at
// speed × its MOVEMENT_SPEED (a water-bound path has no floor to follow).
func (h *hub) swimToward(m *mob, x, y, z, speed float64) {
	dx, dy, dz := x-m.x, y-m.y, z-m.z
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		m.vx, m.vy, m.vz = 0, 0, 0
		return
	}
	sp := m.moveSpeed() * speed
	m.vx, m.vy, m.vz = dx/d*sp, dy/d*sp, dz/d*sp
	m.rest = 0
}
