package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
)

// The generated row names line up with tachyne-common's stonecutter table.
func TestStonecutRowNamesAlign(t *testing.T) {
	if len(stonecutRowNames) != len(protocol.StonecuttingRecipes) {
		t.Fatalf("%d names for %d rows", len(stonecutRowNames), len(protocol.StonecuttingRecipes))
	}
	for i, r := range protocol.StonecuttingRecipes {
		name := stonecutRowNames[i]
		out := itemNameOf[r.Out]
		if len(name) < len(out) || name[:len(out)] != out {
			t.Errorf("row %d (%s) is named %s", i, out, name)
		}
	}
}

// A pack's stonecutting recipes join the stonecutter in the registry's
// order and a file at a vanilla id takes that row away: every Java player
// is sent the merged rows (update_recipes), the menu's buttons index the
// same list the client filters.
func TestDataPackStonecuttingRecipes(t *testing.T) {
	s, h, ps, logs := functionServer(t, map[string]string{
		"data/test/recipe/stone_to_diamond.json":                        `{"type":"minecraft:stonecutting","ingredient":"minecraft:stone","result":{"id":"minecraft:diamond","count":2}}`,
		"data/minecraft/recipe/stone_slab_from_stone_stonecutting.json": `{"type":"minecraft:stonecutting"}`,
	})
	settle(t, h, logs, "S0")
	stone, diamond, slab := itemByName["stone"], itemByName["diamond"], itemByName["stone_slab"]
	u := packUpdateRecipes(currentPack())
	if u == nil || u.Stonecutter == nil || u.ItemSets != nil {
		t.Fatalf("update recipes %+v", u)
	}
	var fromStone []attachproto.StonecutterRecipe // the client's filtered list
	for _, r := range u.Stonecutter {
		if containsItem(r.Input.Items, stone) {
			fromStone = append(fromStone, r)
		}
	}
	list := stonecutList(stone)
	if len(list) != len(fromStone) {
		t.Fatalf("the menu lists %d choices for stone, the client %d", len(list), len(fromStone))
	}
	sawDiamond := false
	for i, r := range list {
		if r.Out != fromStone[i].Result || int32(r.Count) != fromStone[i].Count {
			t.Errorf("choice %d: menu %v, client %v", i, r, fromStone[i])
		}
		if r.Out == slab {
			t.Error("the removed stone slab row is still listed")
		}
		sawDiamond = sawDiamond || r.Out == diamond
	}
	if !sawDiamond {
		t.Error("the pack's stone → diamond row is not listed")
	}
	// The menu cuts by that list.
	onHub(t, h, func() {
		tr := h.playersRef[ps["alice"].eid]
		h.openStonecutter(tr)
		tr.anvil[0] = invStack{item: stone, count: 5}
		for i, r := range list {
			if r.Out == diamond {
				h.stonecutSelect(tr, int32(i))
			}
		}
		if res := h.stonecutResult(tr); res.item != diamond || res.count != 2 {
			t.Errorf("the selected choice cuts %+v", res)
		}
	})
	if s.hub == nil {
		t.Fatal("no hub")
	}
}

// A load that changes the recipe data sends it to every Java session
// (update_recipes); a load that goes back to vanilla's sends the empty
// form (the gateways' own tables) — and Bedrock sessions get neither.
func TestPackRecipesReachTheClients(t *testing.T) {
	t.Cleanup(func() { installPackContent(nil) })
	_, h, ps, logs := signedCmdServer(t)
	ps["carol"].bedrock = true
	d := newPackDataFiles()
	d.recipes["test:cut"] = packFile{pack: "file/fp", data: []byte(`{"type":"minecraft:stonecutting","ingredient":"minecraft:dirt","result":"minecraft:diamond"}`)}
	pc := buildPackContent(d, map[string]map[string]bool{})
	onHub(t, h, func() { h.applyPackContent(h.playersRef, pc) })
	upd := waitAnyEv(t, logs["alice"], "alice's update_recipes", func(ev any) bool {
		_, ok := ev.(attachproto.UpdateRecipes)
		return ok
	}).(attachproto.UpdateRecipes)
	if upd.Stonecutter == nil || upd.ItemSets != nil {
		t.Errorf("update_recipes %+v", upd)
	}
	onHub(t, h, func() { h.applyPackContent(h.playersRef, nil) })
	markAll(t, h, logs, "U1")
	n := 0
	for _, ev := range logs["alice"].snapshot() {
		if u, ok := ev.(attachproto.UpdateRecipes); ok {
			n++
			if n == 2 && (u.Stonecutter != nil || u.ItemSets != nil) {
				t.Errorf("back to vanilla: %+v", u)
			}
		}
	}
	if n != 2 {
		t.Errorf("alice was sent %d update_recipes, want 2", n)
	}
	for _, ev := range logs["carol"].snapshot() {
		if _, ok := ev.(attachproto.UpdateRecipes); ok {
			t.Error("a Bedrock session was sent update_recipes")
		}
	}
	if !isLifecycleFrame(attachproto.UpdateRecipes{}) {
		t.Error("update_recipes may be dropped")
	}
}

