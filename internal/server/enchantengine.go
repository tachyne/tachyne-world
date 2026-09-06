package server

import (
	"math"
	"strings"
)

// The vanilla enchantment engine — EnchantmentHelper.getEnchantmentCost /
// selectEnchantment / getAvailableEnchantmentResults over the data in
// enchantments_gen.go (weights, level caps, cost curves, item sets, exclusive
// sets, pool tags), plus the Enchantable component values items carry. Every
// roller — the table, loot's enchant_randomly and enchant_with_levels, the
// fishing treasure book, the anvil's compatibility rule — draws from here, so
// an enchantment is obtainable exactly where vanilla makes it obtainable and
// nowhere else.
//
// One engine-wide limit remains: a stack holds at most two enchantments
// ([2]enchApply), so a vanilla roll of three or four keeps its first two.

type enchDef struct {
	name                                               string
	weight, maxLevel, minBase, minPer, maxBase, maxPer int
	anvilCost                                          int
	flags                                              uint8
	supported, primary                                 []string // item registry names
	exclusive                                          []int8   // enchantment ids it cannot share a stack with
}

const (
	enchInTable           uint8 = 1 << iota // #in_enchanting_table (= non-treasure)
	enchTreasure                            // #treasure: loot/trades only, never the table
	enchCurse                               // #curse
	enchTradeable                           // #tradeable: a librarian may sell it
	enchOnRandomLoot                        // #on_random_loot: loot rollers may pick it
	enchOnTradedEquipment                   // #on_traded_equipment
	enchOnMobEquipment                      // #on_mob_spawn_equipment
	enchDoubleTradePrice                    // #double_trade_price
)

// LevelBasedValue.Linear: base + per_level_above_first × (level − 1).
func (d *enchDef) minCost(lvl int) int { return d.minBase + d.minPer*(lvl-1) }
func (d *enchDef) maxCost(lvl int) int { return d.maxBase + d.maxPer*(lvl-1) }

// enchMaxLvl is the vanilla cap per enchantment.
func enchMaxLvl(id int8) int8 {
	if int(id) < 0 || int(id) >= len(enchDefs) {
		return 1
	}
	return int8(enchDefs[id].maxLevel)
}

// enchName is an enchantment's registry name ("" for an unknown id).
func enchName(id int8) string {
	if int(id) < 0 || int(id) >= len(enchDefs) {
		return ""
	}
	return enchDefs[id].name
}

var (
	enchByName         = map[string]int8{} // registry name → id
	enchSupportedItems [len(enchDefs)]map[int32]bool
	enchPrimaryItems   [len(enchDefs)]map[int32]bool // nil = every supported item is primary
	itemEnchantability = map[int32]int{}             // the Enchantable component's value
)

func init() {
	resolve := func(names []string) map[int32]bool {
		m := map[int32]bool{}
		for _, n := range names {
			if id, ok := itemByName[n]; ok {
				m[id] = true
			}
		}
		return m
	}
	for i := range enchDefs {
		enchByName[enchDefs[i].name] = int8(i)
		enchSupportedItems[i] = resolve(enchDefs[i].supported)
		if len(enchDefs[i].primary) > 0 {
			enchPrimaryItems[i] = resolve(enchDefs[i].primary)
		}
	}
	// Enchantable values: ToolMaterial / ArmorMaterial enchantmentValue per
	// tier, and the flat ones (Items.java): book, bow, crossbow, trident and
	// fishing rod 1, mace 15.
	tool := map[string]int{"wooden": 15, "stone": 5, "copper": 13, "iron": 14, "golden": 22, "diamond": 10, "netherite": 15}
	armor := map[string]int{"leather": 15, "copper": 8, "chainmail": 12, "iron": 9, "golden": 25, "diamond": 10, "netherite": 15}
	for name, id := range itemByName {
		i := strings.IndexByte(name, '_')
		if i < 0 {
			continue
		}
		tier, kind := name[:i], name[i+1:]
		switch kind {
		case "sword", "axe", "pickaxe", "shovel", "hoe", "spear":
			if v, ok := tool[tier]; ok {
				itemEnchantability[id] = v
			}
		case "helmet", "chestplate", "leggings", "boots":
			if v, ok := armor[tier]; ok {
				itemEnchantability[id] = v
			}
		}
	}
	if id, ok := itemByName["turtle_helmet"]; ok {
		itemEnchantability[id] = 9
	}
	for name, v := range map[string]int{"book": 1, "bow": 1, "crossbow": 1, "trident": 1, "fishing_rod": 1, "mace": 15} {
		if id, ok := itemByName[name]; ok {
			itemEnchantability[id] = v
		}
	}
}

