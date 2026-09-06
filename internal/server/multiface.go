package server

import (
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Multiface blocks and scaffolding — the two survival rules worldgen's
// support shapes deliberately leave out.
//
// Vines, glow lichen, sculk veins and resin clumps carry one boolean per
// face (MultifaceBlock / VineBlock): a face is valid while the neighbour on
// that side offers a full face to attach to; a vine's side face also
// survives when the vine above carries the same face (that is how a vine
// hangs down from a tree). A block that loses its last face drops. Placing
// one sets the face toward the block that was clicked.
//
// Scaffolding (ScaffoldingBlock) carries its distance from support: 0 on a
// sturdy top face, otherwise one more than the least of the scaffold below
// (its own distance) and the four beside it (theirs + 1), capped at 7 —
// and at 7 it falls. `bottom` marks the lowest scaffold of a hanging run.

var (
	vineRange        = blockRange("vine")
	multifaceRanges  = blockRange("vine", "glow_lichen", "sculk_vein", "resin_clump")
	scaffoldingRange = blockRange("scaffolding")
)

func isVineBlock(s uint32) bool   { return inRanges2(s, vineRange) }
func isMultiface(s uint32) bool   { return inRanges2(s, multifaceRanges) }
func isScaffolding(s uint32) bool { return inRanges2(s, scaffoldingRange) }

// faceDirs are the six face properties and the neighbour each attaches to.
var faceDirs = []struct {
	prop string
	d    [3]int
}{
	{"down", [3]int{0, -1, 0}}, {"up", [3]int{0, 1, 0}},
	{"north", [3]int{0, 0, -1}}, {"south", [3]int{0, 0, 1}},
	{"west", [3]int{-1, 0, 0}}, {"east", [3]int{1, 0, 0}},
}

// faceTowardClicked is the face property a multiface block placed against
// clicked face `dir` uses: the block sits on the far side of that face, so
// it attaches back through the opposite one (clicked the north face →
// the new block's south face touches the clicked block).
func faceTowardClicked(dir int32) string {
	switch dir {
	case 0:
		return "up" // placed under a block: hangs from it
	case 1:
		return "down"
	case 2:
		return "south"
	case 3:
		return "north"
	case 4:
		return "east"
	case 5:
		return "west"
	}
	return ""
}

// orientMultiface sets the placed block's attaching face; a vine cannot sit
// on a floor (VineBlock rejects DOWN), so a vine placed on a top face gets no
// face and the caller's support check refuses the placement.
func orientMultiface(info worldgen.BlockInfo, state uint32, dir int32) uint32 {
	face := faceTowardClicked(dir)
	if face == "" || !info.HasProperty(face) {
		return state
	}
	return worldgen.SetProperty(info, state, face, "true")
}

// multifaceUpdated is VineBlock.getUpdatedState / MultifaceBlock's
// updateShape: every set face is re-checked against its neighbour, and the
// surviving state is returned (ok=false when no face is left).
func multifaceUpdated(w *world.World, pos blockPos, state uint32) (uint32, bool) {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return state, true
	}
	vine := isVineBlock(state)
	var above uint32
	haveAbove := false
	any := false
	for _, f := range faceDirs {
		if !info.HasProperty(f.prop) || worldgen.GetProperty(info, state, f.prop) != "true" {
			continue
		}
		n := w.At(pos.x+f.d[0], pos.y+f.d[1], pos.z+f.d[2])
		keep := holdsBlock(n)
		if !keep && vine && f.d[1] == 0 { // a side face may hang from the vine above
			if !haveAbove {
				above, haveAbove = w.At(pos.x, pos.y+1, pos.z), true
			}
			if isVineBlock(above) {
				if ai, ok := worldgen.InfoForState(above); ok && worldgen.GetProperty(ai, above, f.prop) == "true" {
					keep = true
				}
			}
		}
		if keep {
			any = true
		} else {
			state = worldgen.SetProperty(info, state, f.prop, "false")
		}
	}
	return state, any
}

// scaffoldDistance is ScaffoldingBlock.getDistance.
func scaffoldDistance(w *world.World, pos blockPos) int {
	below := w.At(pos.x, pos.y-1, pos.z)
	dist := 7
	if isScaffolding(below) {
		if bi, ok := worldgen.InfoForState(below); ok {
			dist = atoi(worldgen.GetProperty(bi, below, "distance"))
		}
	} else if worldgen.IsSturdyTop(below) {
		return 0
	}
	for _, d := range [4][3]int{{0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		n := w.At(pos.x+d[0], pos.y, pos.z+d[2])
		if isScaffolding(n) {
			if ni, ok := worldgen.InfoForState(n); ok {
				if nd := atoi(worldgen.GetProperty(ni, n, "distance")) + 1; nd < dist {
					dist = nd
				}
			}
			if dist == 1 {
				break
			}
		}
	}
	return dist
}

// scaffoldUpdated recomputes a scaffold's distance and bottom flag; ok=false
// when it has lost all support (distance 7) and must fall.
func scaffoldUpdated(w *world.World, pos blockPos, state uint32) (uint32, bool) {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return state, true
	}
	dist := scaffoldDistance(w, pos)
	bottom := dist > 0 && !isScaffolding(w.At(pos.x, pos.y-1, pos.z))
	state = worldgen.SetProperty(info, state, "distance", itoa(dist))
	state = worldgen.SetProperty(info, state, "bottom", map[bool]string{true: "true", false: "false"}[bottom])
	return state, dist < 7
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
