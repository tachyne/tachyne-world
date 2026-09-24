package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// grid builds a crafting grid from item names ("" = empty).
func grid(names ...string) []invStack {
	g := make([]invStack, len(names))
	for i, n := range names {
		if n != "" {
			g[i] = invStack{item: itemByName[n], count: 1}
		}
	}
	return g
}

// Vanilla's recipe slots take ingredients, not items: a crafting table's four
// cells each accept any planks, so they may be mixed. The table used to hold
// one uniform recipe per material and match each cell against one exact id,
// so every one of these crafted nothing.
func TestMixedMaterialsCraft(t *testing.T) {
	for _, tc := range []struct {
		name string
		w    int
		g    []invStack
		want string
		n    int
	}{
		{"crafting table, oak and spruce", 2, grid("oak_planks", "spruce_planks", "birch_planks", "oak_planks"), "crafting_table", 1},
		{"sticks, oak over spruce", 2, grid("oak_planks", "", "spruce_planks", ""), "stick", 4},
		{"furnace, cobblestone and cobbled deepslate", 3, grid(
			"cobblestone", "cobbled_deepslate", "cobblestone",
			"cobbled_deepslate", "", "blackstone",
			"cobblestone", "cobblestone", "cobbled_deepslate"), "furnace", 1},
		{"chest, mixed planks", 3, grid(
			"oak_planks", "spruce_planks", "oak_planks",
			"jungle_planks", "", "oak_planks",
			"oak_planks", "cherry_planks", "oak_planks"), "chest", 1},
		// and still a plain uniform one
		{"crafting table, all oak", 2, grid("oak_planks", "oak_planks", "oak_planks", "oak_planks"), "crafting_table", 1},
	} {
		got, n := matchRecipe(tc.g, tc.w)
		if got != itemByName[tc.want] || n != tc.n {
			t.Errorf("%s: got item %d x%d, want %s (%d) x%d", tc.name, got, n, tc.want, itemByName[tc.want], tc.n)
		}
	}
	// A slot's ingredient still refuses what it does not accept.
	if got, _ := matchRecipe(grid("oak_planks", "stone", "oak_planks", "oak_planks"), 2); got != 0 {
		t.Errorf("stone is not planks, yet the grid made item %d", got)
	}
}

// Vanilla writes some patterns padded with blank columns — a spyglass is
// " # ", " X ", " X " — and trims them when it loads the recipe. Keeping the
// padding would have left these uncraftable, because a grid is matched on its
// own trimmed bounding box.
func TestPaddedPatternsAreTrimmed(t *testing.T) {
	for _, tc := range []struct {
		g    []invStack
		want string
	}{
		{grid("", "amethyst_shard", "", "", "copper_ingot", "", "", "copper_ingot", ""), "spyglass"},
		{grid("heavy_core", "", "", "breeze_rod", "", "", "", "", ""), "mace"},
		{grid("", "", "pale_oak_log", "", "", "resin_block", "", "", "pale_oak_log"), "creaking_heart"},
	} {
		if got, _ := matchRecipe(tc.g, 3); got != itemByName[tc.want] {
			t.Errorf("%s: got item %d, want %d", tc.want, got, itemByName[tc.want])
		}
	}
}

// Clicking a recipe in the book fills the grid from the inventory, choosing
// for each cell an item its ingredient accepts — so two oak and two spruce
// planks fill a crafting table, as vanilla's recipe placement does.
func TestPlaceRecipeFillsFromMixedMaterials(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	pl.inv.slots[0] = invStack{item: itemByName["oak_planks"], count: 2}
	pl.inv.slots[1] = invStack{item: itemByName["spruce_planks"], count: 2}

	h.placeRecipe(players, pl, evCraftRequest{eid: 1, windowID: 0, recipeID: recipeIDByName["crafting_table"]})
	oak, spruce := 0, 0
	for _, c := range pl.craft[:4] {
		switch c.item {
		case itemByName["oak_planks"]:
			oak += c.count
		case itemByName["spruce_planks"]:
			spruce += c.count
		}
	}
	if oak != 2 || spruce != 2 {
		t.Fatalf("grid holds %d oak and %d spruce, want 2 of each", oak, spruce)
	}
	if pl.inv.slots[0].count != 0 || pl.inv.slots[1].count != 0 {
		t.Fatalf("inventory kept %d oak and %d spruce", pl.inv.slots[0].count, pl.inv.slots[1].count)
	}
	if got, _ := matchRecipe(pl.craft[:4], 2); got != itemByName["crafting_table"] {
		t.Fatalf("the filled grid crafts item %d, want a crafting table", got)
	}
}

