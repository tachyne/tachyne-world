package worldgen

import (
	"math/rand"
	"testing"
)

// The ported tree decorators: vines and cocoa. What a bare placer cannot
// express — a jungle tree IS its vines — and the split the aliasing hid:
// jungle_tree and jungle_tree_no_vine place the same trunk and differ only
// here, worldgen growing the first and saplings the second.

func grownStates(name string, seed int64) map[uint32]int {
	c := TreeFeatures[name]
	out := map[uint32]int{}
	rng := rand.New(rand.NewSource(seed))
	set := func(x, y, z int, state uint32, leaf bool) { out[state]++ }
	PlaceTree(c, 0, 0, 0, rng, TreeDriver{
		Set:        set,
		Free:       func(x, y, z int) bool { return true },
		Read:       func(x, y, z int) uint32 { return Air },
		DirtGround: func(x, y, z int) bool { return false },
		SurfaceTop: func(x, z int) int { return -999 },
	})
	return out
}

func countIn(states map[uint32]int, lo, hi uint32) int {
	n := 0
	for s, k := range states {
		if s >= lo && s <= hi {
			n += k
		}
	}
	return n
}

func TestJungleTreesGrowVinesAndCocoa(t *testing.T) {
	vlo, vhi := BlockRange("vine")
	clo, chi := BlockRange("cocoa")
	vines, cocoa := 0, 0
	for seed := int64(1); seed <= 40; seed++ {
		st := grownStates("jungle_tree", seed)
		vines += countIn(st, vlo, vhi)
		cocoa += countIn(st, clo, chi)
	}
	if vines == 0 {
		t.Error("forty jungle trees grew no vines at all")
	}
	if cocoa == 0 {
		t.Error("forty jungle trees grew no cocoa (gate is 0.2 — expect some)")
	}
	// …and the sapling-grown variant never does either.
	for seed := int64(1); seed <= 40; seed++ {
		st := grownStates("jungle_tree_no_vine", seed)
		if countIn(st, vlo, vhi)+countIn(st, clo, chi) > 0 {
			t.Fatalf("jungle_tree_no_vine grew a vine or cocoa (seed %d)", seed)
		}
	}
}

func TestSwampOaksHangVines(t *testing.T) {
	if _, ok := TreeFeatures["swamp_oak"]; !ok {
		t.Fatal("swamp_oak missing from the feature table")
	}
	vlo, vhi := BlockRange("vine")
	vines := 0
	for seed := int64(1); seed <= 40; seed++ {
		vines += countIn(grownStates("swamp_oak", seed), vlo, vhi)
	}
	if vines == 0 {
		t.Error("forty swamp oaks grew no vines")
	}
	// Plain oaks never do.
	for seed := int64(1); seed <= 40; seed++ {
		if countIn(grownStates("oak", seed), vlo, vhi) > 0 {
			t.Fatalf("a plain oak grew a vine (seed %d)", seed)
		}
	}
}

// grownWorld records a whole tree by position, over an optional pre-seeded
// ground, so decorators that read the world (podzol's #dirt check) see it.
func grownWorld(name string, seed int64, ground map[[3]int]uint32) map[[3]int]uint32 {
	c := TreeFeatures[name]
	blocks := map[[3]int]uint32{}
	for p, st := range ground {
		blocks[p] = st
	}
	rng := rand.New(rand.NewSource(seed))
	set := func(x, y, z int, state uint32, leaf bool) {
		q := [3]int{x, y, z}
		// A real driver's leaf rule: leaves take air and leaves take leaves
		// (the distance rewrite), never a log or the ground.
		if cur, ok := blocks[q]; leaf && ok && !IsLeaves(cur) {
			return
		}
		blocks[q] = state
	}
	read := func(x, y, z int) uint32 { return blocks[[3]int{x, y, z}] }
	free := func(x, y, z int) bool { return y >= 0 }
	PlaceTree(c, 0, 0, 0, rng, TreeDriver{
		Set: set, Free: free, Read: read,
		DirtGround: func(x, y, z int) bool { return IsDirtTag(read(x, y, z)) },
		SurfaceTop: func(x, z int) int { return 0 },
	})
	return blocks
}

