package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Parrot.ParrotWanderGoal (WaterAvoidingRandomFlyingGoal, 1.0): a parrot's
// only wandering. Once in sixty goal checks it picks somewhere to fly —
// from the water, dry land within fifteen; otherwise, all but one time in a
// thousand, a perch on a tree (leaves or a log under two cells of air)
// within three across and six up or down; failing that, a spot one to three
// above the ground ahead of it (HoverRandomPos). Between flights it is not
// held aloft: FlyingMoveControl lets gravity have a parrot that is not
// moving, so it settles on whatever is under it — the branch it flew to.

// logRanges is #logs: every log and wood, stripped or not, and the
// Nether's stems and hyphae (#logs_that_burn + #crimson_stems +
// #warped_stems).
var logRanges = func() [][2]uint32 {
	var out [][2]uint32
	add := func(name string) {
		if lo, hi, ok := worldgen.BlockRangeOK(name); ok {
			out = append(out, [2]uint32{lo, hi})
		}
	}
	for _, w := range []string{"oak", "spruce", "birch", "jungle", "acacia", "dark_oak", "pale_oak", "mangrove", "cherry", "poplar"} {
		add(w + "_log")
		add(w + "_wood")
		add("stripped_" + w + "_log")
		add("stripped_" + w + "_wood")
	}
	for _, w := range []string{"crimson", "warped"} {
		add(w + "_stem")
		add(w + "_hyphae")
		add("stripped_" + w + "_stem")
		add("stripped_" + w + "_hyphae")
	}
	return out
}()

const (
	parrotWanderInterval = 120 / 2 / mobMoveInterval // reducedTickDelay(120), checked each goal tick
	parrotTreeReachXZ    = 3
	parrotTreeReachY     = 6
	parrotWanderProb     = 0.001 // WaterAvoidingRandomStrollGoal.PROBABILITY
	parrotWanderGiveUp   = 200 / mobMoveInterval
)

// parrotWanderStep flies a wandering parrot on to its point, or starts a
// new wander. It reports whether it holds the parrot.
func (h *hub) parrotWanderStep(m *mob) bool {
	if m.sitting || m.mount != 0 || m.cart != 0 || (m.tamed && m.hasTarget) {
		m.parrotWandering = false
		return false
	}
	if !m.parrotWandering {
		if h.rng.Intn(parrotWanderInterval) != 0 {
			return false
		}
		p, ok := h.parrotWanderPos(m)
		if !ok {
			return false
		}
		m.parrotWander, m.parrotWandering, m.parrotWanderLeft = p, true, parrotWanderGiveUp
		m.parrotFollow = 0 // the wander (priority 2) takes the MOVE flag from FollowMobGoal
	}
	tx, ty, tz := m.parrotWander.x, m.parrotWander.y, m.parrotWander.z
	m.parrotWanderLeft--
	if dist3sq(tx, ty, tz, m.x, m.y, m.z) < 1 || m.parrotWanderLeft <= 0 {
		m.parrotWandering = false
		m.flyClearPath()
		m.vx, m.vz = 0, 0
		return false
	}
	speed := m.moveSpeed()
	if h.flyTo(m, blockPos{floorInt(tx), floorInt(ty), floorInt(tz)}) {
		m.vx, m.vz = flySteer(m, speed)
	} else {
		m.vx, m.vz = straightSteer(m, tx, tz, 0)
	}
	m.flyAim(h.tick.Load(), ty)
	if m.vx != 0 || m.vz != 0 {
		m.yaw = yawToward(m.x, m.z, tx, tz)
		m.headYaw = m.yaw
	}
	m.rest = 0
	return true
}

// parrotSettle is the parrot with no goal moving it: it stops and sinks to
// the floor under it (a leaf, a log, the ground) instead of strolling.
func (h *hub) parrotSettle(m *mob) bool {
	if m.sitting || m.panic > 0 || (m.tamed && m.hasTarget) { // FollowOwnerGoal has it
		return false
	}
	w := h.worldFor(m.dim)
	m.flyClearPath()
	m.vx, m.vz = m.vx*0.6, m.vz*0.6
	m.flyAim(h.tick.Load(), float64(w.MobFeetFrom(floorInt(m.x), floorInt(m.z), floorInt(m.y))))
	return true
}

