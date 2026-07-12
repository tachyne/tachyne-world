package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Rails (tier: vehicles). Plain rails bend into corners and slopes; powered/
// detector/activator rails only run straight or ascending. Shapes are
// recomputed from neighbouring rails on placement and on every neighbour
// update, which is deterministic, so the network converges. Powered and
// activator rails sync their `powered` bit from redstone; detector rails are
// pressed by carts (vehicle commit).
//
// State math (inlined, like plates): rail = shape(10) × waterlogged(2);
// the special rails = powered(2) × shape(6) × waterlogged(2), bools true-first.

var (
	poweredRailMin  = worldgen.BlockBase("powered_rail")
	poweredRailMax  = worldgen.BlockBase("powered_rail") + 23
	detectorRailMin = worldgen.BlockBase("detector_rail")
	detectorRailMax = worldgen.BlockBase("detector_rail") + 23
	railMin         = worldgen.BlockBase("rail")
	railMax         = worldgen.BlockBase("rail") + 19
	activatorMin    = worldgen.BlockBase("activator_rail")
	activatorMax    = worldgen.BlockBase("activator_rail") + 23
)

// Rail shape ordinals (shared prefix across all rail types).
const (
	shapeNS = iota
	shapeEW
	shapeAscE
	shapeAscW
	shapeAscN
	shapeAscS
	shapeSE // plain rail only
	shapeSW
	shapeNW
	shapeNE
)

func isPlainRail(s uint32) bool { return s >= railMin && s <= railMax }
func isSpecialRail(s uint32) bool {
	return (s >= poweredRailMin && s <= detectorRailMax) || (s >= activatorMin && s <= activatorMax)
}
func isAnyRail(s uint32) bool { return isPlainRail(s) || isSpecialRail(s) }

func isDetectorRail(s uint32) bool { return s >= detectorRailMin && s <= detectorRailMax }

// railShape reads the shape ordinal of any rail state.
func railShape(s uint32) int {
	if isPlainRail(s) {
		return int(s-railMin) / 2
	}
	base := specialBase(s)
	return int(s-base) % 12 / 2
}

// railPowered reads the powered bit of a special rail.
func railPowered(s uint32) bool { return isSpecialRail(s) && (s-specialBase(s))/12 == 0 }

func specialBase(s uint32) uint32 {
	switch {
	case s >= poweredRailMin && s <= poweredRailMax:
		return poweredRailMin
	case s >= detectorRailMin && s <= detectorRailMax:
		return detectorRailMin
	}
	return activatorMin
}

// railWith rebuilds a rail state with a shape (and powered bit for specials),
// preserving the family. Corners degrade to straight on special rails.
func railWith(s uint32, shape int, powered bool) uint32 {
	if isPlainRail(s) {
		return railMin + uint32(shape)*2 + 1 // waterlogged=false
	}
	if shape > shapeAscS {
		switch shape {
		case shapeSE, shapeNE:
			shape = shapeEW
		default:
			shape = shapeNS
		}
	}
	base := specialBase(s)
	p := uint32(12) // powered=false half
	if powered {
		p = 0
	}
	return base + p + uint32(shape)*2 + 1
}

// computeRailShape picks a rail's shape from its neighbours: prefer two-way
// connections (straights, then corners for plain rails), then one-way
// (ascending toward a rail one block up), defaulting to the placer's axis.
func (h *hub) computeRailShape(x, y, z int, state uint32, defAxis int) int {
	// Connectivity per cardinal: 0 none, 1 level, 2 one up.
	conn := [4]int{} // E, W, N, S
	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, -1}, {0, 1}}
	for i, d := range dirs {
		switch {
		case isAnyRail(h.world.At(x+d[0], y, z+d[1])),
			isAnyRail(h.world.At(x+d[0], y-1, z+d[1])): // neighbour slopes down to us
			conn[i] = 1
		case isAnyRail(h.world.At(x+d[0], y+1, z+d[1])):
			conn[i] = 2
		}
	}
	e, w, n, s := conn[0], conn[1], conn[2], conn[3]
	switch {
	case e == 2:
		return shapeAscE
	case w == 2:
		return shapeAscW
	case n == 2:
		return shapeAscN
	case s == 2:
		return shapeAscS
	case (e > 0 || w > 0) && n == 0 && s == 0:
		return shapeEW
	case (n > 0 || s > 0) && e == 0 && w == 0:
		return shapeNS
	// Corners (plain rails only; specials degrade in railWith).
	case s > 0 && e > 0:
		return shapeSE
	case s > 0 && w > 0:
		return shapeSW
	case n > 0 && w > 0:
		return shapeNW
	case n > 0 && e > 0:
		return shapeNE
	case e > 0 || w > 0:
		return shapeEW
	case n > 0 || s > 0:
		return shapeNS
	}
	return defAxis
}

// updateRail is the scheduled step: re-shape from neighbours, and sync the
// powered bit (powered/activator rails; detector rails are cart-driven).
func (h *hub) updateRail(players map[int32]*tracked, pos blockPos, state uint32) {
	shape := h.computeRailShape(pos.x, pos.y, pos.z, state, railShape(state))
	powered := railPowered(state)
	if isSpecialRail(state) && !isDetectorRail(state) {
		powered = h.inputPower(pos.x, pos.y, pos.z, false) > 0
	}
	if ns := railWith(state, shape, powered); ns != state {
		h.setBlock(players, pos, ns)
		h.scheduleAround(pos, 1)
	}
}

// placeRailShape orients a just-placed rail: connect to neighbours, else lie
// along the player's look axis.
func (h *hub) placeRailShape(x, y, z int, state uint32, yaw float32) uint32 {
	axis := shapeNS
	f := playerFacing(yaw)
	if f == "east" || f == "west" {
		axis = shapeEW
	}
	return railWith(state, h.computeRailShape(x, y, z, state, axis), railPowered(state))
}