// Every leaf family answers IsLeaves — it used to stop at birch, which cut
// seven species out of the seeding rewrite and the item fall-through rule.
func TestIsLeavesCoversEverySpecies(t *testing.T) {
	for _, n := range []string{"oak_leaves", "spruce_leaves", "birch_leaves",
		"jungle_leaves", "acacia_leaves", "cherry_leaves", "dark_oak_leaves",
		"pale_oak_leaves", "mangrove_leaves", "azalea_leaves", "flowering_azalea_leaves"} {
		lo, hi := BlockRange(n)
		if !IsLeaves(lo) || !IsLeaves(hi) {
			t.Errorf("%s (%d..%d) is not IsLeaves", n, lo, hi)
		}
	}
	if IsLeaves(blockBase("oak_leaves")-1) || IsLeaves(blockBase("flowering_azalea_leaves")+28) {
		t.Error("IsLeaves bleeds past the leaf families")
	}
}

// Creaking hearts come from the six-log rule, not a coordinate hash: a shuffled
// log with logs on ALL SIX faces becomes the heart. A plain 2x2 trunk has no
// such cell — the pocket only forms where the dark oak placer's diagonal bend
// folds two footprints together — so hearts must appear in SOME trees, never
// all, and always fully enclosed.
func TestCreakingHeartsAppearInLogPockets(t *testing.T) {
	hearts, trees := 0, 0
	for seed := int64(1); seed <= 400; seed++ {
		blocks := grownWorld("pale_oak_creaking", seed, nil)
		if len(blocks) == 0 {
			continue
		}
		trees++
		for p, st := range blocks {
			if st != CreakingHeartDormant {
				continue
			}
			hearts++
			for _, o := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
				if !IsLog(blocks[[3]int{p[0] + o[0], p[1] + o[1], p[2] + o[2]}]) {
					t.Fatalf("seed %d: heart at %v has a non-log neighbour at %v", seed, p, o)
				}
			}
		}
	}
	if hearts == 0 {
		t.Errorf("no creaking hearts in %d pale oaks — the log pocket never formed", trees)
	}
	if hearts >= trees {
		t.Errorf("%d hearts in %d trees — every tree got one, the six-log rule is not filtering", hearts, trees)
	}
	// The heartless variant never grows one, whatever the seed.
	for seed := int64(1); seed <= 100; seed++ {
		for _, st := range grownWorld("pale_oak", seed, nil) {
			if st == CreakingHeartDormant {
				t.Fatal("a plain pale_oak grew a creaking heart")
			}
		}
	}
}

// Mangrove propagules hang under the canopy: always the hanging stage-0 state,
// always below a leaf, always with the required empty space, and spread out by
// the exclusion zone — never two side by side at the same height.
func TestMangrovesHangPropagules(t *testing.T) {
	plo, phi := BlockRange("mangrove_propagule")
	total := 0
	for seed := int64(1); seed <= 60; seed++ {
		blocks := grownWorld("tall_mangrove", seed, nil)
		var pods [][3]int
		for p, st := range blocks {
			if st < plo || st > phi {
				continue
			}
			total++
			pods = append(pods, p)
			if (st-plo)%8 != 1 {
				t.Fatalf("seed %d: propagule state %d is not hanging/stage0/dry", seed, st)
			}
			if age := (st - plo) / 8; age > 4 {
				t.Fatalf("seed %d: propagule age %d out of range", seed, age)
			}
			if !IsLeaves(blocks[[3]int{p[0], p[1] + 1, p[2]}]) {
				t.Fatalf("seed %d: propagule at %v does not hang from a leaf", seed, p)
			}
			if _, occupied := blocks[[3]int{p[0], p[1] - 1, p[2]}]; occupied {
				t.Fatalf("seed %d: propagule at %v lacks its required empty block below", seed, p)
			}
		}
		for i, a := range pods {
			for _, b := range pods[i+1:] {
				dx, dz := a[0]-b[0], a[2]-b[2]
				if a[1] == b[1] && dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1 {
					t.Fatalf("seed %d: propagules at %v and %v violate the exclusion radius", seed, a, b)
				}
			}
		}
	}
	if total == 0 {
		t.Error("sixty tall mangroves hung no propagules")
	}
}