// parrotWanderPos is ParrotWanderGoal.getPosition.
type parrotPoint struct{ x, y, z float64 }

func (h *hub) parrotWanderPos(m *mob) (parrotPoint, bool) {
	w := h.worldFor(m.dim)
	var p parrotPoint
	ok := false
	if worldgen.HoldsWater(w.At(floorInt(m.x), floorInt(m.y), floorInt(m.z))) {
		if x, z, found := h.landRandomPos(m, 15, 15); found {
			p = parrotPoint{x, float64(w.MobFeetFrom(floorInt(x), floorInt(z), floorInt(m.y))), z}
			ok = true
		}
	}
	if h.rng.Float32() >= parrotWanderProb {
		if tp, found := h.parrotTreePos(m); found {
			p, ok = tp, true
		}
	}
	if ok {
		return p, true
	}
	return h.hoverRandomPos(m)
}

// parrotTreePos is ParrotWanderGoal.getTreePos: the first cell, in
// BlockPos.betweenClosed order (x, then y, then z), that is not the
// parrot's own, is air with air above, and stands on leaves or a log.
func (h *hub) parrotTreePos(m *mob) (parrotPoint, bool) {
	w := h.worldFor(m.dim)
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	for z := floorInt(m.z - parrotTreeReachXZ); z <= floorInt(m.z+parrotTreeReachXZ); z++ {
		for y := floorInt(m.y - parrotTreeReachY); y <= floorInt(m.y+parrotTreeReachY); y++ {
			for x := floorInt(m.x - parrotTreeReachXZ); x <= floorInt(m.x+parrotTreeReachXZ); x++ {
				if x == bx && y == by && z == bz {
					continue
				}
				below := w.At(x, y-1, z)
				if !inRanges2(below, leafRanges) && !inRanges2(below, logRanges) {
					continue
				}
				if isAnyAir(w.At(x, y, z)) && isAnyAir(w.At(x, y+1, z)) {
					return parrotPoint{float64(x) + 0.5, float64(y), float64(z) + 0.5}, true
				}
			}
		}
	}
	return parrotPoint{}, false
}

// hoverRandomPos is HoverRandomPos.getPos(mob, 8, 7, view, π/2, 3, 1): ten
// tries at a point within eight across and seven up or down, inside a
// half-turn about where the mob faces, lifted out of anything solid and
// then set one to three above the ground beneath it.
func (h *hub) hoverRandomPos(m *mob) (parrotPoint, bool) {
	w := h.worldFor(m.dim)
	yaw := float64(m.yaw) * math.Pi / 180
	vx, vz := -math.Sin(yaw), math.Cos(yaw)
	for i := 0; i < 10; i++ {
		ang := math.Atan2(vz, vx) - math.Pi/2 + (2*h.rng.Float64()-1)*(math.Pi/2)
		dist := math.Sqrt(h.rng.Float64()) * math.Sqrt2 * 8
		x := floorInt(m.x + -dist*math.Sin(ang))
		z := floorInt(m.z + dist*math.Cos(ang))
		y := floorInt(m.y) + h.rng.Intn(2*7+1) - 7
		if !w.Loaded(int32(x>>4), int32(z>>4)) {
			continue
		}
		for n := 0; n < 16 && worldgen.Collides(w.At(x, y, z)); n++ {
			y++ // moveUpOutOfSolid
		}
		if worldgen.Collides(w.At(x, y, z)) {
			continue
		}
		ground := w.MobFeetFrom(x, z, y)
		above := y - ground
		switch {
		case above > 3:
			y = ground + 3
		case above < 1:
			y = ground + 1
		}
		if worldgen.Collides(w.At(x, y, z)) || worldgen.HoldsWater(w.At(x, y, z)) {
			continue
		}
		return parrotPoint{float64(x) + 0.5, float64(y), float64(z) + 0.5}, true
	}
	return parrotPoint{}, false
}