// enchantabilityOf is the item's Enchantable value (0 = the table cannot
// enchant it; an anvil or loot still can, if an enchantment supports it).
func enchantabilityOf(item int32) int { return itemEnchantability[item] }

// enchIsSupported is Enchantment.isSupportedItem: the item is in the
// enchantment's supported set (what an anvil or book may apply).
func enchIsSupported(id int8, item int32) bool {
	return int(id) >= 0 && int(id) < len(enchDefs) && enchSupportedItems[id][item]
}

// enchIsPrimary is Enchantment.isPrimaryItem: supported, and in the primary
// set when one exists (what the TABLE offers — smite is supported on an axe
// but primary only on swords, so an axe never rolls it, yet a book of it can
// be applied).
func enchIsPrimary(id int8, item int32) bool {
	if !enchIsSupported(id, item) {
		return false
	}
	return enchPrimaryItems[id] == nil || enchPrimaryItems[id][item]
}

// enchCompatible is Enchantment.areCompatible.
func enchCompatible(a, b int8) bool {
	if a == b || int(a) < 0 || int(b) < 0 || int(a) >= len(enchDefs) || int(b) >= len(enchDefs) {
		return false
	}
	for _, x := range enchDefs[a].exclusive {
		if x == b {
			return false
		}
	}
	for _, x := range enchDefs[b].exclusive {
		if x == a {
			return false
		}
	}
	return true
}

// enchCompatibleWith reports whether id can join every enchantment already on
// a stack (EnchantmentHelper.isEnchantmentCompatible).
func enchCompatibleWith(id int8, have [2]enchApply) bool {
	for _, e := range have {
		if e.lvl > 0 && !enchCompatible(e.id, id) {
			return false
		}
	}
	return true
}

// Pool filters — the tag each roller selects from.
func enchTableAllowed(id int8) bool { return enchDefs[id].flags&enchInTable != 0 }
func enchLootAllowed(id int8) bool  { return enchDefs[id].flags&enchOnRandomLoot != 0 }
func enchTradeAllowed(id int8) bool { return enchDefs[id].flags&enchTradeable != 0 }

// enchRand is the randomness a roll needs: the hub's rng, or a loot context's
// seeded roller adapted by lootRand.
type enchRand interface {
	Intn(n int) int
	Float32() float32
}

// lootRand adapts a chest's func(int) int roller to enchRand.
type lootRand func(int) int

func (r lootRand) Intn(n int) int   { return r(n) }
func (r lootRand) Float32() float32 { return float32(r(1<<24)) / float32(1<<24) }

type enchInstance struct {
	id  int8
	lvl int8
}

// enchAvailable is getAvailableEnchantmentResults: for every enchantment the
// filter allows that is primary for the item (or the item is a book), the
// HIGHEST level whose [minCost, maxCost] window contains the modified cost.
func enchAvailable(cost int, item int32, allow func(int8) bool) []enchInstance {
	var out []enchInstance
	isBook := item == itemBook
	for i := range enchDefs {
		id := int8(i)
		if !allow(id) || !(isBook || enchIsPrimary(id, item)) {
			continue
		}
		d := &enchDefs[i]
		for lvl := d.maxLevel; lvl >= 1; lvl-- {
			if cost >= d.minCost(lvl) && cost <= d.maxCost(lvl) {
				out = append(out, enchInstance{id: id, lvl: int8(lvl)})
				break
			}
		}
	}
	return out
}