// Podzol circles under a mega spruce: only where the ground is in #dirt, only
// at ground level, and never on a stone floor. The probe runs +2 down to -3
// per column and converts the FIRST dirt-tag block it meets.
func TestMegaSprucePodzolsDirtGround(t *testing.T) {
	grass := blockBase("grass_block") + 1 // snowy=false
	ground := map[[3]int]uint32{}
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			ground[[3]int{x, -1, z}] = grass
			ground[[3]int{x, -2, z}] = Dirt
		}
	}
	podzols, spruces := 0, 0
	for seed := int64(1); seed <= 20; seed++ {
		blocks := grownWorld("mega_spruce", seed, ground)
		grew := false
		for p, st := range blocks {
			if IsLog(st) {
				grew = true
			}
			if st != podzolState {
				continue
			}
			podzols++
			if p[1] != -1 {
				t.Fatalf("seed %d: podzol at y=%d — it replaced something that was not the grass surface", seed, p[1])
			}
			if p[0] < -6 || p[0] > 7 || p[2] < -6 || p[2] > 7 {
				t.Fatalf("seed %d: podzol at %v is beyond the decorator's reach", seed, p)
			}
		}
		if grew {
			spruces++
		}
	}
	if spruces == 0 {
		t.Fatal("no mega spruce grew at all")
	}
	if podzols == 0 {
		t.Error("mega spruces podzoled nothing on a grass floor")
	}
	// A stone floor is not in #dirt: the only podzol is on the (at most
	// four) blocks the trunk's own setDirtAt converted — the circles find
	// nothing else to replace.
	stone := map[[3]int]uint32{}
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			stone[[3]int{x, -1, z}] = Stone
		}
	}
	trunkDirt := map[[3]int]bool{{0, -1, 0}: true, {1, -1, 0}: true, {0, -1, 1}: true, {1, -1, 1}: true}
	for seed := int64(1); seed <= 10; seed++ {
		for p, st := range grownWorld("mega_spruce", seed, stone) {
			if st == podzolState && !trunkDirt[p] {
				t.Fatalf("seed %d: podzol at %v on a stone floor, beyond the trunk's own dirt", seed, p)
			}
		}
	}
}

// Pale oaks wear their moss: a pale-moss patch in the ground at the foot
// (80% of trees), hanging strands off the trunk and canopy, and carpet or
// grass scattered on the patch. The bonemeal twin — what a sapling grows —
// stays bare.
func TestPaleOaksGrowPaleMoss(t *testing.T) {
	grass := blockBase("grass_block") + 1
	ground := map[[3]int]uint32{}
	for x := -14; x <= 14; x++ {
		for z := -14; z <= 14; z++ {
			ground[[3]int{x, -1, z}] = grass
			ground[[3]int{x, -2, z}] = Dirt
			ground[[3]int{x, -3, z}] = Stone
		}
	}
	mossBlock := blockBase("pale_moss_block")
	hangTip, _ := BlockRange("pale_hanging_moss")
	carpetLo, carpetHi := BlockRange("pale_moss_carpet")
	shortGrass := blockBase("short_grass")
	tallLo, tallHi := BlockRange("tall_grass")
	patches, strands, vegetation := 0, 0, 0
	for seed := int64(1); seed <= 60; seed++ {
		blocks := grownWorld("pale_oak", seed, ground)
		sawPatch := false
		for p, st := range blocks {
			switch {
			case st == mossBlock:
				sawPatch = true
				if p[1] != -1 {
					t.Fatalf("seed %d: moss ground at y=%d, not in the surface", seed, p[1])
				}
			case st == hangTip || st == hangTip+1:
				strands++
				above := blocks[[3]int{p[0], p[1] + 1, p[2]}]
				if !IsLog(above) && !IsLeaves(above) && above != hangTip+1 {
					t.Fatalf("seed %d: hanging moss at %v under %d — not a log, leaf or stem", seed, p, above)
				}
				if st == hangTip+1 {
					below := blocks[[3]int{p[0], p[1] - 1, p[2]}]
					if below != hangTip && below != hangTip+1 {
						t.Fatalf("seed %d: a stem at %v has no moss below — a strand without a tip", seed, p)
					}
				}
			case (st >= carpetLo && st <= carpetHi) || st == shortGrass || (st >= tallLo && st <= tallHi):
				vegetation++
				if st == tallLo+1 { // half=lower must carry its upper half
					if blocks[[3]int{p[0], p[1] + 1, p[2]}] != tallLo {
						t.Fatalf("seed %d: tall grass lower at %v without its upper half", seed, p)
					}
				}
			}
		}
		if sawPatch {
			patches++
		}
	}
	if patches < 30 {
		t.Errorf("only %d of 60 pale oaks grew a moss patch — the 0.8 ground roll is off", patches)
	}
	if strands == 0 {
		t.Error("no hanging moss on sixty pale oaks")
	}
	if vegetation == 0 {
		t.Error("no carpet or grass on sixty moss patches")
	}
	// The sapling-grown twin is bare: no moss, no patch, no heart.
	for seed := int64(1); seed <= 30; seed++ {
		for _, st := range grownWorld("pale_oak_bonemeal", seed, ground) {
			if st == mossBlock || st == hangTip || st == hangTip+1 || st == CreakingHeartDormant {
				t.Fatalf("pale_oak_bonemeal grew decoration state %d (seed %d)", st, seed)
			}
		}
	}
}

