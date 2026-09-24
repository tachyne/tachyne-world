package server

// Recipe-book progression, mirroring the vanilla ServerRecipeBook model: a
// per-player KNOWN set (the client only ever learns known recipes — the whole
// book at join, increments as unlocks happen), a HIGHLIGHT set (the "new"
// badge, cleared when the client views the entry), and the per-book-type
// open/filter settings the client round-trips. Unlocks are derived the way
// vanilla's recipe-unlock advancements work: obtaining any ingredient of a
// recipe reveals it.

import (
	"encoding/json"
	"log"
	"sort"
	"sync"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Display ids are canonical recipe indices: shaped recipes first, then
// shapeless — the same numbering placeRecipe resolves — and finally the
// cooking recipes, which are book-only (a furnace has no craft_recipe_request
// path, so they sit above the ids placeRecipe will ever be handed).

// cookBookRecipe is one book entry for a cooker recipe, pre-built at init.
// The four cooker tables are flattened in a fixed order (furnace, blast
// furnace, smoker, campfire), each sorted by input item, so a display id
// means the same recipe across restarts — which is what makes a persisted
// book restore correctly.
var cookBookRecipes = func() []attachproto.CookingRecipe {
	tables := []struct {
		m       map[int32]cookEntry
		station int32
	}{
		{smeltResult, itemByName["furnace"]},
		{blastResult, itemByName["blast_furnace"]},
		{smokeResult, itemByName["smoker"]},
		{campfireResult, itemByName["campfire"]},
	}
	id := int32(len(shapedRecipes) + len(shapelessRecipes))
	var out []attachproto.CookingRecipe
	for _, t := range tables {
		inputs := make([]int32, 0, len(t.m))
		for in := range t.m {
			inputs = append(inputs, in)
		}
		sort.Slice(inputs, func(i, j int) bool { return inputs[i] < inputs[j] })
		for _, in := range inputs {
			e := t.m[in]
			out = append(out, attachproto.CookingRecipe{
				ID: id, Ingredient: in, Result: e.Out, Count: 1,
				Cook: int32(e.Cook), XP: float32(e.XP),
				Station: t.station, Category: e.Cat,
			})
			id++
		}
	}
	return out
}()

// cookBookFirstID is the display id the cooking block starts at.
var cookBookFirstID = int32(len(shapedRecipes) + len(shapelessRecipes))

// rbIngredientIndex maps an item id to the display ids of every recipe using
// it as an ingredient (the unlock rule's lookup).
var rbIngredientIndex = func() map[int32][]int32 {
	idx := map[int32][]int32{}
	add := func(id int32, items []int32) {
		seen := map[int32]bool{}
		for _, it := range items {
			if it != 0 && !seen[it] {
				seen[it] = true
				idx[it] = append(idx[it], id)
			}
		}
	}
	// Every item an ingredient accepts unlocks the recipe: holding spruce
	// planks reveals the crafting table as much as oak does.
	accepted := func(sets []uint16) []int32 {
		var items []int32
		for _, set := range sets {
			items = append(items, ingredientSets[set]...)
		}
		return items
	}
	for i := range shapedRecipes {
		add(int32(i), accepted(shapedRecipes[i].Cells))
	}
	for i := range shapelessRecipes {
		add(int32(len(shapedRecipes)+i), accepted(shapelessRecipes[i].Ingredients))
	}
	// A cooker recipe unlocks on its single input, the same rule.
	for i := range cookBookRecipes {
		add(cookBookRecipes[i].ID, []int32{cookBookRecipes[i].Ingredient})
	}
	return idx
}()

// representatives turns ingredient sets into the one item per slot the book
// frame carries: each set's first item, 0 for an empty cell. Vanilla's book
// cycles through every item an ingredient accepts; the frame does not carry
// the whole set yet, so the book shows one — crafting itself accepts them all.
func representatives(sets []uint16) []int32 {
	out := make([]int32, len(sets))
	for i, set := range sets {
		if s := ingredientSets[set]; len(s) > 0 {
			out[i] = s[0]
		}
	}
	return out
}

// rbBuildEntries assembles the attach frame for a set of display ids.
func rbBuildEntries(ids []int32, replace, notify bool, highlighted map[int32]bool) attachproto.RecipeBook {
	rb := attachproto.RecipeBook{Replace: replace}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		hl := highlighted[id]
		if int(id) < len(shapedRecipes) {
			r := &shapedRecipes[id]
			rb.Shaped = append(rb.Shaped, attachproto.ShapedRecipe{
				ID: id, W: int32(r.W), H: int32(r.H), Cells: representatives(r.Cells),
				Result: r.Result, Count: int32(r.Count), Notify: notify, Highlight: hl,
			})
		} else if n := int(id) - len(shapedRecipes); n < len(shapelessRecipes) {
			r := &shapelessRecipes[n]
			rb.Shapeless = append(rb.Shapeless, attachproto.ShapelessRecipe{
				ID: id, Ingredients: representatives(r.Ingredients),
				Result: r.Result, Count: int32(r.Count), Notify: notify, Highlight: hl,
			})
		} else if n := int(id - cookBookFirstID); n >= 0 && n < len(cookBookRecipes) {
			r := cookBookRecipes[n]
			r.Notify, r.Highlight = notify, hl
			rb.Cooking = append(rb.Cooking, r)
		}
	}
	return rb
}

