package worldgen

import "testing"

// The pale garden is the only biome whose whole identity is its colour, and it
// was decorating itself with DARK OAK — the biome existed, generated, and grew
// the wrong tree, which no test noticed because a tree is a tree. These tests
// pin every biome's tree kind to the FEATURE it actually resolves to, on the
// same path stampTree takes.

func TestPaleGardenGrowsPaleOak(t *testing.T) {
	b, ok := biomeReg["minecraft:pale_garden"]
	if !ok {
		t.Fatal("pale_garden has no biome style")
	}
	if b.Tree != treePaleOak {
		t.Errorf("pale garden tree kind = %v, want treePaleOak", b.Tree)
	}
	pale := TreeFeatures["pale_oak"]
	for _, wx := range []int{0, 17, 4096, -333} {
		c := treeFeatureFor(b.Tree, 1, wx, 7)
		if c == nil {
			t.Fatalf("pale garden resolves no tree feature at wx=%d", wx)
		}
		if c.Log != pale.Log || c.Leaves != pale.Leaves {
			t.Errorf("pale garden tree at wx=%d has wood %d/%d, want pale oak %d/%d",
				wx, c.Log, c.Leaves, pale.Log, pale.Leaves)
		}
	}
}

// One pale oak in ten runs the creaking-heart decorator — the
// pale_garden_vegetation split. The rest grow bare.
func TestPaleGardenSeedsCreakingTrees(t *testing.T) {
	hearts, total := 0, 4000
	for wx := 0; wx < total; wx++ {
		c := treeFeatureFor(treePaleOak, 1, wx*31, 55)
		if c.HeartProb > 0 {
			hearts++
		}
	}
	if hearts < total/20 || hearts > total/5 {
		t.Errorf("%d of %d pale oaks carry the heart decorator, want ~1 in 10", hearts, total)
	}
}

// The dark forest rolls vanilla's vegetation cascade: dark oak leads at about
// three in five, birch and the odd large oak mix in, and the huge-mushroom
// slots stay EMPTY until mushrooms are ported — never another biome's tree.
func TestDarkForestRollsItsCascade(t *testing.T) {
	b := biomeReg["minecraft:dark_forest"]
	if b.Tree != treeDarkForest {
		t.Errorf("dark forest tree kind = %v, want treeDarkForest", b.Tree)
	}
	darkOak, birch, other, empty := 0, 0, 0, 0
	total := 4000
	for wx := 0; wx < total; wx++ {
		switch c := treeFeatureFor(treeDarkForest, 1, wx*17, 44); {
		case c == nil:
			empty++
		case c == TreeFeatures["dark_oak_leaf_litter"]:
			darkOak++
		case c == TreeFeatures["birch_leaf_litter"]:
			birch++
		case c == TreeFeatures["fancy_oak_leaf_litter"] || c == TreeFeatures["oak_leaf_litter"]:
			other++
		default:
			t.Fatalf("dark forest rolled a foreign tree at wx=%d", wx)
		}
	}
	if darkOak < total/2 {
		t.Errorf("dark oak led only %d of %d rolls, want ~62%%", darkOak, total)
	}
	if birch == 0 || other == 0 {
		t.Errorf("birch (%d) and oak-family (%d) should both appear", birch, other)
	}
	if empty == 0 || empty > total/5 {
		t.Errorf("%d empty mushroom/fallen slots of %d, want ~8%%", empty, total)
	}
}

// Every tree kind resolves to a real feature with its own wood — a kind added
// to the enum and forgotten in treeFeatureFor's switch resolves to nil and
// grows nothing, which is exactly the failure this pins.
func TestEveryTreeKindResolvesItsOwnWood(t *testing.T) {
	kinds := []treeKind{treeOak, treeBirch, treeSpruce, treeJungle, treeAcacia,
		treeDarkOak, treeCherry, treeMangrove, treePaleOak, treeSwampOak,
		treeSpruceOld, treePineOld}
	woodOf := map[treeKind]uint32{}
	for _, k := range kinds {
		c := treeFeatureFor(k, 1, 5, 5)
		if c == nil {
			t.Errorf("tree kind %v resolves to no feature", k)
			continue
		}
		woodOf[k] = c.Log
	}
	// The distinct-wood rule, minus the deliberate sharers: swamp oaks are
	// oaks with vines, and the taiga variants are all spruce wood.
	spruceWood := map[treeKind]bool{treeSpruce: true, treeSpruceOld: true, treePineOld: true}
	seen := map[uint32]treeKind{}
	for k, log := range woodOf {
		if k == treeSwampOak || spruceWood[k] {
			continue
		}
		if prev, dup := seen[log]; dup {
			t.Errorf("tree kinds %v and %v both grow trunk %d", prev, k, log)
		}
		seen[log] = k
	}
	if woodOf[treeSwampOak] != woodOf[treeOak] {
		t.Errorf("swamp oaks should grow oak wood, got %d", woodOf[treeSwampOak])
	}
	for k := range spruceWood {
		if woodOf[k] != woodOf[treeSpruce] {
			t.Errorf("%v should grow spruce wood, got %d", k, woodOf[k])
		}
	}
}

// Old-growth taigas actually roll their mega trees: vanilla's cascade gives
// the spruce taiga a third mega spruces, and the pine taiga mostly mega pines.
func TestOldGrowthTaigasGrowMegaTrees(t *testing.T) {
	megaSpruce, megaPine := TreeFeatures["mega_spruce"], TreeFeatures["mega_pine"]
	if megaSpruce == nil || megaPine == nil {
		t.Fatal("mega features missing from the generated table")
	}
	counts := func(k treeKind) (spruces, pines int) {
		for wx := 0; wx < 3000; wx++ {
			switch c := treeFeatureFor(k, 1, wx*13, 99); c {
			case megaSpruce:
				spruces++
			case megaPine:
				pines++
			}
		}
		return
	}
	if s, p := counts(treeSpruceOld); s < 700 || p != 0 {
		t.Errorf("old-growth spruce taiga rolled %d mega spruces, %d mega pines; want ~1000, 0", s, p)
	}
	if s, p := counts(treePineOld); s < 30 || s > 200 || p < 700 {
		t.Errorf("old-growth pine taiga rolled %d mega spruces, %d mega pines; want ~77, ~900", s, p)
	}
	// …and the megas are the podzol trees.
	if !megaSpruce.AlterGround || !megaPine.AlterGround {
		t.Error("mega spruce/pine should carry the alter_ground decorator")
	}
}