// The moss patch respects the ground: nothing is placed into a stone floor
// (stone IS moss-replaceable, so it mosses over), but a floor of obsidian —
// outside #moss_replaceable — stays untouched.
func TestPaleMossPatchRespectsReplaceable(t *testing.T) {
	obsidian := blockBase("obsidian")
	ground := map[[3]int]uint32{}
	for x := -14; x <= 14; x++ {
		for z := -14; z <= 14; z++ {
			ground[[3]int{x, -1, z}] = obsidian
		}
	}
	mossBlock := blockBase("pale_moss_block")
	for seed := int64(1); seed <= 20; seed++ {
		for _, st := range grownWorld("pale_oak", seed, ground) {
			if st == mossBlock {
				t.Fatal("pale moss laid into an obsidian floor")
			}
		}
	}
}

// Every trunk placer runs setDirtAt beneath it — but the rule only converts
// ground that is NOT already in #dirt, and grass IS in #dirt: a tree on grass
// keeps its grass, a tree on stone gets dirt put under it (all four blocks
// under a 2x2 trunk), and a mangrove — whose ground belongs to its root
// placer — converts nothing anywhere.
func TestTrunksSetDirtBeneath(t *testing.T) {
	grass := blockBase("grass_block") + 1
	plane := func(b uint32) map[[3]int]uint32 {
		ground := map[[3]int]uint32{}
		for x := -12; x <= 12; x++ {
			for z := -12; z <= 12; z++ {
				ground[[3]int{x, -1, z}] = b
			}
		}
		return ground
	}
	if got := grownWorld("oak", 1, plane(grass))[[3]int{0, -1, 0}]; got != grass {
		t.Errorf("under an oak on grass: state %d — grass is #dirt and must stay", got)
	}
	if got := grownWorld("oak", 1, plane(Stone))[[3]int{0, -1, 0}]; got != Dirt {
		t.Errorf("under an oak on stone: state %d, want dirt", got)
	}
	// The mega spruce dirts all four blocks — and then its own alter_ground
	// decorator podzols them, dirt being in #dirt. The end state is podzol,
	// which is vanilla's pipeline exactly (setDirtAt first, circles second).
	spruce := grownWorld("mega_spruce", 1, plane(Stone))
	for _, q := range [4][3]int{{0, -1, 0}, {1, -1, 0}, {0, -1, 1}, {1, -1, 1}} {
		if spruce[q] != podzolState {
			t.Errorf("under a mega spruce on stone at %v: state %d, want podzol", q, spruce[q])
		}
	}
	if got := grownWorld("mangrove", 1, plane(Stone))[[3]int{0, -1, 0}]; got != Stone {
		t.Errorf("under a mangrove: state %d — its ground belongs to the root placer", got)
	}
}

// The cocoa window is anchored at the FIRST trunk position — the dirt row
// when the placer actually converted the ground (stone), the base log when
// the ground was already #dirt (grass). Vanilla measures from
// logs.getFirst(), and only a WRITTEN dirt block is in that list.
func TestCocoaWindowAnchorsAtTheDirtRow(t *testing.T) {
	clo, chi := BlockRange("cocoa")
	plant := func(groundBlock uint32) (minY, maxY int, any bool) {
		ground := map[[3]int]uint32{}
		for x := -10; x <= 10; x++ {
			for z := -10; z <= 10; z++ {
				ground[[3]int{x, -1, z}] = groundBlock
			}
		}
		minY, maxY = 99, -99
		for seed := int64(1); seed <= 80; seed++ {
			for p, st := range grownWorld("jungle_tree", seed, ground) {
				if st >= clo && st <= chi {
					any = true
					if p[1] < minY {
						minY = p[1]
					}
					if p[1] > maxY {
						maxY = p[1]
					}
				}
			}
		}
		return
	}
	if lo, hi, any := plant(Stone); !any {
		t.Error("no cocoa on eighty jungle trees over stone")
	} else if lo < -1 || hi > 1 {
		t.Errorf("cocoa on stone-grown jungles spans y %d..%d, want within [-1,1] (dirt-row anchor)", lo, hi)
	}
	grass := blockBase("grass_block") + 1
	if lo, hi, any := plant(grass); !any {
		t.Error("no cocoa on eighty jungle trees over grass")
	} else if lo < 0 || hi > 2 {
		t.Errorf("cocoa on grass-grown jungles spans y %d..%d, want within [0,2] (base-log anchor)", lo, hi)
	}
}

