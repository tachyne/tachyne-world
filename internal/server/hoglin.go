package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Hoglins and piglins out of the Nether (Hoglin/AbstractPiglin
// isConverting): three hundred ticks anywhere the dimension is not piglin
// safe and a hoglin is a zoglin, a piglin or brute a zombified piglin,
// nauseous for ten seconds — unless flagged immune. The hoglin's own ways
// (HoglinAi + HoglinBase): a bite of half its damage plus a random roll
// that throws the victim (adults), forty ticks between bites (fifteen for
// a piglet); warped fungus, a nether portal or a respawn anchor within
// eight blocks pacifies it for ten seconds and it walks off; an adult
// keeps eight blocks from adult piglins, and retreats at 1.3 for five to
// twenty seconds when they outnumber the hoglins; a hurt piglet runs.

const (
	zombifyTicks         = 300 // CONVERSION_TIME (hoglin and piglin alike)
	hoglinRepellentH     = 8   // REPELLENT_DETECTION_RANGE_HORIZONTAL
	hoglinRepellentV     = 4
	hoglinPacifyTicks    = 200 // REPELLENT_PACIFY_TIME
	hoglinIdleFromPiglin = 8.0 // DESIRED_DISTANCE_FROM_PIGLIN_WHEN_IDLING
	hoglinRetreatFrom    = 15.0
	hoglinRetreatSpeed   = 1.3
	hoglinIdleAwaySpeed  = 0.4
	hoglinAttackTicks    = 40 // ATTACK_INTERVAL
	hoglinBabyAttack     = 15 // BABY_ATTACK_INTERVAL
	hoglinAnimTicks      = 10 // ATTACK_ANIMATION_DURATION
	hoglinSeeRange       = 16.0
)

// zombifiesOutside reports the species the overworld turns.
func zombifiesOutside(etype int) (int, bool) {
	switch etype {
	case entityHoglin:
		return entityZoglin, true
	case entityPiglin, entityPiglinBrute:
		return entityZombifiedPiglin, true
	}
	return 0, false
}

// zombifyTick runs each mob update for piglins, brutes and hoglins.
func (h *hub) zombifyTick(players map[int32]*tracked, m *mob) {
	target, ok := zombifiesOutside(m.etype)
	if !ok {
		return
	}
	if m.dim == 1 || m.immuneZombify {
		m.overworldTicks = 0
		return
	}
	m.overworldTicks += mobMoveInterval
	if m.overworldTicks <= zombifyTicks {
		return
	}
	sound := "minecraft:entity.piglin.converted_to_zombified"
	if m.etype == entityHoglin {
		sound = "minecraft:entity.hoglin.converted_to_zombified"
	}
	h.playSoundDim(players, m.dim, sound, sndHostile, m.x, m.y, m.z, 1, 1)
	h.convertMob(players, m, target)
	if nm := h.mobAtEID(m.x, m.y, m.z, target, m.dim); nm != nil {
		h.applyMobEffect(players, nm, effNausea, 0, 10) // NAUSEA 200 ticks
	}
}

// mobAtEID finds the freshly converted mob standing where the old one was.
func (h *hub) mobAtEID(x, y, z float64, etype, dim int) *mob {
	for _, o := range h.mobs {
		if o.etype == etype && o.dim == dim && o.x == x && o.y == y && o.z == z {
			return o
		}
	}
	return nil
}

// hoglinBiteDamage is hurtAndThrowTarget's roll: half the attack damage
// plus a random part of it for an adult, the plain value for a piglet.
func (h *hub) hoglinBiteDamage(m *mob) float32 {
	f := float32(m.attackDamage())
	if m.baby || int(f) <= 0 {
		return f
	}
	return f/2 + float32(h.rng.Intn(int(f)))
}

// hoglinThrow is HoglinBase.throwTarget on a player: ATTACK_KNOCKBACK
// (1.0) less the target's resistance, a random fifth-to-seventh of it
// sideways within ten degrees, up to half of it upward.
func (h *hub) hoglinThrow(t *tracked, m *mob) {
	d3 := m.mobAttrs().Value(attr.AttackKnockback) - t.playerAttrs().Value(attr.KnockbackResistance)
	if d3 <= 0 {
		return
	}
	dx, dz := t.x-m.x, t.z-m.z
	if d := math.Hypot(dx, dz); d > 1e-6 {
		dx, dz = dx/d, dz/d
	} else {
		dx, dz = 1, 0
	}
	rot := float64(h.rng.Intn(21)-10) * math.Pi / 180
	dx, dz = dx*math.Cos(rot)-dz*math.Sin(rot), dx*math.Sin(rot)+dz*math.Cos(rot)
	d6 := d3 * (h.rng.Float64()*0.5 + 0.2)
	d7 := d3 * h.rng.Float64() * 0.5
	t.p.trySendEv(attachproto.Velocity{EID: t.p.eid, VX: dx * d6, VY: d7, VZ: dz * d6})
}

// hoglinBiteStart is doHurtTarget's extras: the animation and the sound,
// and the species' own interval between bites.
func (h *hub) hoglinBiteStart(players map[int32]*tracked, m *mob) {
	h.toNearbyEv(players, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusAttack))
	sound := "minecraft:entity.hoglin.attack"
	if m.etype == entityZoglin {
		sound = "minecraft:entity.zoglin.attack"
	}
	h.playSoundDim(players, m.dim, sound, sndHostile, m.x, m.y, m.z, 1, 1)
}

func hoglinAttackCD(m *mob) int {
	if m.baby {
		return hoglinBabyAttack/mobMoveInterval - 1
	}
	return hoglinAttackTicks/mobMoveInterval - 1
}