// recipeSendInitial ships the player's settings + their known book (vanilla
// sendInitialRecipeBook: settings packet, then one replace=true add carrying
// the highlight flags).
func (h *hub) recipeSendInitial(t *tracked) {
	t.p.sendEv(t.rbSettings)
	ids := make([]int32, 0, len(t.rbKnown))
	for id := range t.rbKnown {
		ids = append(ids, id)
	}
	t.p.sendEv(rbBuildEntries(ids, true, false, t.rbHighlight))
}

// recipeUnlocks reveals recipes whose ingredients the player now holds
// (called from the 1 Hz poll with the already-collected inventory item ids).
// New entries arrive with the notification toast + highlight badge, exactly
// like vanilla's addRecipes.
func (h *hub) recipeUnlocks(t *tracked, invItems []int32) {
	if t.rbKnown == nil {
		return
	}
	var fresh []int32
	for _, item := range invItems {
		for _, id := range rbIngredientIndex[item] {
			if !t.rbKnown[id] && !t.rbTaken[id] {
				t.rbKnown[id] = true
				t.rbHighlight[id] = true
				fresh = append(fresh, id)
			}
		}
	}
	if len(fresh) == 0 {
		return
	}
	all := map[int32]bool{}
	for _, id := range fresh {
		all[id] = true
	}
	t.p.trySendEv(rbBuildEntries(fresh, false, true, all))
}

// A saved book stores recipe NAMES, as vanilla's ServerRecipeBook stores recipe
// keys; display ids are only what this build hands the client. The book used
// to store the ids themselves, which were indices into the recipe table — so
// any change to the table (the 2026-09-22 move to vanilla's own recipes, which
// folded twelve per-wood crafting tables into one; or a new Minecraft version
// adding recipes) would have pointed every saved entry at a different recipe.

// recipeNames[id] is the saved name of display id id: a crafting recipe's
// vanilla name, or a cooking entry's cookRecipeKey.
var recipeNames = func() []string {
	names := make([]string, 0, len(shapedRecipes)+len(shapelessRecipes)+len(cookBookRecipes))
	for _, r := range shapedRecipes {
		names = append(names, r.Name)
	}
	for _, r := range shapelessRecipes {
		names = append(names, r.Name)
	}
	for _, r := range cookBookRecipes {
		names = append(names, cookRecipeKey(r))
	}
	return names
}()

// recipeIDByName is recipeNames the other way round.
var recipeIDByName = func() map[string]int32 {
	m := make(map[string]int32, len(recipeNames))
	for id, n := range recipeNames {
		m[n] = int32(id)
	}
	return m
}()

// cookRecipeKey names a cooking entry for the saved book. The engine's cooker
// tables are keyed by input rather than by vanilla recipe name, so the key is
// built from names that do not move between versions — station and input,
// e.g. "cook/furnace/raw_iron". The slash keeps it apart from vanilla's names.
func cookRecipeKey(r attachproto.CookingRecipe) string {
	return "cook/" + itemNameOf[r.Station] + "/" + itemNameOf[r.Ingredient]
}

// legacyRecipeName is the name an id saved by the old index-based book stood
// for: crafting ids through the frozen legacyRecipeNames, cooking ids by
// position, since the cooking entries are still built in the same order.
// "" when the id meant nothing.
func legacyRecipeName(id int32) string {
	switch {
	case id < 0:
		return ""
	case id < legacyCraftRecipeCount:
		return legacyRecipeNames[id]
	case int(id-legacyCraftRecipeCount) < len(cookBookRecipes):
		return cookRecipeKey(cookBookRecipes[id-legacyCraftRecipeCount])
	}
	return ""
}