// The forest pool rolls vanilla's cascade — a fifth birch, a tenth large
// oaks, oak for the rest, all litter variants — and never a foreign tree.
func TestForestRollsBirchAndLargeOaks(t *testing.T) {
	if biomeReg["minecraft:forest"].Tree != treeForest {
		t.Fatal("forest biome does not use the forest pool")
	}
	birch, fancy, oak, empty := 0, 0, 0, 0
	total := 4000
	for wx := 0; wx < total; wx++ {
		switch c := treeFeatureFor(treeForest, 1, wx*23, 71); {
		case c == nil:
			empty++
		case c == TreeFeatures["birch_leaf_litter"]:
			birch++
		case c == TreeFeatures["fancy_oak_leaf_litter"]:
			fancy++
		case c == TreeFeatures["oak_leaf_litter"]:
			oak++
		default:
			t.Fatalf("forest rolled a foreign tree at wx=%d", wx)
		}
	}
	if birch < total/8 || birch > total/3 {
		t.Errorf("birch rolled %d of %d, want ~20%%", birch, total)
	}
	if fancy < total/25 || fancy > total/6 {
		t.Errorf("large oaks rolled %d of %d, want ~8%%", fancy, total)
	}
	if oak < total/2 {
		t.Errorf("oak rolled %d of %d, want ~70%%", oak, total)
	}
	if empty > total/20 {
		t.Errorf("%d empty fallen-tree slots of %d, want ~1.5%%", empty, total)
	}
}

// Leaf litter settles around the litter trees: always on solid ground, always
// in the passes' reach, never floating, and never inside the trunk column —
// the heightmap rule keeps it out from directly under the tree's own logs.
func TestLitterTreesScatterLeafLitter(t *testing.T) {
	llo, lhi := BlockRange("leaf_litter")
	grass := blockBase("grass_block") + 1
	ground := map[[3]int]uint32{}
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			ground[[3]int{x, -1, z}] = grass
		}
	}
	total := 0
	for seed := int64(1); seed <= 30; seed++ {
		blocks := grownWorld("oak_leaf_litter", seed, ground)
		for p, st := range blocks {
			if st < llo || st > lhi {
				continue
			}
			total++
			if p[1] != 0 {
				t.Fatalf("seed %d: litter at y=%d — the floor is at y=0", seed, p[1])
			}
			if !IsSolidFull(blocks[[3]int{p[0], -1, p[2]}]) {
				t.Fatalf("seed %d: litter floating at %v", seed, p)
			}
			if p[0] < -4 || p[0] > 4 || p[2] < -4 || p[2] > 4 {
				t.Fatalf("seed %d: litter at %v beyond the pass radius", seed, p)
			}
			if p[0] == 0 && p[2] == 0 {
				t.Fatalf("seed %d: litter inside the trunk column", seed)
			}
		}
	}
	if total == 0 {
		t.Error("thirty litter oaks scattered no leaf litter")
	}
	// The heightmap rule: no litter in a column under one of the tree's own
	// branch LOGS (a canopy of leaves is fine — MOTION_BLOCKING_NO_LEAVES).
	// The large oak's limbs are what make this observable.
	for seed := int64(1); seed <= 30; seed++ {
		blocks := grownWorld("fancy_oak_leaf_litter", seed, ground)
		for p, st := range blocks {
			if st < llo || st > lhi {
				continue
			}
			for yy := p[1] + 1; yy <= p[1]+24; yy++ {
				if IsLog(blocks[[3]int{p[0], yy, p[2]}]) {
					t.Fatalf("seed %d: litter at %v under a branch log at y=%d", seed, p, yy)
				}
			}
		}
	}
	// The bare oak scatters nothing.
	for seed := int64(1); seed <= 20; seed++ {
		for _, st := range grownWorld("oak", seed, ground) {
			if st >= llo && st <= lhi {
				t.Fatal("a bare oak scattered leaf litter")
			}
		}
	}
}
