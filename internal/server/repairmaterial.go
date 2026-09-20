package server

import "strings"

// What repairs what on an anvil — the items' `repairable` component. A tool
// or a piece of armour takes its own tier's material, and each one put in
// mends a quarter of the item's durability. Without this an anvil could
// only combine two of the same item, so a diamond pickaxe could never be
// mended with diamonds.

// repairsFor is the repair material of each tier, by the item-name prefix
// the registry gives it. The lists are the vanilla tags: the wooden tier
// takes any plank, stone takes cobblestone, blackstone or cobbled
// deepslate, and the rest take their ingot or gem.
var repairsFor = []struct {
	prefix    string
	materials []string
}{
	{"wooden_", planksList},
	{"stone_", []string{"cobblestone", "blackstone", "cobbled_deepslate"}},
	{"copper_", []string{"copper_ingot"}},
	{"iron_", []string{"iron_ingot"}},
	{"chainmail_", []string{"iron_ingot"}},
	{"golden_", []string{"gold_ingot"}},
	{"diamond_", []string{"diamond"}},
	{"netherite_", []string{"netherite_ingot"}},
	{"leather_", []string{"leather"}},
}

// Named items whose repair material is not a tier's.
var repairsExact = map[string][]string{
	"turtle_helmet": {"turtle_scute"},
	"wolf_armor":    {"armadillo_scute"},
	"elytra":        {"phantom_membrane"},
	"mace":          {"breeze_rod"},
	"shield":        planksList,
}

var planksList = []string{"oak_planks", "spruce_planks", "birch_planks", "jungle_planks",
	"acacia_planks", "dark_oak_planks", "mangrove_planks", "cherry_planks", "pale_oak_planks",
	"bamboo_planks", "crimson_planks", "warped_planks"}

// repairMaterials maps an item to the items that mend it.
var repairMaterials = func() map[int32]map[int32]bool {
	out := map[int32]map[int32]bool{}
	add := func(item int32, names []string) {
		if item == 0 {
			return
		}
		set := map[int32]bool{}
		for _, n := range names {
			if id := itemByName[n]; id != 0 {
				set[id] = true
			}
		}
		if len(set) > 0 {
			out[item] = set
		}
	}
	for name, id := range itemByName {
		if mats, ok := repairsExact[name]; ok {
			add(id, mats)
			continue
		}
		if _, damageable := itemMaxDurability[id]; !damageable {
			continue
		}
		for _, r := range repairsFor {
			if strings.HasPrefix(name, r.prefix) {
				add(id, r.materials)
				break
			}
		}
	}
	return out
}()

// repairsWith reports whether the sacrifice is a valid repair material for
// the item — ItemStack.isValidRepairItem.
func repairsWith(item, material int32) bool {
	return repairMaterials[item][material]
}

// anvilMaterialRepair is AnvilMenu.createResult's first branch: each
// material mends a quarter of the item's maximum durability, as many as
// are needed and available, and each one used costs a level. It reports
// the mended stack, how many materials went in, and the level cost.
func anvilMaterialRepair(a, b invStack) (invStack, int, int, bool) {
	max, ok := itemMaxDurability[a.item]
	if !ok || !repairsWith(a.item, b.item) {
		return invStack{}, 0, 0, false
	}
	step := func(dmg int) int { return minInt(dmg, max/4) }
	if step(a.dmg) <= 0 {
		return invStack{}, 0, 0, false // nothing to mend
	}
	res, used, cost := a, 0, 0
	for used < b.count && step(res.dmg) > 0 {
		res.dmg -= step(res.dmg)
		used++
		cost++
	}
	return res, used, cost, true
}

// minInt is the smaller of two ints.
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
