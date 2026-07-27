package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The ender dragon's phase machine.
//
// `updateDragon` was a waypoint chase plus contact damage: the dragon circled,
// occasionally swooped, and could be beaten by standing still and swinging. It
// never breathed, never threw a fireball, and never perched — so the fight had
// none of its shape, and the bow was pointless because the head never came
// within reach.
//
// Vanilla drives the fight from a phase machine, and the phases are the fight:
// it circles, it decides to either strafe you with a fireball or come in to
// land, it perches on the exit portal where you can finally reach its head,
// it breathes over the podium to drive you off, and then it takes off again.
// The odds of it landing rise as the crystals come down, which is what makes
// destroying them the actual objective.

const (
	dragonHolding         = iota // circling the pillar ring
	dragonStrafing               // lining up a fireball on a player
	dragonLandingApproach        // heading for the portal
	dragonLanding                // dropping onto it
	dragonPerched                // sitting on the portal, head in reach
	dragonFlaming                // breathing over the podium
	dragonTakeoff                // climbing back into the circuit
)

const (
	dragonStrafeRangeSq = 4096.0 // 64 blocks: inside this it charges a fireball
	dragonFireballAim   = 10.0   // degrees of alignment needed to loose one
	dragonChargeTicks   = 5      // ticks of aim before the fireball leaves
	dragonPerchTicks    = 100    // how long it sits before deciding what next
	dragonFlameAt       = 10     // ticks into the flame phase the cloud appears
	dragonFlameCount    = 4      // flames before it gives up and takes off
	dragonTakeoffTicks  = 60
	// NOTE: no perched-damage multiplier. Vanilla makes perching dangerous by
	// bringing the HEAD within reach, not by scaling damage; tachyne has no
	// dragon parts, and inventing a multiplier would be a deviation dressed up
	// as a feature. Landing already gives the player their window.
)

// perchY is the height of the exit-portal podium the dragon lands on.
func (h *hub) perchY() float64 { return float64(worldgen.EndSurfaceY) + 1 }

// setDragonPhase moves the dragon into a phase and resets the phase clock.
func (h *hub) setDragonPhase(m *mob, phase int) {
	m.dragonPhase = phase
	m.dragonPhaseTick = 0
}

// updateDragonPhase runs one tick of the machine. It returns the point the
// dragon should be flying toward, and whether it is currently sitting still.
func (h *hub) updateDragonPhase(players map[int32]*tracked, m *mob) (tx, ty, tz float64, sitting bool) {
	m.dragonPhaseTick++
	switch m.dragonPhase {

	case dragonStrafing:
		t := h.dragonTarget(players, m)
		if t == nil {
			h.setDragonPhase(m, dragonHolding)
			return h.dragonCircuit(m)
		}
		// Vanilla climbs above the target by how far away it is, so the dragon
		// comes down at you rather than flying level through you.
		lift := math.Min(0.4+dist2(t.x, t.z, m.x, m.z)/80-1, 10)
		h.dragonChargeFireball(players, m, t)
		return t.x, t.y + lift, t.z, false

	case dragonLandingApproach:
		if dist2(m.x, m.z, 0, 0) < 12 {
			h.setDragonPhase(m, dragonLanding)
		}
		return 0.5, h.perchY() + 12, 0.5, false

	case dragonLanding:
		if m.y <= h.perchY()+1.5 {
			h.setDragonPhase(m, dragonPerched)
			m.dragonFlames = 0
		}
		return 0.5, h.perchY(), 0.5, false

	case dragonPerched:
		if m.dragonPhaseTick >= dragonPerchTicks {
			// Someone close enough to be worth breathing on keeps it sitting;
			// otherwise it gives up the perch and climbs away.
			if h.dragonTarget(players, m) != nil && m.dragonFlames < dragonFlameCount {
				h.setDragonPhase(m, dragonFlaming)
			} else {
				h.setDragonPhase(m, dragonTakeoff)
			}
		}
		return 0.5, h.perchY(), 0.5, true

	case dragonFlaming:
		if m.dragonPhaseTick == dragonFlameAt {
			m.dragonFlames++
			h.playSoundDim(players, 2, "minecraft:entity.ender_dragon.growl", sndHostile, m.x, m.y, m.z, 4, 1)
			h.spawnBreathCloud(2, 0.5, h.perchY(), 0.5)
		}
		if m.dragonPhaseTick >= breathTicks {
			if m.dragonFlames >= dragonFlameCount {
				h.setDragonPhase(m, dragonTakeoff)
			} else {
				h.setDragonPhase(m, dragonPerched)
			}
		}
		return 0.5, h.perchY(), 0.5, true

	case dragonTakeoff:
		if m.dragonPhaseTick >= dragonTakeoffTicks {
			h.setDragonPhase(m, dragonHolding)
		}
		return 0.5, h.perchY() + 24, 0.5, false
	}

	// Holding pattern: circle, and at each lap decide whether to break off.
	if m.dragonPhaseTick%80 == 0 {
		h.dragonDecide(players, m)
	}
	return h.dragonCircuit(m)
}