// setOf finds the index of the ingredient set that is exactly these items.
func setOf(t *testing.T, names ...string) uint16 {
	t.Helper()
	want := map[int32]bool{}
	for _, n := range names {
		want[itemByName[n]] = true
	}
	for i, s := range ingredientSets {
		if len(s) != len(want) {
			continue
		}
		ok := true
		for _, it := range s {
			ok = ok && want[it]
		}
		if ok {
			return uint16(i)
		}
	}
	t.Fatalf("no ingredient set is exactly %v", names)
	return 0
}

// The assignment has to be able to take back a first choice. With one oak and
// one spruce plank, a cell that takes any planks grabs the oak first — and then
// a cell that takes only oak has nothing, unless the first moves to spruce.
func TestIngredientAssignmentReroutes(t *testing.T) {
	oak, spruce := itemByName["oak_planks"], itemByName["spruce_planks"]
	var planks uint16
	for i, s := range ingredientSets {
		if len(s) > 2 && ingredientAccepts(uint16(i), oak) && ingredientAccepts(uint16(i), spruce) &&
			ingredientAccepts(uint16(i), itemByName["cherry_planks"]) {
			planks = uint16(i)
			break
		}
	}
	oakOnly := setOf(t, "oak_planks")
	pick := assignIngredients([]uint16{planks, oakOnly}, []int32{oak, spruce}, map[int32]int{oak: 1, spruce: 1})
	if pick == nil || pick[0] != spruce || pick[1] != oak {
		t.Fatalf("pick = %v, want [spruce oak]", pick)
	}
	if assignIngredients([]uint16{oakOnly, oakOnly}, []int32{oak, spruce}, map[int32]int{oak: 1, spruce: 1}) != nil {
		t.Fatal("two oak-only cells cannot both be filled from one oak plank")
	}
	// The shapeless test is the same matching.
	if !pairIngredients([]int32{oak, spruce}, []uint16{planks, oakOnly}) {
		t.Fatal("oak and spruce pair off against any-planks and oak-only")
	}
	if pairIngredients([]int32{spruce, spruce}, []uint16{planks, oakOnly}) {
		t.Fatal("two spruce cannot fill an oak-only ingredient")
	}
}

// A book saved while it stored table indices loads into the same recipes by
// name. The old table had twelve crafting tables, one per wood; all twelve
// become the one vanilla recipe. Cooking entries keep their meaning too.
func TestLegacyRecipeBookMigrates(t *testing.T) {
	var old []int32
	for i, n := range legacyRecipeNames {
		if n == "crafting_table" {
			old = append(old, int32(i))
		}
	}
	if len(old) != 12 {
		t.Fatalf("expected the old table's twelve crafting tables, found %d", len(old))
	}
	cook := cookBookRecipes[0]
	oldCook := legacyCraftRecipeCount + int32(0)
	st := newRecipeBookStore(filepath.Join(t.TempDir(), "recipebook.json"))
	st.m[ids.key("legion")] = rbState{Known: append(append([]int32{}, old...), oldCook), Highlight: []int32{old[3]}}

	pl := testTracked()
	st.loadInto(pl, "legion")
	table := recipeIDByName["crafting_table"]
	if len(pl.rbKnown) != 2 || !pl.rbKnown[table] || !pl.rbKnown[cook.ID] {
		t.Fatalf("known = %v, want the crafting table (%d) and %s (%d)", pl.rbKnown, table, cookRecipeKey(cook), cook.ID)
	}
	if len(pl.rbHighlight) != 1 || !pl.rbHighlight[table] {
		t.Fatalf("highlight = %v, want the crafting table", pl.rbHighlight)
	}
	st.record("legion", pl)
	saved := st.m[ids.key("legion")]
	if len(saved.Known) != 0 || len(saved.Highlight) != 0 {
		t.Fatal("the book was written back in the old index form")
	}
	if len(saved.Names) != 2 || saved.HighlightNames[0] != "crafting_table" {
		t.Fatalf("saved names = %v, highlight %v", saved.Names, saved.HighlightNames)
	}
}

// Every name the frozen legacy map can hand out must exist in the current
// table; a regenerated table that dropped one would silently empty that entry
// from an old book.
func TestEveryLegacyNameStillResolves(t *testing.T) {
	for i, n := range legacyRecipeNames {
		if _, ok := recipeIDByName[n]; !ok {
			t.Errorf("old id %d means %q, which is not a recipe any more", i, n)
		}
	}
}
