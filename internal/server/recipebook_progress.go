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
	for i := range shapedRecipes {
		add(int32(i), shapedRecipes[i].Cells)
	}
	for i := range shapelessRecipes {
		add(int32(len(shapedRecipes)+i), shapelessRecipes[i].Ingredients)
	}
	// A cooker recipe unlocks on its single input, the same rule.
	for i := range cookBookRecipes {
		add(cookBookRecipes[i].ID, []int32{cookBookRecipes[i].Ingredient})
	}
	return idx
}()

// rbBuildEntries assembles the attach frame for a set of display ids.
func rbBuildEntries(ids []int32, replace, notify bool, highlighted map[int32]bool) attachproto.RecipeBook {
	rb := attachproto.RecipeBook{Replace: replace}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		hl := highlighted[id]
		if int(id) < len(shapedRecipes) {
			r := &shapedRecipes[id]
			rb.Shaped = append(rb.Shaped, attachproto.ShapedRecipe{
				ID: id, W: int32(r.W), H: int32(r.H), Cells: r.Cells,
				Result: r.Result, Count: int32(r.Count), Notify: notify, Highlight: hl,
			})
		} else if n := int(id) - len(shapedRecipes); n < len(shapelessRecipes) {
			r := &shapelessRecipes[n]
			rb.Shapeless = append(rb.Shapeless, attachproto.ShapelessRecipe{
				ID: id, Ingredients: r.Ingredients,
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
			if !t.rbKnown[id] {
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

// rbState is one player's persisted book.
type rbState struct {
	Known     []int32 `json:"known"`
	Highlight []int32 `json:"highlight,omitempty"`
	Open      [4]bool `json:"open"`
	Filter    [4]bool `json:"filter"`
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
	t.rbKnown = make(map[int32]bool, len(st.Known))
	t.rbHighlight = make(map[int32]bool, len(st.Highlight))
	for _, id := range st.Known {
		t.rbKnown[id] = true
	}
	for _, id := range st.Highlight {
		t.rbHighlight[id] = true
	}
	t.rbSettings = attachproto.RecipeSettings{Open: st.Open, Filter: st.Filter}
}

func (s *recipeBookStore) record(name string, t *tracked) {
	if t.rbKnown == nil {
		return
	}
	st := rbState{Open: t.rbSettings.Open, Filter: t.rbSettings.Filter}
	for id := range t.rbKnown {
		st.Known = append(st.Known, id)
	}
	for id := range t.rbHighlight {
		st.Highlight = append(st.Highlight, id)
	}
	sort.Slice(st.Known, func(i, j int) bool { return st.Known[i] < st.Known[j] })
	sort.Slice(st.Highlight, func(i, j int) bool { return st.Highlight[i] < st.Highlight[j] })
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
