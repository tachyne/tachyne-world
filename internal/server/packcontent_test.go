package server

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// writePackFile adds (or replaces) a file in the enabled test pack "fp".
func writePackFile(t *testing.T, s *Server, rel, body string) {
	t.Helper()
	p := filepath.Join(s.DataPackDir, "fp", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// grid2 is a 2×2 crafting grid of the named items ("" = empty).
func grid2(names ...string) []invStack {
	g := make([]invStack, 4)
	for i, n := range names {
		if n != "" {
			g[i] = invStack{item: itemByName[n], count: 1}
		}
	}
	return g
}

// A pack's recipes through /reload and /datapack: a shaped and a
// shapeless recipe (an item tag of the pack's own among its ingredients)
// crafted in the grid, a smelting recipe cooked in a furnace, a vanilla
// crafting and a vanilla smelting recipe removed by files that cannot be
// made, a stonecutting recipe listed as not applied, /recipe give for the
// pack's ids, and a player's book carried across a reload that renumbers
// the pack's entries — kept by name while the pack is disabled, and back
// when it returns.
func TestDataPackRecipesApplyOnReload(t *testing.T) {
	s, h, ps, logs := functionServer(t, map[string]string{
		"data/test/tags/item/gravelish.json":                           `{"values":["minecraft:gravel"]}`,
		"data/test/recipe/dirt_diamonds.json":                          `{"type":"minecraft:crafting_shaped","pattern":[" # "," # "],"key":{"#":"minecraft:dirt"},"result":{"id":"minecraft:diamond","count":3}}`,
		"data/test/recipe/glass_mix.json":                              `{"type":"minecraft:crafting_shapeless","ingredients":["minecraft:sand","#minecraft:planks","#test:gravelish"],"result":"minecraft:glass"}`,
		"data/test/recipe/dirt_smelt.json":                             `{"type":"minecraft:smelting","ingredient":"minecraft:dirt","result":{"id":"minecraft:emerald"},"cookingtime":10,"experience":5,"category":"blocks"}`,
		"data/test/recipe/cut.json":                                    `{"type":"minecraft:stonecutting","ingredient":"minecraft:dirt","result":{"id":"minecraft:diamond","count":2}}`,
		"data/minecraft/recipe/crafting_table.json":                    `{"type":"minecraft:crafting_shaped","pattern":["#"],"key":{"#":"minecraft:air"},"result":"minecraft:crafting_table"}`,
		"data/minecraft/recipe/iron_ingot_from_smelting_raw_iron.json": `not json`,
	})
	alice := ps["alice"]
	settle(t, h, logs, "R") // the load is installed on the hub
	var firstID int32
	onHub(t, h, func() {
		if it, n := matchRecipe(grid2("dirt", "", "dirt", ""), 2); it != itemByName["diamond"] || n != 3 {
			t.Errorf("dirt over dirt makes %d × %d, want 3 diamonds", n, it)
		}
		if it, _ := matchRecipe(grid2("", "dirt", "", "dirt"), 2); it != itemByName["diamond"] {
			t.Errorf("the shaped recipe should match anywhere in the grid, got %d", it)
		}
		if it, _ := matchRecipe(grid2("gravel", "sand", "spruce_planks", ""), 2); it != itemByName["glass"] {
			t.Errorf("sand, planks and gravel make %d, want glass", it)
		}
		if it, _ := matchRecipe(grid2("oak_planks", "oak_planks", "oak_planks", "oak_planks"), 2); it != 0 {
			t.Errorf("the crafting table recipe was removed, yet four planks make %d", it)
		}
		if e, ok := cookerRecipe(cookFurnace, itemByName["dirt"]); !ok || e.Out != itemByName["emerald"] || e.Cook != 10 {
			t.Errorf("dirt in a furnace: %+v %v", e, ok)
		}
		if _, ok := cookerRecipe(cookFurnace, itemByName["raw_iron"]); ok {
			t.Error("raw iron still smelts after its recipe was removed")
		}
		if _, ok := cookerRecipe(cookBlast, itemByName["raw_iron"]); !ok {
			t.Error("the blasting recipe for raw iron is another id and should stay")
		}
		if xp := smeltXP(itemByName["emerald"]); xp != 5 {
			t.Errorf("an emerald from the pack's recipe banks %v xp, want 5", xp)
		}

		// The furnace cooks the pack's recipe.
		players := h.playersRef
		h.world.SetBlock(4, 150, 0, worldgen.BlockBase("furnace"))
		f := &furnace{kind: cookFurnace}
		f.slots[furnaceInput] = invStack{item: itemByName["dirt"], count: 1}
		f.burnLeft, f.burnMax = 1000, 1000
		h.furnaces[simPos{blockPos: blockPos{4, 150, 0}}] = f
		for i := 0; i < 12; i++ {
			h.updateFurnaces(players)
		}
		if out := f.slots[furnaceOutput]; out.item != itemByName["emerald"] || out.count != 1 {
			t.Errorf("furnace output %+v, want one emerald", out)
		}
		firstID, _ = recipeIDIn(currentPack(), "test:dirt_diamonds")
	})

	settle(t, h, logs, "R0")
	s.handleCommand(alice, "recipe give alice test:dirt_diamonds")
	s.handleCommand(alice, "recipe give alice test:dirt_smelt")
	s.handleCommand(alice, "recipe give alice crafting_table")
	s.handleCommand(alice, "recipe give alice test:cut")
	s.handleCommand(alice, "datapack list enabled")
	settle(t, h, logs, "R1")
	a := linesBetween(logs["alice"], "R0", "R1")
	for _, want := range []string{
		"Unlocked 1 recipe(s) for alice",
		"Unknown recipe: minecraft:crafting_table",
		"Unknown recipe: test:cut",
		"[file/fp (world)] also carries recipe (minecraft:stonecutting): this server does not apply that data",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	if n := strings.Count(strings.Join(a, "\n"), "Unlocked 1 recipe(s) for alice"); n != 2 {
		t.Errorf("%d recipes given, want 2: %q", n, a)
	}
	onHub(t, h, func() {
		tr := h.playersRef[alice.eid]
		if firstID < packBookFirstID || !tr.rbKnown[firstID] {
			t.Errorf("alice's book lacks the pack's recipe %d (pack ids from %d): %v", firstID, packBookFirstID, tr.rbKnown)
		}
	})

	// A recipe that sorts first renumbers the pack's entries; the book follows.
	writePackFile(t, s, "data/test/recipe/a_first.json", `{"type":"minecraft:crafting_shapeless","ingredients":["minecraft:dirt"],"result":"minecraft:stick"}`)
	s.handleCommand(alice, "reload")
	settle(t, h, logs, "R2")
	onHub(t, h, func() {
		tr := h.playersRef[alice.eid]
		id, ok := recipeIDIn(currentPack(), "test:dirt_diamonds")
		if !ok || id == firstID {
			t.Errorf("after the reload test:dirt_diamonds is %d (%v), was %d", id, ok, firstID)
		}
		if !tr.rbKnown[id] || tr.rbKnown[firstID] {
			t.Errorf("alice's book was not carried to the new ids: %v", tr.rbKnown)
		}
		if it, _ := matchRecipe(grid2("dirt", "", "", ""), 2); it != itemByName["stick"] {
			t.Errorf("the recipe added by the reload makes %d", it)
		}
	})

	// Disabled, the pack's recipes leave the game and vanilla's come back;
	// the book keeps the pack's recipe by name.
	s.handleCommand(alice, `datapack disable "file/fp"`)
	settle(t, h, logs, "R3")
	onHub(t, h, func() {
		tr := h.playersRef[alice.eid]
		if it, _ := matchRecipe(grid2("oak_planks", "oak_planks", "oak_planks", "oak_planks"), 2); it != itemByName["crafting_table"] {
			t.Errorf("with the pack disabled four planks make %d", it)
		}
		if _, ok := cookerRecipe(cookFurnace, itemByName["raw_iron"]); !ok {
			t.Error("with the pack disabled raw iron should smelt again")
		}
		if !tr.rbDormant["test:dirt_diamonds"] {
			t.Errorf("the disabled pack's recipe was not kept by name: %v", tr.rbDormant)
		}
	})
	s.handleCommand(alice, `datapack enable "file/fp"`)
	settle(t, h, logs, "R4")
	onHub(t, h, func() {
		tr := h.playersRef[alice.eid]
		id, _ := recipeIDIn(currentPack(), "test:dirt_diamonds")
		if !tr.rbKnown[id] || tr.rbDormant["test:dirt_diamonds"] {
			t.Errorf("re-enabled, the recipe did not come back to the book: known %v dormant %v", tr.rbKnown, tr.rbDormant)
		}
	})
}

// A pack's loot tables through /reload and /loot: a block's table in the
// 26.x shape (a vanilla predicate named by id, a modifier) overriding the
// stone's, a mob's death table, a pack table nesting another by id and one
// inline with an expanded item tag, and a vanilla chest table removed by a
// file that does not parse.
func TestDataPackLootTablesApplyOnReload(t *testing.T) {
	s, h, ps, logs := functionServer(t, nil)
	alice := ps["alice"]
	onHub(t, h, func() { h.world.SetBlock(3, 100, 3, worldgen.BlockID("stone")) })
	for rel, body := range map[string]string{
		"data/minecraft/loot_table/blocks/stone.json": `{"type":"minecraft:block","pools":[{"rolls":1,"entries":[{"type":"minecraft:alternatives","children":[
			{"type":"minecraft:item","condition":"minecraft:tool/can_shear","name":"minecraft:stone"},
			{"type":"minecraft:item","modifier":{"type":"minecraft:set_count","count":2},"name":"minecraft:diamond"}]}]}]}`,
		"data/minecraft/loot_table/entities/cow.json":          `{"type":"minecraft:entity","pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:diamond"}]}]}`,
		"data/minecraft/loot_table/chests/simple_dungeon.json": `not json`,
		"data/test/tags/item/shiny.json":                       `{"values":["minecraft:emerald"]}`,
		"data/test/loot_table/inner.json":                      `{"pools":[{"rolls":{"type":"minecraft:uniform","min":5,"max":5},"entries":[{"type":"minecraft:tag","items":"#test:shiny","expand":true}]}]}`,
		"data/test/loot_table/box.json": `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:loot_table","value":"test:inner"}]},
			{"rolls":1,"entries":[{"type":"minecraft:loot_table","value":{"pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:apple"}]}]}}]}]}`,
	} {
		writePackFile(t, s, rel, body)
	}
	s.handleCommand(alice, "reload")
	settle(t, h, logs, "L0")
	s.handleCommand(alice, "loot give alice mine 3 100 3")
	s.handleCommand(alice, "loot give bob mine 3 100 3 shears")
	s.handleCommand(alice, "loot give alice loot test:box")
	s.handleCommand(alice, "loot give alice loot chests/simple_dungeon")
	s.handleCommand(alice, "loot give alice loot test:nope")
	s.handleCommand(alice, "summon cow 2 100 2")
	settle(t, h, logs, "L1")
	s.handleCommand(alice, "loot give carol kill @e[type=cow,limit=1]")
	settle(t, h, logs, "L2")
	a := linesBetween(logs["alice"], "L0", "L2")
	for _, want := range []string{
		"Dropped 2 [Diamond] from loot table minecraft:blocks/stone",
		"Dropped 1 [Stone] from loot table minecraft:blocks/stone",
		"Dropped 6 items",
		"Dropped 0 items",
		"The loot table test:nope is not available on this server",
		"Dropped 1 [Diamond] from loot table minecraft:entities/cow",
	} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	onHub(t, h, func() {
		count := func(name, item string) int {
			n := 0
			for _, tr := range h.playersRef {
				if tr.p.name != name {
					continue
				}
				for _, st := range tr.inv.slots {
					if st.item == itemByName[item] {
						n += st.count
					}
				}
			}
			return n
		}
		if n := count("alice", "emerald"); n != 5 {
			t.Errorf("alice holds %d emeralds from test:inner, want 5", n)
		}
		if n := count("alice", "apple"); n != 1 {
			t.Errorf("alice holds %d apples from the inline table, want 1", n)
		}
		if n := count("carol", "diamond"); n != 1 {
			t.Errorf("carol holds %d diamonds from the cow, want 1", n)
		}
	})
}

// A pack's tags through /reload and /datapack: a vanilla block tag added
// to (and seen by worldgen's lookups and a tag nesting it), an item tag
// replaced, a new biome tag `execute if biome` tests, an item tag that does
// not load for a missing element, and every change gone once the pack is
// disabled.
func TestDataPackTagsApplyOnReload(t *testing.T) {
	s, h, ps, logs := functionServer(t, nil)
	alice := ps["alice"]
	var biome string
	onHub(t, h, func() { biome = nsID(h.world.BiomeAt3D(0, 64, 0)) })
	for rel, body := range map[string]string{
		"data/minecraft/tags/block/logs.json":     `{"values":["minecraft:stone"]}`,
		"data/test/tags/block/new.json":           `{"values":["#minecraft:logs","minecraft:dirt",{"id":"minecraft:no_such_block","required":false}]}`,
		"data/minecraft/tags/item/boats.json":     `{"replace":true,"values":["minecraft:oak_boat"]}`,
		"data/test/tags/item/broken.json":         `{"values":["minecraft:no_such_item"]}`,
		"data/test/tags/worldgen/biome/here.json": fmt.Sprintf(`{"values":[%q]}`, biome),
	} {
		writePackFile(t, s, rel, body)
	}
	s.handleCommand(alice, "reload")
	settle(t, h, logs, "T0")
	onHub(t, h, func() {
		lg := worldgen.BlockTagNames("logs")
		if !slices.Contains(lg, "stone") || !slices.Contains(lg, "oak_log") {
			t.Errorf("#logs = %v, want vanilla's logs and stone", lg)
		}
		if n := worldgen.BlockTagNames("test:new"); !slices.Contains(n, "stone") || !slices.Contains(n, "dirt") || !slices.Contains(n, "birch_log") {
			t.Errorf("#test:new = %v", n)
		}
		if b := worldgen.ItemTag("boats"); !slices.Equal(b, []string{"oak_boat"}) {
			t.Errorf("#boats = %v, want only the oak boat", b)
		}
		if _, ok := tagMembers("item", "test:broken"); ok {
			t.Error("a tag naming a missing item loaded")
		}
		if !slices.Contains(worldgen.BlockTagList(), "test:new") {
			t.Error("the pack's block tag is not listed")
		}
	})
	s.handleCommand(alice, "execute if biome 0 64 0 #test:here run gamerule keep_inventory true")
	s.handleCommand(alice, "execute if biome 0 64 0 #test:nope run gamerule fall_damage false")
	settle(t, h, logs, "T1")
	if !hasLine(linesBetween(logs["alice"], "T0", "T1"), "Can't find tag 'test:nope' of type 'minecraft:worldgen/biome'") {
		t.Errorf("an unknown biome tag: %q", linesBetween(logs["alice"], "T0", "T1"))
	}
	if r := ruleState(t, h); !r.KeepInventory || !r.FallDamage {
		t.Errorf("keep_inventory %v fall_damage %v after the biome tests", r.KeepInventory, r.FallDamage)
	}

	s.handleCommand(alice, `datapack disable "file/fp"`)
	settle(t, h, logs, "T2")
	onHub(t, h, func() {
		if slices.Contains(worldgen.BlockTagNames("logs"), "stone") {
			t.Error("the disabled pack's #logs entry stayed")
		}
		if slices.Contains(worldgen.BlockTagList(), "test:new") {
			t.Error("the disabled pack's tag stayed")
		}
		if b := worldgen.ItemTag("boats"); len(b) < 2 {
			t.Errorf("#boats = %v, want vanilla's again", b)
		}
	})
}

// A pack's predicates and item modifiers: `execute if predicate` with a
// pack predicate, one naming another by id, a weather check, an unknown id
// and an inline predicate; /item modify with a pack's modifier, and one
// that is a list naming another.
func TestDataPackPredicatesAndItemModifiers(t *testing.T) {
	s, h, ps, logs := functionServer(t, map[string]string{
		"data/test/predicate/always.json":    `{"type":"minecraft:random_chance","chance":1.0}`,
		"data/test/predicate/never.json":     `{"type":"minecraft:inverted","term":"test:always"}`,
		"data/test/predicate/rain.json":      `{"type":"minecraft:weather_check","raining":true}`,
		"data/test/item_modifier/five.json":  `{"type":"minecraft:set_count","count":5}`,
		"data/test/item_modifier/named.json": `["test:five",{"type":"minecraft:set_name","name":"Rock"}]`,
	})
	alice := ps["alice"]
	settle(t, h, logs, "P0")
	s.handleCommand(alice, "execute if predicate test:always run gamerule keep_inventory true")
	s.handleCommand(alice, "execute if predicate test:never run gamerule fall_damage false")
	s.handleCommand(alice, "execute if predicate test:rain run gamerule mob_griefing false")
	s.handleCommand(alice, "execute if predicate test:missing run gamerule mob_griefing false")
	s.handleCommand(alice, `execute unless predicate {type:"minecraft:random_chance",chance:0.0} run gamerule drowning_damage false`)
	s.handleCommand(alice, "item replace entity alice hotbar.0 with stone 1")
	s.handleCommand(alice, "item replace entity alice hotbar.1 with stone 1")
	settle(t, h, logs, "P1")
	s.handleCommand(alice, "item modify entity alice hotbar.0 test:five")
	s.handleCommand(alice, "item modify entity alice hotbar.1 test:named")
	settle(t, h, logs, "P2")
	a := linesBetween(logs["alice"], "P0", "P2")
	if !hasLine(a, "Can't find element 'test:missing' in registry 'minecraft:predicate'") {
		t.Errorf("an unknown predicate: %q", a)
	}
	if n := strings.Count(strings.Join(a, "\n"), "Modified 1 slot(s) on alice"); n != 2 {
		t.Errorf("modifier lines: %q", a)
	}
	r := ruleState(t, h)
	if !r.KeepInventory || !r.FallDamage || !r.MobGriefing || r.DrownDamage {
		t.Errorf("keep_inventory %v fall_damage %v mob_griefing %v drowning_damage %v", r.KeepInventory, r.FallDamage, r.MobGriefing, r.DrownDamage)
	}
	onHub(t, h, func() {
		tr := h.playersRef[alice.eid]
		if st := tr.inv.slots[0]; st.count != 5 {
			t.Errorf("hotbar.0 after test:five: %+v", st)
		}
		if st := tr.inv.slots[1]; st.count != 5 || st.name != "Rock" {
			t.Errorf("hotbar.1 after test:named: %+v", st)
		}
	})
}
