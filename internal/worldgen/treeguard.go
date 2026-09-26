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
//   - a floor, a path or a dug hole on the ground cell under its trunk (not
//     soil the world turned from grass to dirt and back: groundBuilt), or a
//     player block on any cell its trunk or branches would
//     occupy (a roof or a wall it would pass through). Air there is not a
//     build: it is where a player chopped this tree's logs, and a chopped
//     tree keeps standing as the player left it, as in vanilla — skipping
//     it took the rest of its trunk, its leaves and the vines on them.
//     Nor is fire, lava, water or a plant (isBuildBlock): a tree that
//     caught fire left fire and lava in its cells, and the guard read them
//     as a build and dropped the tree, leaving the fire, the lava and its
//     bee nest hanging in the air (bugs #19-21);
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
	if s, ok := g.editAt(x, y-1, z); ok && groundBuilt(s) {
		return true
	}
	for p := range f.logs {
		if s, ok := g.editAt(p[0], p[1], p[2]); ok && isBuildBlock(s) {
			return true
		}
	}
	if len(f.leaves) == 0 {
		return false
	}
	hits := 0
	for p := range f.leaves {
		if s, ok := g.editAt(p[0], p[1], p[2]); ok && isBuildBlock(s) {
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
	s, ok := g.editAt(x, y-1, z)
	return ok && groundBuilt(s)
}

// groundBuilt reports whether an edit on the ground under a trunk is a
// player's doing: a dug hole (air) or a floor or path (a build block that is
// not natural soil). Soil the world changed by itself is not. Grass under a
// trunk decays to dirt and spreads back, each change saved as an edit, and
// counting those dropped almost every tree near anyone who had been about.
func groundBuilt(s uint32) bool {
	return s == Air || (isBuildBlock(s) && !IsDirtTag(s))
}

// isBuildBlock reports whether an edit is something a player built with, as
// opposed to what the world did on its own: air, a fluid, fire, or a plant
// or anything else vanilla counts as replaceable (fire and soul fire are
// replaceable in vanilla too; IsReplaceable leaves them to the fire code).
func isBuildBlock(s uint32) bool {
	return s != Air && !IsReplaceable(s) && !IsFluid(s) && !fireStates[s]
}

// PlayerBuilt reports whether an edit is the work of a player building
// rather than a tree or the ground: a build block that is not a leaf, a log
// or a soil block (grass spreads and dirt reverts as edits of their own).
// It tells a hedge, which stands among a player's lanterns and walls, from
// the canopy of a felled tree, which stands among nothing.
func PlayerBuilt(s uint32) bool {
	return isBuildBlock(s) && !IsDirtTag(s) && !notBuilt[s]
}

// notBuilt is what trees, plants and weather leave as edits without a
// player: every leaf (#leaves) and trunk block (#prevents_nearby_leaf_decay),
// the plants and fungi that grow wild beside them, and the snow and ice that
// fall and freeze.
var notBuilt = func() map[uint32]bool {
	m := map[uint32]bool{}
	for _, tag := range []string{"leaves", "prevents_nearby_leaf_decay"} {
		for _, r := range BlockTag(tag) {
			for s := r[0]; s <= r[1]; s++ {
				m[s] = true
			}
		}
	}
	for _, name := range []string{"snow_block", "ice", "powder_snow",
		"sweet_berry_bush", "brown_mushroom", "red_mushroom", "brown_mushroom_block", "red_mushroom_block",
		"mushroom_stem", "cactus", "cactus_flower", "sugar_cane", "bamboo", "bamboo_sapling", "pumpkin",
		"melon", "azalea", "flowering_azalea", "spore_blossom", "cocoa", "bee_nest", "moss_block",
		"pale_moss_block", "cave_vines", "cave_vines_plant", "big_dripleaf", "big_dripleaf_stem",
		"small_dripleaf", "dandelion", "golden_dandelion", "open_eyeblossom", "closed_eyeblossom", "poppy",
		"blue_orchid", "allium", "azure_bluet", "red_tulip", "orange_tulip", "white_tulip", "pink_tulip",
		"oxeye_daisy", "cornflower", "lily_of_the_valley", "wither_rose", "torchflower", "sunflower",
		"lilac", "rose_bush", "peony", "pitcher_plant", "lily_pad", "oak_sapling", "spruce_sapling",
		"birch_sapling", "jungle_sapling", "acacia_sapling", "dark_oak_sapling", "cherry_sapling",
		"pale_oak_sapling", "poplar_sapling", "mangrove_propagule", "sulfur", "cinnabar"} {
		if lo, hi, ok := BlockRangeOK(name); ok {
			for s := lo; s <= hi; s++ {
				m[s] = true
			}
		}
	}
	return m
}()

var fireStates = func() map[uint32]bool {
	m := map[uint32]bool{}
	for _, name := range []string{"fire", "soul_fire"} {
		if lo, hi, ok := BlockRangeOK(name); ok {
			for s := lo; s <= hi; s++ {
				m[s] = true
			}
		}
	}
	return m
}()