// A pack's smithing recipes are tried first (a transform with no template
// wants the slot empty) and a file at a vanilla id takes that upgrade away;
// the smithing item sets the clients are sent take the pack's items.
func TestDataPackSmithingRecipes(t *testing.T) {
	_, h, ps, logs := functionServer(t, map[string]string{
		"data/test/recipe/gild.json":                          `{"type":"minecraft:smithing_transform","base":"minecraft:iron_sword","addition":"minecraft:gold_ingot","result":{"id":"minecraft:golden_sword"}}`,
		"data/minecraft/recipe/netherite_sword_smithing.json": `{}`,
	})
	settle(t, h, logs, "M0")
	u := packUpdateRecipes(currentPack())
	if u == nil || u.ItemSets == nil {
		t.Fatalf("update recipes %+v", u)
	}
	for _, set := range u.ItemSets {
		if set.Key == "minecraft:smithing_addition" && !containsItem(set.Items, itemByName["gold_ingot"]) {
			t.Error("the addition set lacks the pack's gold ingot")
		}
	}
	onHub(t, h, func() {
		tr := h.playersRef[ps["alice"].eid]
		tr.extraSlot = invStack{}
		tr.anvil[0] = invStack{item: itemByName["iron_sword"], count: 1, dmg: 7}
		tr.anvil[1] = invStack{item: itemByName["gold_ingot"], count: 1}
		if res := h.smithResult(tr); res.item != itemByName["golden_sword"] || res.dmg != 7 {
			t.Errorf("the pack's transform made %+v", res)
		}
		tr.extraSlot = invStack{item: protocol.SmithingUpgradeTemplate, count: 1}
		tr.anvil[0] = invStack{item: itemByName["diamond_sword"], count: 1}
		tr.anvil[1] = invStack{item: itemByName["netherite_ingot"], count: 1}
		if res := h.smithResult(tr); res.item != 0 {
			t.Errorf("the removed netherite sword upgrade made %+v", res)
		}
		tr.anvil[0] = invStack{item: itemByName["diamond_pickaxe"], count: 1}
		if res := h.smithResult(tr); res.item != itemByName["netherite_pickaxe"] {
			t.Errorf("the pickaxe upgrade is gone: %+v", res)
		}
	})
}

// A pack's brewing recipe brews in the stand (its reagent goes in the
// ingredient slot), and a file at a vanilla id takes that brew away.
func TestDataPackBrewingRecipes(t *testing.T) {
	_, h, _, logs := functionServer(t, map[string]string{
		"data/test/recipe/lucky.json": `{"type":"minecraft:brewing","input":{"item":"minecraft:potion","potion_contents":{"potions":"minecraft:awkward"}},` +
			`"reagent":{"item":"minecraft:apple"},"output":{"id":"minecraft:potion","components":{"minecraft:potion_contents":{"potion":"minecraft:luck"}}}}`,
		"data/minecraft/recipe/brewing/potion_awkward_sugar.json": `{}`,
	})
	settle(t, h, logs, "B0")
	onHub(t, h, func() {
		if out, ok := brewOne(potionStack(potAwkward), itemByName["apple"]); !ok || out.potion != potLuck || out.item != itemPotion {
			t.Errorf("awkward + apple brewed %+v %v", out, ok)
		}
		if !brewIsIngredient(itemByName["apple"]) {
			t.Error("the apple is no brewing ingredient")
		}
		if _, ok := brewOne(potionStack(potAwkward), itemByName["sugar"]); ok {
			t.Error("the removed awkward + sugar recipe still brews")
		}
		if out, ok := brewOne(potionStackIn(itemSplashPotion, potAwkward), itemByName["sugar"]); !ok || out.potion != potSwiftness {
			t.Errorf("the splash awkward + sugar recipe is gone: %+v %v", out, ok)
		}
	})
}
