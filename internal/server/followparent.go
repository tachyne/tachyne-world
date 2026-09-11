package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Baby animals follow a parent (vanilla FollowParentGoal, and the brain
// mobs' BabyFollowAdult): a baby looks for the nearest adult of its kind
// within 8 blocks sideways and 4 up or down, and if it is more than three
// blocks off (five for the brain species) walks to it at the species'
// speed, giving up past sixteen blocks or once it is close again. The
// search repeats every ten ticks while the goal runs.

const (
	followParentScanXZ = 8.0
	followParentScanY  = 4.0
	followParentStop   = 3.0  // DONT_FOLLOW_IF_CLOSER_THAN
	followAdultStop    = 5.0  // BabyFollowAdult's ADULT_FOLLOW_RANGE minimum
	followParentGiveUp = 16.0 // both give up beyond this
	followParentRecalc = 10 / mobMoveInterval
)

// followParentSpeed is the goal's speed modifier per species; a species not
// listed has no such goal (rabbits, wolves, cats, foxes, turtles, frogs and
// sniffers use other behaviours in vanilla).
var followParentSpeed = map[int]float64{
	entityCow: 1.25, entityMooshroom: 1.25, entityBee: 1.25, entityChicken: 1.1,
	entityHorse: 1.0, entityDonkey: 1.0, entityMule: 1.0, entityZombieHorse: 1.0, entitySkeletonHorse: 1.0,
	entityLlama: 1.0, entityTraderLlama: 1.0, entityPanda: 1.25, entityPig: 1.1, entityPolarBear: 1.25,
	entitySheep: 1.1, entityStrider: 1.0,
	// Brain-driven: BabyFollowAdult with ADULT_FOLLOW_RANGE 5..16. The
	// axolotl's speed depends on where it is (0.6 in water, 0.15 ashore).
	entityGoat: 1.25, entityAxolotl: 0.15, entityArmadillo: 1.25, entityCamel: 2.5, entityHoglin: 0.6,
}

var followAdultBrain = map[int]bool{
	entityGoat: true, entityAxolotl: true, entityArmadillo: true, entityCamel: true, entityHoglin: true,
}

// followParentStep steers a baby toward its parent for this update and
// reports whether it is doing so.
func (h *hub) followParentStep(m *mob) bool {
	speed, ok := followParentSpeed[m.etype]
	if !ok {
		return false
	}
	stop := followParentStop
	if followAdultBrain[m.etype] {
		stop = followAdultStop
	}
	if m.parentRecalc > 0 {
		m.parentRecalc--
	}
	p := h.mobs[m.parent]
	if p == nil || p.baby || p.dying > 0 || p.dim != m.dim {
		m.parent = 0
		if m.parentRecalc > 0 {
			return false
		}
		m.parentRecalc = followParentRecalc
		best := math.MaxFloat64
		h.grid().nearby(m.dim, m.x, m.z, followParentScanXZ, func(o *mob) {
			if o == m || o.etype != m.etype || o.baby || o.dying > 0 || math.Abs(o.y-m.y) > followParentScanY {
				return
			}
			if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < best {
				best, p = d, o
			}
		})
		if p == nil || best < stop {
			return false
		}
		m.parent = p.eid
	}
	d := dist3(p.x, p.y, p.z, m.x, m.y, m.z)
	if d < stop || d > followParentGiveUp {
		m.parent = 0
		return false
	}
	dx, dz := p.x-m.x, p.z-m.z
	hd := math.Hypot(dx, dz)
	if hd < 1e-6 {
		return false
	}
	if m.etype == entityAxolotl {
		if w := h.worldFor(m.dim); w != nil && worldgen.HoldsWater(w.At(int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z)))) {
			speed = 0.6
		}
	}
	sp := m.moveSpeed() * speed
	m.vx, m.vz = dx/hd*sp, dz/hd*sp
	m.rest = 0
	return true
}
