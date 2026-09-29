package server

import (
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

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
// preserving the family and the water. Corners degrade to straight on
// special rails.
func railWith(s uint32, shape int, powered bool) uint32 {
	if isPlainRail(s) {
		return railMin + uint32(shape)*2 + (s-railMin)%2
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
	return base + p + uint32(shape)*2 + (s-base)%2
}

// updateRail is the rail's neighborChanged (railstate.go does the work).
func (h *hub) updateRail(players map[int32]*tracked, pos blockPos, state uint32) {
	h.railNeighborChanged(players, pos, state)
}

// placeRailShape is BaseRailBlock.getStateForPlacement: the rail lies along
// the placer's horizontal facing; onPlace (railOnPlace, on the hub) then
// connects it to the rails around.
func (h *hub) placeRailShape(_ *world.World, _, _, _ int, state uint32, yaw float32) uint32 {
	axis := shapeNS
	if f := playerFacing(yaw); f == "east" || f == "west" {
		axis = shapeEW
	}
	return railWith(state, axis, railPowered(state))
}
