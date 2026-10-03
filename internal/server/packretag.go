package server

import (
	"sort"
	"strings"
)

// Vanilla's recipes re-resolving the item tags a data pack changed. A
// recipe's ingredient that names a tag (#minecraft:planks) is a holder set
// bound to the tag, so a pack that adds a plank to #planks makes every
// recipe asking for planks take it, and one that empties #logs_that_burn
// leaves charcoal with nothing to smelt. The engine's vanilla tables were
// expanded from vanilla's tags when they were generated; the ingredients
// written with a tag are kept as written (ingredientSetRefs, cookTagRecipes)
// and a load whose tags differ resolves them again: a crafting ingredient
// set takes the tag's new members (setOverride, which ingredientSet reads),
// and a cooking recipe gains the inputs the tag gained and loses the ones
// it lost. A recipe a pack replaced or removed is left alone.

// retagVanilla re-resolves vanilla's tag-written ingredients against a
// load's tags.
func (pr *packRecipes) retagVanilla(tags *tagRegistry) {
	changed := tags.changedTagSet("item")
	if len(changed) == 0 {
		return
	}
	touches := func(refs []string) bool {
		for _, r := range refs {
			if t, ok := strings.CutPrefix(r, "#"); ok && changed[nsID(t)] {
				return true
			}
		}
		return false
	}
	resolve := func(refs []string) []int32 {
		seen := map[int32]bool{}
		var out []int32
		add := func(it int32) {
			if it != 0 && !seen[it] {
				seen[it] = true
				out = append(out, it)
			}
		}
		for _, r := range refs {
			if t, ok := strings.CutPrefix(r, "#"); ok {
				names, _ := tags.members("item", nsID(t))
				for _, n := range names {
					if it, ok := itemIDOf(n); ok {
						add(it)
					}
				}
				continue
			}
			if it, ok := itemIDOf(r); ok {
				add(it)
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		return out
	}
	for set, refs := range ingredientSetRefs {
		if touches(refs) {
			if pr.setOverride == nil {
				pr.setOverride = map[uint16][]int32{}
			}
			pr.setOverride[set] = resolve(refs) // an empty set: the ingredient matches nothing
		}
	}
	for name, r := range cookTagRecipes {
		if pr.removed[name] || !touches(r.Refs) {
			continue
		}
		now := map[int32]bool{}
		for _, it := range resolve(r.Refs) {
			now[it] = true
			if _, taken := pr.cook[r.Kind][it]; !taken {
				pr.cook[r.Kind][it] = r.Entry
			}
		}
		for _, key := range cookRecipeKeys[name] { // the inputs vanilla's tag gave it
			parts := strings.SplitN(key, "/", 3)
			if len(parts) != 3 {
				continue
			}
			if it, ok := itemByName[parts[2]]; ok && !now[it] {
				pr.cookRemoved[r.Kind][it] = true
			}
		}
	}
}

// changedTagSet is the ids of the tags a load changed in one registry.
func (r *tagRegistry) changedTagSet(reg string) map[string]bool {
	if r == nil {
		return nil
	}
	return r.changed[reg]
}
