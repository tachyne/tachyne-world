package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
)

// Data pack stonecutting, smithing and brewing recipes (the station
// recipes), beside the crafting and cooking ones packrecipes.go applies.
//
// The stonecutter's choices are the client's: it filters the recipe rows
// update_recipes gave it by the input and the menu's button is an index into
// that filtered list, so the engine's menu and the rows the gateways send
// must be one list. A load's list is vanilla's rows (tachyne-common's
// generated table, each row's recipe id in stonecutRowNames) without the
// ones a pack replaced or removed, and the packs' recipes, in the recipe
// registry's order (by path, then namespace — Identifier.compareTo). When a
// load changes that list, or the smithing table's item sets, every Java
// session is sent the new data (attach UpdateRecipes, the reloadResources
// update_recipes) and a later join is given it in its Welcome.
//
// Smithing: a pack's smithing_transform (template and addition optional:
// an absent one wants the slot empty) gives its result with the base's
// components carried across; a smithing_trim puts the addition's trim
// material on the base in the recipe's pattern. Brewing: a pack's brewing
// recipe turns an input (an item, and maybe a potions predicate) and a
// reagent into its output. Pack recipes are tried before vanilla's, as for
// crafting; a pack file at a vanilla id takes that recipe out of the
// engine's tables (vanillaSmithing, vanillaBrewing).

// packStone is a pack's stonecutting recipe.
type packStone struct {
	id    string // namespaced
	items []int32
	out   int32
	count int
}

// packSmith is a pack's smithing recipe.
type packSmith struct {
	trim                     bool
	template, base, addition []int32 // nil template/addition: the slot must be empty
	result                   int32   // transform
	count                    int
	pattern                  int8 // trim: trim_pattern registry id + 1
}

// packBrew is a pack's brewing recipe.
type packBrew struct {
	in, reagent []int32
	potions     map[int8]bool // nil: any (no potion_contents predicate)
	out         invStack
}

// brewKey is a vanilla brewing recipe a pack took away: input item, input
// potion (potNone: any) and reagent.
type brewKey struct {
	in      int32
	potion  int8
	reagent int32
}

// stationRecipes is a load's station recipes.
type stationRecipes struct {
	stone       []packStone
	smith       []packSmith // in id order
	brew        []packBrew  // in id order
	smithIDs    []string    // the smith recipes' ids, parallel
	brewIDs     []string
	noTransform map[[2]int32]bool // vanilla (template, base) transforms taken away
	noTrim      map[int32]bool    // vanilla trim templates taken away
	noBrew      map[brewKey]bool
	stoneDirty  bool // the stonecutter rows differ from vanilla's
	smithDirty  bool

	rows  []attachproto.StonecutterRecipe         // the merged rows (stoneDirty)
	index map[int32][]protocol.StonecuttingRecipe // by input item, in row order
}

func newStationRecipes() *stationRecipes {
	return &stationRecipes{noTransform: map[[2]int32]bool{}, noTrim: map[int32]bool{}, noBrew: map[brewKey]bool{}}
}

// setItems is a set index's items during a build (the pack's own sets are
// not installed yet).
func (pr *packRecipes) setItems(set uint16) []int32 {
	if int(set) < len(ingredientSets) {
		return ingredientSets[set]
	}
	if n := int(set) - len(ingredientSets); n < len(pr.sets) {
		return pr.sets[n]
	}
	return nil
}

// optIngredient reads an optional ingredient field: nil when absent.
func (pr *packRecipes) optIngredient(raw json.RawMessage, tags *tagRegistry) ([]int32, error) {
	if raw == nil {
		return nil, nil
	}
	set, err := pr.ingredient(raw, tags)
	if err != nil {
		return nil, err
	}
	return pr.setItems(set), nil
}

