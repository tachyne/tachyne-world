package server

import (
	"encoding/json"
	"math/rand"
	"strings"
	"testing"
)

// Every enchantment is obtainable somewhere vanilla makes it obtainable: the
// table for the non-treasure set, loot for on_random_loot, a trade for the
// tradeable set — and the fifteen the old hand-rolled tables never offered
// (the crossbow and trident sets, knockback, sweeping edge, the mace trio,
// soul speed, both curses) all show up.
func TestEveryEnchantmentIsObtainable(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	seen := map[int8]bool{}
	items := []int32{itemBook, itemByName["diamond_sword"], itemByName["diamond_pickaxe"], itemByName["diamond_boots"],
		itemByName["diamond_helmet"], itemByName["diamond_chestplate"], itemByName["diamond_leggings"], itemBow,
		itemCrossbow, itemTrident, itemFishingRod, itemByName["mace"], itemByName["golden_sword"]}
	for i := 0; i < 4000; i++ {
		item := items[i%len(items)]
		for _, e := range enchSelect(r, item, 1+r.Intn(30), enchTableAllowed) {
			seen[e.id] = true
		}
		for _, e := range enchWithLevels(r, item, 30) {
			if e.lvl > 0 {
				seen[e.id] = true
			}
		}
		if e := enchRandomly(r, item); e[0].lvl > 0 {
			seen[e[0].id] = true
		}
	}
	// Soul speed, swift sneak and wind burst are loot-only in vanilla too —
	// pinned enchant_randomly / set_enchantments functions in the bastion,
	// ancient city and ominous vault tables, which the chest data carries.
	pinned, _ := json.Marshal(chestLoot)
	for _, name := range []string{"soul_speed", "swift_sneak", "wind_burst"} {
		if strings.Contains(string(pinned), `"ench":"`+name+`"`) {
			seen[enchByName[name]] = true
		}
	}
	for _, name := range []string{"multishot", "quick_charge", "piercing", "loyalty", "impaling", "riptide", "channeling",
		"knockback", "sweeping_edge", "density", "breach", "binding_curse", "vanishing_curse", "sharpness", "mending"} {
		var id int8 = -1
		for i := range enchDefs {
			if enchDefs[i].name == name {
				id = int8(i)
			}
		}
		if id < 0 || !seen[id] {
			t.Errorf("%s never came out of any roller", name)
		}
	}
	for i := range enchDefs {
		if !seen[int8(i)] {
			t.Errorf("%s is unobtainable", enchDefs[i].name)
		}
	}
}

// The treasure set never comes out of the table, and the table never offers
// an enchantment the item is not primary for (smite on an axe).
func TestTableOffersOnlyPrimaryNonTreasure(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	axe := itemByName["diamond_axe"]
	for i := 0; i < 3000; i++ {
		for _, e := range enchSelect(r, axe, 1+r.Intn(30), enchTableAllowed) {
			d := enchDefs[e.id]
			if d.flags&enchTreasure != 0 {
				t.Fatalf("table rolled treasure enchantment %s", d.name)
			}
			if d.name == "smite" || d.name == "bane_of_arthropods" || d.name == "fire_aspect" {
				t.Fatalf("an axe rolled %s, which is only primary on swords", d.name)
			}
		}
	}
	if !enchIsSupported(enchSmite, axe) || enchIsPrimary(enchSmite, axe) {
		t.Error("smite must be supported-but-not-primary on an axe (a book applies, the table does not)")
	}
}

// Level windows: Sharpness V needs a modified cost of at least 45; a cost of
// 1 can only yield level I of anything.
func TestAvailableLevelsFollowCostWindows(t *testing.T) {
	sword := itemByName["diamond_sword"]
	for _, e := range enchAvailable(1, sword, enchTableAllowed) {
		if e.lvl != 1 {
			t.Errorf("cost 1 offered %s %d", enchDefs[e.id].name, e.lvl)
		}
	}
	got := 0
	for _, e := range enchAvailable(45, sword, enchTableAllowed) {
		if e.id == enchSharpness {
			got = int(e.lvl)
		}
	}
	if got != 5 {
		t.Errorf("cost 45 should offer Sharpness V, got %d", got)
	}
	if enchDefs[enchSharpness].minCost(5) != 45 || enchDefs[enchProtection].maxCost(4) != 45 {
		t.Error("linear cost curves: sharpness min(5)=1+11*4=45, protection max(4)=12+11*3=45")
	}
}