// weightedEnch is WeightedRandom.getRandomItem over the candidates' weights.
func weightedEnch(r enchRand, list []enchInstance) (enchInstance, bool) {
	total := 0
	for _, e := range list {
		total += enchDefs[e.id].weight
	}
	if total <= 0 {
		return enchInstance{}, false
	}
	n := r.Intn(total)
	for _, e := range list {
		if n -= enchDefs[e.id].weight; n < 0 {
			return e, true
		}
	}
	return list[len(list)-1], true
}

// enchSelect is EnchantmentHelper.selectEnchantment: the enchantability
// bonus and ±15% spread on the cost, a weighted first pick, then extra picks
// while nextInt(50) <= cost, each halving the cost and dropping candidates the
// previous pick excludes. Returns nothing for an item with no Enchantable
// value (vanilla returns an empty list) — loot callers pass a book or a tool.
func enchSelect(r enchRand, item int32, cost int, allow func(int8) bool) []enchInstance {
	ev := enchantabilityOf(item)
	if ev == 0 {
		return nil
	}
	cost += 1 + r.Intn(ev/4+1) + r.Intn(ev/4+1)
	span := (r.Float32() + r.Float32() - 1) * 0.15
	cost = int(math.Round(float64(cost) + float64(cost)*float64(span)))
	if cost < 1 {
		cost = 1
	}
	cands := enchAvailable(cost, item, allow)
	if len(cands) == 0 {
		return nil
	}
	var out []enchInstance
	if first, ok := weightedEnch(r, cands); ok {
		out = append(out, first)
	}
	for r.Intn(50) <= cost {
		if len(out) > 0 {
			last := out[len(out)-1]
			kept := cands[:0]
			for _, c := range cands {
				if enchCompatible(last.id, c.id) {
					kept = append(kept, c)
				}
			}
			cands = kept
		}
		if len(cands) == 0 {
			break
		}
		if next, ok := weightedEnch(r, cands); ok {
			out = append(out, next)
		}
		cost /= 2
	}
	return out
}

// enchApplyList writes a selection onto a stack's two enchantment slots
// (ItemStack.enchant for each; the third and later are lost to the cap).
func enchApplyList(list []enchInstance) [2]enchApply {
	var out [2]enchApply
	for i, e := range list {
		if i >= len(out) {
			break
		}
		out[i] = enchApply{id: e.id, lvl: e.lvl}
	}
	return out
}

// enchTableCost is getEnchantmentCost: the level requirement of table row
// `slot` (0-2) for `bookcases` shelves (capped at 15); 0 = not enchantable.
func enchTableCost(r enchRand, slot, bookcases int, item int32) int {
	if enchantabilityOf(item) == 0 {
		return 0
	}
	if bookcases > 15 {
		bookcases = 15
	}
	sel := r.Intn(8) + 1 + bookcases>>1 + r.Intn(bookcases+1)
	switch slot {
	case 0:
		return max(sel/3, 1)
	case 1:
		return sel*2/3 + 1
	}
	return max(sel, bookcases*2)
}

// enchRandomly is loot's enchant_randomly: one uniformly chosen enchantment
// the loot tag allows and the item supports (any, for a book), at a uniform
// level in [1, max].
func enchRandomly(r enchRand, item int32) [2]enchApply {
	var pool []int8
	for i := range enchDefs {
		id := int8(i)
		if enchLootAllowed(id) && (item == itemBook || enchIsSupported(id, item)) {
			pool = append(pool, id)
		}
	}
	if len(pool) == 0 {
		return [2]enchApply{}
	}
	id := pool[r.Intn(len(pool))]
	return [2]enchApply{{id: id, lvl: int8(1 + r.Intn(enchDefs[id].maxLevel))}}
}

// enchWithLevels is loot's enchant_with_levels / the fishing treasure book:
// a full table-style selection at the given level cost from the loot tag
// (which, unlike the table, includes mending, frost walker and the curses).
func enchWithLevels(r enchRand, item int32, levels int) [2]enchApply {
	return enchApplyList(enchSelect(r, item, levels, enchLootAllowed))
}