// addStation parses a station recipe. ok is false for a type that is not
// one.
func (pr *packRecipes) addStation(id, typ string, top map[string]json.RawMessage, tags *tagRegistry) (bool, error) {
	st := pr.station
	switch typ {
	case "minecraft:stonecutting":
		set, err := pr.ingredient(top["ingredient"], tags)
		if err != nil {
			return true, err
		}
		out, count, err := recipeResult(top["result"])
		if err != nil {
			return true, err
		}
		st.stone = append(st.stone, packStone{id: id, items: pr.setItems(set), out: out, count: int(count)})
		st.stoneDirty = true
		return true, nil
	case "minecraft:smithing_transform", "minecraft:smithing_trim":
		trim := typ == "minecraft:smithing_trim"
		var r packSmith
		var err error
		r.trim = trim
		if trim && top["template"] == nil {
			return true, errors.New("No key template in MapLike")
		}
		if r.template, err = pr.optIngredient(top["template"], tags); err != nil {
			return true, err
		}
		base, err := pr.ingredient(top["base"], tags)
		if err != nil {
			return true, err
		}
		r.base = pr.setItems(base)
		if trim && top["addition"] == nil {
			return true, errors.New("No key addition in MapLike")
		}
		if r.addition, err = pr.optIngredient(top["addition"], tags); err != nil {
			return true, err
		}
		if trim {
			var name string
			if err := json.Unmarshal(top["pattern"], &name); err != nil {
				return true, errors.New("a trim pattern is a registry id here")
			}
			id, ok := registryIDs("minecraft:trim_pattern")[nsID(name)]
			if !ok {
				return true, fmt.Errorf("unknown trim pattern %s", name)
			}
			r.pattern = int8(id + 1)
		} else {
			out, count, err := recipeResult(top["result"])
			if err != nil {
				return true, err
			}
			r.result, r.count = out, int(count)
		}
		st.smith = append(st.smith, r)
		st.smithIDs = append(st.smithIDs, id)
		st.smithDirty = true
		return true, nil
	case "minecraft:brewing":
		var r packBrew
		var err error
		if r.in, r.potions, err = pr.potionIngredient(top["input"], tags); err != nil {
			return true, err
		}
		if r.reagent, _, err = pr.potionIngredient(top["reagent"], tags); err != nil {
			return true, err
		}
		if r.out, err = brewOutput(top["output"]); err != nil {
			return true, err
		}
		st.brew = append(st.brew, r)
		st.brewIDs = append(st.brewIDs, id)
		return true, nil
	}
	return false, nil
}

// potionIngredient reads PotionIngredient: an ingredient under "item" and
// an optional potion_contents predicate (its potions).
func (pr *packRecipes) potionIngredient(raw json.RawMessage, tags *tagRegistry) ([]int32, map[int8]bool, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, nil, errors.New("a potion ingredient is an object")
	}
	set, err := pr.ingredient(m["item"], tags)
	if err != nil {
		return nil, nil, err
	}
	var pots map[int8]bool
	if pc, ok := m["potion_contents"]; ok {
		var p struct {
			Potions json.RawMessage `json:"potions"`
		}
		if err := json.Unmarshal(pc, &p); err != nil {
			return nil, nil, errors.New("bad potion_contents")
		}
		if p.Potions != nil {
			var v any
			_ = json.Unmarshal(p.Potions, &v)
			pots = map[int8]bool{}
			for _, n := range holderSetNames(v) {
				if tag, isTag := strings.CutPrefix(n, "#"); isTag {
					members, ok := tags.members("potion", nsID(tag))
					if !ok {
						return nil, nil, fmt.Errorf("unknown potion tag %s", tag)
					}
					for _, mb := range members {
						if k, ok := potionByVanillaName[strings.TrimPrefix(mb, "minecraft:")]; ok {
							pots[k] = true
						}
					}
					continue
				}
				k, ok := potionByVanillaName[strings.TrimPrefix(nsID(n), "minecraft:")]
				if !ok {
					return nil, nil, fmt.Errorf("unknown potion %s", n)
				}
				pots[k] = true
			}
		}
	}
	return pr.setItems(set), pots, nil
}

// brewOutput reads a brewing recipe's output (ItemStackTemplate): the item,
// its count, and the potion its potion_contents names.
func brewOutput(raw json.RawMessage) (invStack, error) {
	it, count, err := recipeResult(raw)
	if err != nil {
		return invStack{}, err
	}
	st := invStack{item: it, count: int(count)}
	var r struct {
		Components map[string]json.RawMessage `json:"components"`
	}
	_ = json.Unmarshal(raw, &r)
	for k, v := range r.Components {
		if nsID(k) != "minecraft:potion_contents" {
			continue
		}
		var name string
		if json.Unmarshal(v, &name) != nil {
			var pc struct {
				Potion string `json:"potion"`
			}
			if err := json.Unmarshal(v, &pc); err != nil {
				return invStack{}, errors.New("bad potion_contents")
			}
			name = pc.Potion
		}
		if name == "" {
			continue
		}
		k, ok := potionByVanillaName[strings.TrimPrefix(nsID(name), "minecraft:")]
		if !ok {
			return invStack{}, fmt.Errorf("unknown potion %s", name)
		}
		st = potionStackIn(it, k)
		st.count = int(count)
	}
	return st, nil
}