// rbState is one player's persisted book.
type rbState struct {
	Names          []string `json:"names,omitempty"`
	HighlightNames []string `json:"highlight_names,omitempty"`
	// Known and Highlight are the old index-based form. A book saved before
	// names is read through legacyRecipeName once, and written back as names.
	Known     []int32 `json:"known,omitempty"`
	Highlight []int32 `json:"highlight,omitempty"`
	// TakenNames are recipes /recipe take removed: vanilla's recipe
	// advancement stays done, so holding the ingredients again does not
	// bring them back. Only /recipe give does.
	TakenNames []string `json:"taken_names,omitempty"`
	Open       [4]bool  `json:"open"`
	Filter     [4]bool  `json:"filter"`
}

// recipeBookStore persists per-player books (recipebook.json), the same
// shape/cadence as the advancement and stats stores.
type recipeBookStore struct {
	mu   sync.Mutex
	path string
	m    map[string]rbState
}

func newRecipeBookStore(path string) *recipeBookStore {
	s := &recipeBookStore{path: path, m: map[string]rbState{}}
	if path != "" {
		if err := loadStore(path, &s.m); err != nil {
			log.Fatal(err)
		}
	}
	return s
}

// loadInto restores a player's book state (fresh maps when unknown).
func (s *recipeBookStore) loadInto(t *tracked, name string) {
	s.mu.Lock()
	st := s.m[name]
	s.mu.Unlock()
	known, highlight := st.Names, st.HighlightNames
	if len(known) == 0 && len(st.Known) > 0 { // saved by the index-based book
		for _, id := range st.Known {
			known = append(known, legacyRecipeName(id))
		}
		for _, id := range st.Highlight {
			highlight = append(highlight, legacyRecipeName(id))
		}
	}
	t.rbKnown = make(map[int32]bool, len(known))
	t.rbHighlight = make(map[int32]bool, len(highlight))
	// A name this build does not have — a recipe a later version removed — is
	// dropped, as vanilla drops a recipe key it cannot resolve.
	for _, n := range known {
		if id, ok := recipeIDByName[n]; ok {
			t.rbKnown[id] = true
		}
	}
	for _, n := range highlight {
		if id, ok := recipeIDByName[n]; ok {
			t.rbHighlight[id] = true
		}
	}
	t.rbTaken = nil
	for _, n := range st.TakenNames {
		if id, ok := recipeIDByName[n]; ok {
			if t.rbTaken == nil {
				t.rbTaken = map[int32]bool{}
			}
			t.rbTaken[id] = true
		}
	}
	t.rbSettings = attachproto.RecipeSettings{Open: st.Open, Filter: st.Filter}
}

func (s *recipeBookStore) record(name string, t *tracked) {
	if t.rbKnown == nil {
		return
	}
	st := rbState{Open: t.rbSettings.Open, Filter: t.rbSettings.Filter}
	for id := range t.rbKnown {
		if int(id) < len(recipeNames) {
			st.Names = append(st.Names, recipeNames[id])
		}
	}
	for id := range t.rbHighlight {
		if int(id) < len(recipeNames) {
			st.HighlightNames = append(st.HighlightNames, recipeNames[id])
		}
	}
	for id := range t.rbTaken {
		if int(id) < len(recipeNames) {
			st.TakenNames = append(st.TakenNames, recipeNames[id])
		}
	}
	sort.Strings(st.Names)
	sort.Strings(st.HighlightNames)
	sort.Strings(st.TakenNames)
	s.mu.Lock()
	s.m[name] = st
	s.mu.Unlock()
}

func (s *recipeBookStore) flush() {
	s.mu.Lock()
	data, _ := json.MarshalIndent(s.m, "", "  ")
	path := s.path
	s.mu.Unlock()
	if path == "" {
		return
	}
	writeStore(path, data)
}

func (s *recipeBookStore) save(name string, t *tracked) {
	s.record(name, t)
	s.flush()
}

// Serverbound: the client toggled a book tab's open/filter state, or viewed
// a highlighted entry (clears the badge).
type evRecipeSettings struct {
	eid          int32
	book         int32
	open, filter bool
}

func (evRecipeSettings) isHubEvent() {}

type evRecipeSeen struct {
	eid int32
	id  int32
}

func (evRecipeSeen) isHubEvent() {}
