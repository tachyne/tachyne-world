package worldgen

// Generated trees and player builds.
//
// Vanilla decorates a chunk once and keeps it, so a worldgen tree never meets
// a building: whatever a player builds, they build around trees already
// there. tachyne keeps only the player's edits and generates the terrain
// again, so when generation changes (a new tree placer, a new biome), a tree
// can grow where a player built. The castle's walls and roof are edits and
// win, but the air inside is generated, and the tree fills it.
//
// So a generated tree that collides with a build is not grown. The test is
// the tree's own footprint against the edit overlay:
//
//   - an edit on the ground cell under its trunk (a floor, a path, a dug
//     hole) or on any cell its trunk or branches would occupy (a roof or a
//     wall it would pass through, or a log a player already chopped);
//   - or a player block where at least a quarter of its leaves would go (a
//     roof or wall cutting through the canopy). A block or two placed in a
//     canopy (a torch, a bridge) does not count.
//
// Every chunk the tree reaches draws the same tree and asks the same
// question of the same global edit overlay, so they all agree and no half
// tree is left. The footprint is what the placer ATTEMPTS, which is the same
// in every pass: trunk and leaf placement is decided from the terrain model,
// not from the chunk buffer.

// treeFootprint collects a tree's attempted writes during a stamping pass.
type treeFootprint struct {
	logs   map[[3]int]bool
	leaves map[[3]int]bool
}

func newTreeFootprint() *treeFootprint {
	return &treeFootprint{logs: map[[3]int]bool{}, leaves: map[[3]int]bool{}}
}

// note records one attempted write: logs are the trunk and branches, leaves
// the canopy. Decorations (vines, litter, moss, roots) are ignored — they are
// placed by reads that can differ between chunk passes, and a torch in the
// grass should not remove a tree.
func (f *treeFootprint) note(x, y, z int, state uint32, leaf bool) {
	switch {
	case leaf && IsLeaves(state):
		f.leaves[[3]int{x, y, z}] = true
	case !leaf && IsLog(state):
		f.logs[[3]int{x, y, z}] = true
	}
}

// collidesWithBuild reports whether a generated tree rooted at (x, y, z)
// (the first cell of its trunk) would grow into a player's build.
func (g *Generator) collidesWithBuild(f *treeFootprint, x, y, z int) bool {
	if g.editAt == nil {
		return false
	}
	if _, ok := g.editAt(x, y-1, z); ok {
		return true
	}
	for p := range f.logs {
		if _, ok := g.editAt(p[0], p[1], p[2]); ok {
			return true
		}
	}
	if len(f.leaves) == 0 {
		return false
	}
	hits := 0
	for p := range f.leaves {
		if s, ok := g.editAt(p[0], p[1], p[2]); ok && s != Air {
			hits++
		}
	}
	return hits*4 >= len(f.leaves)
}

// groundEdited reports an edit on the ground cell under a feature's origin —
// the guard for features whose footprint is not the same in every chunk pass
// (huge mushrooms, fallen trees): one standing on a player's floor or path
// is not placed.
func (g *Generator) groundEdited(x, y, z int) bool {
	if g.editAt == nil {
		return false
	}
	_, ok := g.editAt(x, y-1, z)
	return ok
}