// removeVanillaStation takes a vanilla station recipe out of the game.
func (pr *packRecipes) removeVanillaStation(name string) {
	st := pr.station
	for _, n := range stonecutRowNames {
		if n == name {
			st.stoneDirty = true
			break
		}
	}
	if s, ok := vanillaSmithing[name]; ok {
		st.smithDirty = true
		if s.trim {
			for _, t := range s.templates {
				st.noTrim[t] = true
			}
		} else {
			for _, t := range s.templates {
				for _, b := range s.bases {
					st.noTransform[[2]int32{t, b}] = true
				}
			}
		}
	}
	if b, ok := vanillaBrewing[name]; ok {
		var pot int8 = potNone
		if b.potion != "" {
			if k, ok := potionByVanillaName[b.potion]; ok {
				pot = k
			}
		}
		for _, rg := range b.reagents {
			st.noBrew[brewKey{in: b.in, potion: pot, reagent: rg}] = true
		}
	}
}

// identifierLess is Identifier.compareTo: the path first, then the
// namespace.
func identifierLess(a, b string) bool {
	an, ap, _ := strings.Cut(a, ":")
	bn, bp, _ := strings.Cut(b, ":")
	if ap != bp {
		return ap < bp
	}
	return an < bn
}

// finishStations merges the stonecutter rows once every file is read.
func (pr *packRecipes) finishStations() {
	st := pr.station
	if !st.stoneDirty {
		return
	}
	all := make([]packStone, 0, len(protocol.StonecuttingRecipes)+len(st.stone))
	for i, r := range protocol.StonecuttingRecipes {
		if i >= len(stonecutRowNames) || pr.removed[stonecutRowNames[i]] {
			continue
		}
		all = append(all, packStone{id: "minecraft:" + stonecutRowNames[i], items: []int32{r.In}, out: r.Out, count: int(r.Count)})
	}
	all = append(all, st.stone...)
	sort.SliceStable(all, func(i, j int) bool { return identifierLess(all[i].id, all[j].id) })
	st.rows = make([]attachproto.StonecutterRecipe, 0, len(all))
	st.index = map[int32][]protocol.StonecuttingRecipe{}
	for _, r := range all {
		st.rows = append(st.rows, attachproto.StonecutterRecipe{
			Input: attachproto.RecipeIngredient{Items: append([]int32(nil), r.items...)}, Result: r.out, Count: int32(r.count)})
		for _, in := range r.items {
			st.index[in] = append(st.index[in], protocol.StonecuttingRecipe{In: in, Out: r.out, Count: int8(r.count)})
		}
	}
}

// stationsOf is a load's station recipes (nil: none, or no load).
func stationsOf(pc *packContent) *stationRecipes {
	if pr := pc.recipeSet(); pr != nil {
		return pr.station
	}
	return nil
}

// stonecutList is the stonecutter's choices for an input item, in the
// order the client lists them.
func stonecutList(item int32) []protocol.StonecuttingRecipe {
	if st := stationsOf(currentPack()); st != nil && st.stoneDirty {
		return st.index[item]
	}
	return stonecutIndex[item]
}

// packUpdateRecipes is the recipe data a load gives the clients: nil when
// it is vanilla's (the gateways' own tables), else each half that differs
// (a nil half: vanilla's).
func packUpdateRecipes(pc *packContent) *attachproto.UpdateRecipes {
	st := stationsOf(pc)
	if st == nil || (!st.stoneDirty && !st.smithDirty) {
		return nil
	}
	u := &attachproto.UpdateRecipes{}
	if st.stoneDirty {
		u.Stonecutter = st.rows
	}
	if st.smithDirty {
		u.ItemSets = smithingItemSets(st)
	}
	return u
}

