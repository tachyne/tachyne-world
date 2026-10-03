package server

import "testing"

// Vanilla's recipes follow the item tags a pack changes: with #planks cut
// to oak, a crafting table takes oak planks and no longer spruce; with dirt
// added to #logs_that_burn it smelts into charcoal; with #smelts_to_glass
// emptied, sand no longer smelts. A vanilla recipe naming an untouched tag
// keeps its members.
func TestVanillaRecipesFollowChangedTags(t *testing.T) {
	_, h, _, logs := functionServer(t, map[string]string{
		"data/minecraft/tags/item/planks.json":          `{"replace":true,"values":["minecraft:oak_planks"]}`,
		"data/minecraft/tags/item/logs_that_burn.json":  `{"values":["minecraft:dirt"]}`,
		"data/minecraft/tags/item/smelts_to_glass.json": `{"replace":true,"values":[]}`,
	})
	settle(t, h, logs, "T0")
	onHub(t, h, func() {
		four := func(n string) []invStack { return grid2(n, n, n, n) }
		if it, _ := matchRecipe(four("oak_planks"), 2); it != itemByName["crafting_table"] {
			t.Errorf("four oak planks make %d, want a crafting table", it)
		}
		if it, _ := matchRecipe(four("spruce_planks"), 2); it == itemByName["crafting_table"] {
			t.Error("spruce planks, no longer #planks, still make a crafting table")
		}
		if e, ok := cookerRecipe(cookFurnace, itemByName["dirt"]); !ok || e.Out != itemByName["charcoal"] {
			t.Errorf("dirt smelts to %+v %v, want charcoal", e, ok)
		}
		if e, ok := cookerRecipe(cookFurnace, itemByName["oak_log"]); !ok || e.Out != itemByName["charcoal"] {
			t.Errorf("an oak log no longer smelts to charcoal: %+v %v", e, ok)
		}
		if _, ok := cookerRecipe(cookFurnace, itemByName["sand"]); ok {
			t.Error("sand still smelts with #smelts_to_glass emptied")
		}
		if it, _ := matchRecipe(grid2("charcoal", "", "stick", ""), 2); it != itemByName["torch"] {
			t.Errorf("charcoal over a stick (#coals, untouched) makes %d, want a torch", it)
		}
	})
}
