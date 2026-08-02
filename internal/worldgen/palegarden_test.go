package worldgen

import "testing"

// The pale garden is the only biome whose whole identity is its colour, and it
// was decorating itself with DARK OAK — the biome existed, generated, and grew
// the wrong tree, which no test noticed because a tree is a tree.

func TestPaleGardenGrowsPaleOak(t *testing.T) {
	b, ok := biomeReg["minecraft:pale_garden"]
	if !ok {
		t.Fatal("pale_garden has no biome style")
	}
	if b.Tree != treePaleOak {
		t.Errorf("pale garden tree kind = %v, want treePaleOak", b.Tree)
	}
	log, leaves, _, _, _ := treeStyle(b.Tree)
	if log != PaleOakLog {
		t.Errorf("pale garden trunk = %d, want pale oak %d", log, PaleOakLog)
	}
	if leaves != PaleOakLeaves {
		t.Errorf("pale garden canopy = %d, want pale oak %d", leaves, PaleOakLeaves)
	}
}

// The dark forest keeps its own tree — the fix must not have swapped the two.
func TestDarkForestStillGrowsDarkOak(t *testing.T) {
	b := biomeReg["minecraft:dark_forest"]
	if b.Tree != treeDarkOak {
		t.Errorf("dark forest tree kind = %v, want treeDarkOak", b.Tree)
	}
	if log, _, _, _, _ := treeStyle(b.Tree); log != DarkOakLog {
		t.Errorf("dark forest trunk = %d, want dark oak %d", log, DarkOakLog)
	}
}

// Every tree kind resolves to its own wood. The default arm of treeStyle
// returns oak, so a kind added to the enum and forgotten in the switch grows
// an oak and looks plausible — which is exactly how pale oak got missed.
func TestEveryTreeKindHasItsOwnWood(t *testing.T) {
	seen := map[uint32]treeKind{}
	for k := treeOak; k <= treePaleOak; k++ {
		log, _, _, _, _ := treeStyle(k)
		if prev, dup := seen[log]; dup {
			t.Errorf("tree kinds %v and %v both grow trunk %d", prev, k, log)
		}
		seen[log] = k
	}
}

// A generated pale oak and a planted one must be the same tree. The sapling
// grower has always built dark/pale oak on a 2x2 trunk (TreeShape.TwoByTwo)
// while the generator wrote a single column, so the two disagreed — and the
// creaking heart's placement rule, which needs a log with logs on every side,
// could never be satisfied by a generated tree.
func TestMegaSpeciesGenerateOnAWideTrunk(t *testing.T) {
	for _, k := range []treeKind{treeDarkOak, treePaleOak} {
		if !wideTrunk(k) {
			t.Errorf("tree kind %v should generate on a 2x2 trunk", k)
		}
	}
	for _, k := range []treeKind{treeOak, treeBirch, treeSpruce, treeJungle, treeAcacia, treeCherry, treeMangrove} {
		if wideTrunk(k) {
			t.Errorf("tree kind %v should generate on a single trunk", k)
		}
	}
	// …and the two tables agree on which species those are.
	for name, shape := range treeShapeBySapling {
		wide := shape.Log == DarkOakLog || shape.Log == PaleOakLog
		if shape.TwoByTwo != wide {
			t.Errorf("%s: sapling TwoByTwo=%v disagrees with its wood", name, shape.TwoByTwo)
		}
	}
}