// Exclusive sets and item support at the anvil.
func TestAnvilHonoursSupportAndExclusiveSets(t *testing.T) {
	sword := invStack{item: itemByName["diamond_sword"], count: 1, ench: enchList{{id: enchSharpness, lvl: 3}}}
	smiteBook := invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchSmite, lvl: 2}}}
	if res, _ := anvilResult(sword, smiteBook, ""); res.item != 0 {
		t.Errorf("a Smite book must not go onto a Sharpness sword, got %+v", res)
	}
	boots := invStack{item: itemByName["diamond_boots"], count: 1}
	sharpBook := invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchSharpness, lvl: 1}}}
	if res, _ := anvilResult(boots, sharpBook, ""); res.item != 0 {
		t.Errorf("a Sharpness book must not go onto boots, got %+v", res)
	}
	unbBook := invStack{item: itemEnchantedBook, count: 1, ench: enchList{{id: enchUnbreaking, lvl: 3}}}
	res, cost := anvilResult(sword, unbBook, "")
	if res.enchLvl(enchUnbreaking) != 3 || res.enchLvl(enchSharpness) != 3 {
		t.Errorf("Unbreaking III should join Sharpness III, got %+v", res)
	}
	if cost != 3 { // anvil cost 2, halved for a book → 1 per level × 3
		t.Errorf("book cost %d, want 3 (unbreaking anvil cost 2 halved, times level 3)", cost)
	}
	if !enchCompatible(enchSharpness, enchUnbreaking) || enchCompatible(enchFortune, enchSilkTouch) ||
		enchCompatible(enchInfinity, enchMending) || enchCompatible(enchDepthStrider, enchFrostWalker) {
		t.Error("exclusive sets: fortune/silk touch, infinity/mending, depth strider/frost walker")
	}
}

// Enchantable values drive the cost bonus: gold rolls higher than diamond.
func TestEnchantabilityValues(t *testing.T) {
	for name, want := range map[string]int{"golden_sword": 22, "diamond_pickaxe": 10, "iron_boots": 9, "leather_helmet": 15,
		"netherite_chestplate": 15, "turtle_helmet": 9, "book": 1, "mace": 15, "stone_axe": 5, "chainmail_leggings": 12} {
		if got := enchantabilityOf(itemByName[name]); got != want {
			t.Errorf("%s enchantability %d, want %d", name, got, want)
		}
	}
	if enchantabilityOf(itemByName["rotten_flesh"]) != 0 {
		t.Error("rotten flesh has no Enchantable value")
	}
}

// Four enchantments ride a stack through the two persisted forms.
func TestFourEnchantmentsPersist(t *testing.T) {
	st := invStack{item: itemByName["diamond_sword"], count: 1, ench: enchList{
		{id: enchSharpness, lvl: 5}, {id: enchUnbreaking, lvl: 3}, {id: enchLooting, lvl: 3}, {id: enchFireAspect, lvl: 2}}}
	if back := unpackStack(packStack(st)); back != st {
		t.Errorf("stack row round trip %+v, want %+v", back.ench, st.ench)
	}
	if got := unpackEnch2(packEnch(st.ench), packEnchHi(st.ench)); got != st.ench {
		t.Errorf("column round trip %+v", got)
	}
	// An older row with only the first column keeps its two enchantments.
	two := unpackEnch2(packEnch(st.ench), 0)
	if two[0] != st.ench[0] || two[1] != st.ench[1] || two[2].lvl != 0 {
		t.Errorf("legacy row decode %+v", two)
	}
	list := []enchInstance{{enchSharpness, 5}, {enchUnbreaking, 3}, {enchLooting, 3}, {enchFireAspect, 2}, {enchSmite, 1}}
	if got := enchApplyList(list); got[3].id != enchFireAspect || got[3].lvl != 2 {
		t.Errorf("enchApplyList must keep four: %+v", got)
	}
}