// isHoglinRepellent is #hoglin_repellents.
func isHoglinRepellent(s uint32) bool {
	return s == worldgen.BlockBase("warped_fungus") || s == worldgen.BlockBase("potted_warped_fungus") ||
		(s >= worldgen.NetherPortal && s <= worldgen.NetherPortal+1) ||
		(s >= worldgen.BlockBase("respawn_anchor") && s <= worldgen.BlockBase("respawn_anchor")+4)
}

// hoglinNearestRepellent is the NEAREST_REPELLENT sensor: 8 × 4 × 8 round it.
func (h *hub) hoglinNearestRepellent(m *mob) (blockPos, bool) {
	w := h.worldFor(m.dim)
	bx, by, bz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	best, bestD := blockPos{}, math.Inf(1)
	for x := bx - hoglinRepellentH; x <= bx+hoglinRepellentH; x++ {
		for y := by - hoglinRepellentV; y <= by+hoglinRepellentV; y++ {
			for z := bz - hoglinRepellentH; z <= bz+hoglinRepellentH; z++ {
				if !isHoglinRepellent(w.At(x, y, z)) {
					continue
				}
				if d := dist3(float64(x)+0.5, float64(y), float64(z)+0.5, m.x, m.y, m.z); d < bestD {
					best, bestD = blockPos{x, y, z}, d
				}
			}
		}
	}
	return best, bestD < math.Inf(1)
}

// hoglinStep is the brain's idle/avoid activities. Returns whether it
// holds the hoglin (the fight activity is the ordinary hostile hunt).
func (h *hub) hoglinStep(players map[int32]*tracked, m *mob) bool {
	now := h.tick.Load()
	if m.hogPacified > 0 {
		m.hogPacified -= mobMoveInterval
	}
	if m.hogRetreat > 0 {
		m.hogRetreat -= mobMoveInterval
	}
	// The repellent sensor, once a second.
	if now%20 < uint64(mobMoveInterval) {
		if pos, ok := h.hoglinNearestRepellent(m); ok {
			m.hogPacified = hoglinPacifyTicks
			m.hogRepellent, m.hogRepelled = pos, true
		} else {
			m.hogRepelled = false
		}
	}
	if m.hogPacified > 0 {
		m.hasTarget = false // BecomePassiveIfMemoryPresent
	}
	// A hurt piglet runs from the nearest player; an outnumbered adult from
	// the piglins.
	adultPiglins, adultHoglins := 0, 0
	var nearestPiglin *mob
	nearD := math.Inf(1)
	h.grid().nearby(m.dim, m.x, m.z, hoglinSeeRange, func(o *mob) {
		if o == m || o.baby || o.dying > 0 {
			return
		}
		switch o.etype {
		case entityPiglin, entityPiglinBrute:
			adultPiglins++
			if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < nearD {
				nearestPiglin, nearD = o, d
			}
		case entityHoglin:
			adultHoglins++
		}
	})
	outnumbered := !m.baby && adultPiglins > adultHoglins+1
	if m.hogRetreat == 0 {
		switch {
		case m.baby && m.kb > 0:
			if t := h.nearestHuntable(players, m.dim, m.x, m.z, hoglinSeeRange); t != nil {
				m.hogRetreat = 100 + h.rng.Intn(301) // RETREAT_DURATION 5–20 s
				m.hogRetreatX, m.hogRetreatZ = t.x, t.z
			}
		case outnumbered && nearestPiglin != nil:
			m.hogRetreat = 100 + h.rng.Intn(301)
			m.hogRetreatX, m.hogRetreatZ = nearestPiglin.x, nearestPiglin.z
		}
	}
	if m.hogRetreat > 0 {
		m.hasTarget = false
		if nearestPiglin != nil && !m.baby {
			m.hogRetreatX, m.hogRetreatZ = nearestPiglin.x, nearestPiglin.z
		}
		if dist3(m.hogRetreatX, m.y, m.hogRetreatZ, m.x, m.y, m.z) < hoglinRetreatFrom {
			h.steerAwayFrom(m, m.hogRetreatX, m.hogRetreatZ, hoglinRetreatSpeed)
			m.rest = 0
			return true
		}
		return false
	}
	if m.hogRepelled && m.hogPacified > 0 {
		rx, rz := float64(m.hogRepellent.x)+0.5, float64(m.hogRepellent.z)+0.5
		if math.Hypot(rx-m.x, rz-m.z) < hoglinIdleFromPiglin {
			h.steerAwayFrom(m, rx, rz, 1.0) // SetWalkTargetAwayFrom.pos(NEAREST_REPELLENT, 1.0, 8)
			m.rest = 0
			return true
		}
	}
	if !m.baby && !m.hasTarget && nearestPiglin != nil && nearD < hoglinIdleFromPiglin {
		h.steerAwayFrom(m, nearestPiglin.x, nearestPiglin.z, hoglinIdleAwaySpeed)
		m.rest = 0
		return true
	}
	return false
}

// steerAwayFrom sets a walker's velocity straight away from (x, z).
func (h *hub) steerAwayFrom(m *mob, x, z, speed float64) {
	dx, dz := m.x-x, m.z-z
	d := math.Hypot(dx, dz)
	if d < 1e-6 {
		ang := h.rng.Float64() * 2 * math.Pi
		dx, dz, d = math.Cos(ang), math.Sin(ang), 1
	}
	sp := m.moveSpeed() * speed
	m.vx, m.vz = dx/d*sp, dz/d*sp
}