// dragonDecide is vanilla's findNewTarget roll: the fewer crystals left alive,
// the likelier it commits to landing — which is precisely why a player breaks
// the crystals first.
func (h *hub) dragonDecide(players map[int32]*tracked, m *mob) {
	crystals := len(h.crystals)
	if h.rng.Intn(crystals+3) == 0 {
		h.setDragonPhase(m, dragonLandingApproach)
		return
	}
	if t := h.dragonTarget(players, m); t != nil && h.rng.Intn(crystals+2) == 0 {
		h.setDragonPhase(m, dragonStrafing)
	}
}

// dragonCircuit is the lap it flies when nothing else is going on.
func (h *hub) dragonCircuit(m *mob) (tx, ty, tz float64, sitting bool) {
	ang := float64(h.tick.Load()%1200) / 1200
	return worldgen.EndPillarRing * 1.2 * cosTurn(ang),
		float64(worldgen.EndSurfaceY + 28),
		worldgen.EndPillarRing * 1.2 * sinTurn(ang), false
}

// dragonChargeFireball builds up an aimed shot and looses it once the dragon
// is both close enough and pointing at the target — a fireball only ever comes
// out of the front of its head.
func (h *hub) dragonChargeFireball(players map[int32]*tracked, m *mob, t *tracked) {
	if dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) > dragonStrafeRangeSq {
		if m.dragonCharge > 0 {
			m.dragonCharge--
		}
		return
	}
	m.dragonCharge++
	// The angle between where it is looking and where the target actually is.
	aim := math.Atan2(t.z-m.z, t.x-m.x)
	facing := float64(m.yaw+90) * math.Pi / 180
	off := math.Abs(math.Mod(math.Abs(aim-facing)+math.Pi, 2*math.Pi) - math.Pi)
	if m.dragonCharge < dragonChargeTicks || off*180/math.Pi > dragonFireballAim {
		return
	}
	m.dragonCharge = 0
	h.launchDragonFireball(players, m, t)
	h.setDragonPhase(m, dragonHolding)
}

// launchDragonFireball throws the shot that bursts into a breath cloud.
func (h *hub) launchDragonFireball(players map[int32]*tracked, m *mob, t *tracked) {
	dx, dy, dz := t.x-m.x, (t.y+0.5)-(m.y+0.5), t.z-m.z
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return
	}
	const v = 0.7
	a := h.launchProjectileIn(players, entityDragonFireball, 2, m.x, m.y+0.5, m.z,
		dx/d*v, dy/d*v, dz/d*v)
	a.shooter, a.dmg, a.breaks, a.breath = m.eid, 0, true, true
	h.playSoundDim(players, 2, "minecraft:entity.ender_dragon.shoot", sndHostile, m.x, m.y, m.z, 4, 1)
}

// dragonTarget is the player the dragon is interested in: the nearest one
// standing on the island in survival.
func (h *hub) dragonTarget(players map[int32]*tracked, m *mob) *tracked {
	var best *tracked
	bestD := math.MaxFloat64
	for _, t := range players {
		if t.dim != 2 || t.dead || t.gamemode != gmSurvival {
			continue
		}
		if d := dist2(t.x, t.z, m.x, m.z); d < bestD {
			best, bestD = t, d
		}
	}
	return best
}

// dist2 is horizontal distance.
func dist2(x1, z1, x2, z2 float64) float64 {
	dx, dz := x1-x2, z1-z2
	return math.Sqrt(dx*dx + dz*dz)
}
