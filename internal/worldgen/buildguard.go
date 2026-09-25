package worldgen

// Generated features and player builds, beyond trees.
//
// tachyne keeps only edits and regenerates terrain, so a feature added to
// generation lands in land players have already built on. treeguard.go
// keeps trees out of builds; this file is the same guard for every other
// feature, in two sizes:
//
//   - buildGuard.decorationBlocked, for a plant or a small patch cell: no
//     plant on a player's floor or path (the ground under it built), and
//     none under a player's roof (a build block above it in its column) —
//     that is what stops tall grass sprouting inside a house that stands on
//     natural grass. A garden open to the sky still grows.
//   - Generator.builtIn, for anything that moves terrain (a ravine, a lake,
//     an ice spike, a boulder): a build block anywhere in the feature's box
//     and it is not placed at all.
//
// "Build block" is isBuildBlock (treeguard.go): not air, fluid, fire or a
// replaceable plant — never the changes the world makes on its own.
//
// Every chunk pass asks the same question of the same edit overlay, so a
// feature straddling chunks is placed or skipped whole.

// buildRoofReach is how far above a plant the guard looks for a roof.
const buildRoofReach = 24

// buildGuard is one chunk pass's view of the builds around a chunk: the
// build-block heights per column over the chunk and a margin around it,
// read once so each plant is a map lookup.
type buildGuard struct {
	g    *Generator
	cols map[[2]int][]int // column → heights of its build blocks
	dug  map[[3]int]bool  // ground cells a player dug or built on (groundBuilt)
}

// newBuildGuard reads the edits of chunk (cx, cz) and its eight neighbours.
// With no edit overlay (tests, tools) it blocks nothing.
func (g *Generator) newBuildGuard(cx, cz int32) *buildGuard {
	bg := &buildGuard{g: g}
	if g.editsIn == nil {
		return bg
	}
	bg.cols, bg.dug = map[[2]int][]int{}, map[[3]int]bool{}
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			g.editsIn(cx+dx, cz+dz, func(x, y, z int, s uint32) {
				if isBuildBlock(s) {
					k := [2]int{x, z}
					bg.cols[k] = append(bg.cols[k], y)
				}
				if groundBuilt(s) {
					bg.dug[[3]int{x, y, z}] = true
				}
			})
		}
	}
	return bg
}

// decorationBlocked reports whether a plant at (x, y, z) would stand on a
// player's floor or under a player's roof.
func (bg *buildGuard) decorationBlocked(x, y, z int) bool {
	if bg == nil || bg.cols == nil {
		return false
	}
	if bg.dug[[3]int{x, y - 1, z}] {
		return true
	}
	for _, by := range bg.cols[[2]int{x, z}] {
		if by >= y && by <= y+buildRoofReach {
			return true
		}
	}
	return false
}

// builtIn reports whether any build block lies in the box (inclusive) —
// for features that reshape terrain, which are skipped whole if so.
func (g *Generator) builtIn(x0, y0, z0, x1, y1, z1 int) bool {
	if g.editsIn == nil {
		return false
	}
	if x0 > x1 {
		x0, x1 = x1, x0
	}
	if z0 > z1 {
		z0, z1 = z1, z0
	}
	found := false
	for cx := int32(floorDiv16(x0)); cx <= int32(floorDiv16(x1)) && !found; cx++ {
		for cz := int32(floorDiv16(z0)); cz <= int32(floorDiv16(z1)) && !found; cz++ {
			g.editsIn(cx, cz, func(x, y, z int, s uint32) {
				if !found && x >= x0 && x <= x1 && y >= y0 && y <= y1 && z >= z0 && z <= z1 && isBuildBlock(s) {
					found = true
				}
			})
		}
	}
	return found
}

func floorDiv16(v int) int {
	if v < 0 {
		return (v - 15) / 16
	}
	return v / 16
}