// smithingItemSets are the smithing table's property sets (the items each
// slot takes): vanilla's, with the packs' recipes' items added.
func smithingItemSets(st *stationRecipes) []attachproto.RecipePropertySet {
	tmpl := map[int32]bool{protocol.SmithingUpgradeTemplate: true}
	base, add := map[int32]bool{}, map[int32]bool{}
	for t := range protocol.SmithingTrimTemplate {
		tmpl[t] = true
	}
	for _, b := range protocol.SmithingTrimmable {
		base[b] = true
	}
	for b := range protocol.SmithingTransform {
		base[b] = true
	}
	for m := range protocol.SmithingTrimMaterial {
		add[m] = true
	}
	for _, r := range st.smith {
		for _, it := range r.template {
			tmpl[it] = true
		}
		for _, it := range r.base {
			base[it] = true
		}
		for _, it := range r.addition {
			add[it] = true
		}
	}
	list := func(m map[int32]bool) []int32 {
		out := make([]int32, 0, len(m))
		for it := range m {
			out = append(out, it)
		}
		sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
		return out
	}
	return []attachproto.RecipePropertySet{
		{Key: "minecraft:smithing_template", Items: list(tmpl)},
		{Key: "minecraft:smithing_base", Items: list(base)},
		{Key: "minecraft:smithing_addition", Items: list(add)},
	}
}

func containsItem(set []int32, it int32) bool {
	for _, x := range set {
		if x == it {
			return true
		}
	}
	return false
}

// optFits is Ingredient.testOptionalIngredient: an absent ingredient wants
// an empty slot.
func optFits(set []int32, st invStack) bool {
	empty := st.item == 0 || st.count <= 0
	if set == nil {
		return empty
	}
	return !empty && containsItem(set, st.item)
}

// packSmithResult tries the packs' smithing recipes: the result, and
// whether one matched.
func packSmithResult(tmpl, base, add invStack) (invStack, bool) {
	st := stationsOf(currentPack())
	if st == nil || base.item == 0 || base.count <= 0 {
		return invStack{}, false
	}
	for _, r := range st.smith {
		if !optFits(r.template, tmpl) || !containsItem(r.base, base.item) || !optFits(r.addition, add) {
			continue
		}
		if !r.trim { // SmithingTransformRecipe.assemble: base.transmuteCopy(result)
			res := base
			res.item, res.count = r.result, r.count
			return res, true
		}
		mat, ok := protocol.SmithingTrimMaterial[add.item] // provides_trim_material
		if !ok {
			return invStack{}, true
		}
		res := base
		res.count = 1
		res.trimMat, res.trimPat = int8(mat+1), r.pattern
		if res.trimMat == base.trimMat && res.trimPat == base.trimPat {
			return invStack{}, true
		}
		return res, true
	}
	return invStack{}, false
}

// vanillaSmithAllowed reports whether a vanilla smithing recipe for this
// template and base is still in the game.
func vanillaSmithAllowed(tmpl, base int32, trim bool) bool {
	st := stationsOf(currentPack())
	if st == nil {
		return true
	}
	if trim {
		return !st.noTrim[tmpl]
	}
	return !st.noTransform[[2]int32{tmpl, base}]
}

// packBrewOne tries the packs' brewing recipes on one bottle.
func packBrewOne(bottle invStack, reagent int32) (invStack, bool) {
	st := stationsOf(currentPack())
	if st == nil {
		return invStack{}, false
	}
	for _, r := range st.brew {
		if !containsItem(r.in, bottle.item) || !containsItem(r.reagent, reagent) {
			continue
		}
		if r.potions != nil && (bottle.potion == potNone || !r.potions[bottle.potion]) {
			continue
		}
		return r.out, true
	}
	return invStack{}, false
}

// vanillaBrewAllowed reports whether vanilla's recipe for a bottle and a
// reagent is still in the game.
func vanillaBrewAllowed(bottle invStack, reagent int32) bool {
	st := stationsOf(currentPack())
	if st == nil || len(st.noBrew) == 0 {
		return true
	}
	return !st.noBrew[brewKey{bottle.item, bottle.potion, reagent}] && !st.noBrew[brewKey{bottle.item, potNone, reagent}]
}

// packBrewReagent and packBrewInput report whether a pack's brewing recipe
// takes an item as its reagent or its input (PotionBrewing's isIngredient
// and the input property set).
func packBrewReagent(item int32) bool {
	if st := stationsOf(currentPack()); st != nil {
		for _, r := range st.brew {
			if containsItem(r.reagent, item) {
				return true
			}
		}
	}
	return false
}

func packBrewInput(item int32) bool {
	if st := stationsOf(currentPack()); st != nil {
		for _, r := range st.brew {
			if containsItem(r.in, item) {
				return true
			}
		}
	}
	return false
}
