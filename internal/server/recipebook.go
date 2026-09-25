package server

import attachproto "github.com/tachyne/tachyne-common/attach"

// Recipe book: clicking a book entry sends craft_recipe_request; display ids
// are canonical recipe indices (shaped first, then shapeless). Which entries
// the client KNOWS is per-player progression — see recipebook_progress.go.
// (Legacy note: on join the client used to get every recipe as a display
// entry (recipe_book_add 0x43), so the green book actually lists what can be
// made instead of sitting empty. Clicking a book entry sends
// craft_recipe_request (0x25) with our display id; the hub then auto-fills the
// crafting grid from the player's inventory (the "click to arrange" UX), and
// the player takes the result as usual. Display ids are indices into our
// generated tables: shaped first, then shapeless.
//
// The wire encoding lives in the gateway (tachyne-common/render770, built at
// the client's real protocol version because recipe_book_add has no body
// rewriter in the translation chain); the engine just emits the canonical
// (770-id) recipe list once at join.

const (
	playServerCraftRequest = 0x25 // craft_recipe_request (place a book recipe)

	recipeCategoryMisc = 3 // crafting_misc book tab (cosmetic placement)
)

// evCraftRequest is a click on a recipe-book entry: fill the grid with its
// ingredients from the inventory.
type evCraftRequest struct {
	eid      int32
	windowID int32
	recipeID int32
}

func (evCraftRequest) isHubEvent() {}

// ---- request handling --------------------------------------------------------

// placeRecipe auto-fills the active crafting grid with a book recipe's
// ingredients pulled from the player's inventory: the server-side answer to
// clicking a recipe in the book. Fills only when the recipe fits the open grid
// and every ingredient is available; otherwise it's a no-op (the client keeps
// its ghost preview).
func (h *hub) placeRecipe(players map[int32]*tracked, t *tracked, e evCraftRequest) {
	if t.inv == nil || e.windowID != t.winID {
		return
	}
	if t.winKind == winFurnace {
		h.placeCookRecipe(t, e.recipeID)
		return
	}
	if t.winKind != winPlayer && t.winKind != winCraft {
		return
	}
	w := gridSize(t)

	// Work out which grid cell needs which ingredient (top-left aligned for
	// shaped).
	type needCell struct {
		cell int
		set  uint16
	}
	var need []needCell
	switch {
	case int(e.recipeID) < len(shapedRecipes):
		r := &shapedRecipes[e.recipeID]
		if int(r.W) > w || int(r.H) > w {
			return // 3x3 recipe requested in the 2x2 player grid
		}
		for row := 0; row < int(r.H); row++ {
			for col := 0; col < int(r.W); col++ {
				if set := r.Cells[row*int(r.W)+col]; set != 0 {
					need = append(need, needCell{row*w + col, set})
				}
			}
		}
	case int(e.recipeID) < len(shapedRecipes)+len(shapelessRecipes):
		r := &shapelessRecipes[int(e.recipeID)-len(shapedRecipes)]
		if len(r.Ingredients) > w*w {
			return
		}
		for i, set := range r.Ingredients {
			need = append(need, needCell{i, set})
		}
	default:
		return
	}

	h.reclaimCraft(players, t) // return whatever's in the grid first

	// Choose an item for every cell from what the inventory holds.
	supply := map[int32]int{}
	var order []int32 // distinct items, in inventory order
	for _, s := range t.inv.slots {
		if s.item != 0 && s.count > 0 {
			if _, seen := supply[s.item]; !seen {
				order = append(order, s.item)
			}
			supply[s.item] += s.count
		}
	}
	sets := make([]uint16, len(need))
	for i, n := range need {
		sets[i] = n.set
	}
	pick := assignIngredients(sets, order, supply)
	if pick == nil {
		// handlePlaceRecipe's PLACE_GHOST_RECIPE: the grid (already
		// emptied) shows the recipe's ingredients as ghosts.
		t.p.trySendEv(ghostRecipeFor(e.windowID, e.recipeID))
		return
	}

	// Pull one of each needed item out of the inventory into its grid cell.
	for k, n := range need {
		item := pick[k]
		for i := range t.inv.slots {
			s := &t.inv.slots[i]
			if s.item == item && s.count > 0 {
				if s.count--; s.count == 0 {
					s.item = 0
				}
				h.sendSlot(t, i)
				break
			}
		}
		t.craft[n.cell] = invStack{item: item, count: t.craft[n.cell].count + 1}
	}
	for i := 0; i < w*w; i++ {
		h.sendWinSlot(t, int16(i+1), t.craft[i])
	}
	h.sendCraftResult(t)
}

// cookStationItem is the item a cooker kind shows as its station icon — the
// same value cookBookRecipes files its entries under.
func cookStationItem(kind int8) int32 {
	switch kind {
	case cookBlast:
		return itemByName["blast_furnace"]
	case cookSmoker:
		return itemByName["smoker"]
	}
	return itemByName["furnace"]
}

// placeCookRecipe answers a click on a furnace-book entry. Vanilla's
// ServerPlaceRecipe for a cooker menu moves ONE of the recipe's ingredient
// from the inventory into the input slot; it refuses when the open cooker is
// not the one the entry belongs to (a blasting entry clicked in a smoker), or
// when the input already holds something else.
func (h *hub) placeCookRecipe(t *tracked, id int32) {
	n := int(id - cookBookFirstID)
	if n < 0 || n >= len(cookBookRecipes) {
		return
	}
	r := cookBookRecipes[n]
	f := h.furnaces[t.winPos]
	if f == nil || r.Station != cookStationItem(f.kind) {
		return
	}
	if in := f.slots[0]; in.item != 0 && (in.item != r.Ingredient || in.count >= 64) {
		return
	}
	for i := range t.inv.slots {
		s := &t.inv.slots[i]
		if s.item != r.Ingredient || s.count == 0 {
			continue
		}
		if s.count--; s.count == 0 {
			s.item = 0
		}
		h.sendSlot(t, i)
		f.slots[0].item = r.Ingredient
		f.slots[0].count++
		h.sendWinSlot(t, 0, f.slots[0])
		return
	}
}

// assignIngredients picks, for each ingredient in need, an item that set
// accepts, drawing no item more times than supply holds — the assignment
// vanilla's StackedItemContents.canCraft solves before ServerPlaceRecipe fills
// a grid, which is what lets a crafting table be filled from two oak planks
// and two spruce. It is a small max-flow (Kuhn's augmenting paths, with each
// item a capacity-limited sink), so a greedy first choice that would strand a
// later cell is undone rather than failing. Items are tried in order, so the
// earlier stacks in the inventory are preferred. Returns nil when the recipe
// cannot be filled.
func assignIngredients(need []uint16, order []int32, supply map[int32]int) []int32 {
	pick := make([]int32, len(need))
	used := map[int32]int{}
	var augment func(i int, seen map[int32]bool) bool
	augment = func(i int, seen map[int32]bool) bool {
		for _, it := range order {
			if seen[it] || !ingredientAccepts(need[i], it) {
				continue
			}
			seen[it] = true
			if used[it] < supply[it] {
				used[it]++
				pick[i] = it
				return true
			}
			// Every unit of it is taken: see whether one holder can move to
			// another item, and take its unit if so.
			for j := range need {
				if j != i && pick[j] == it && augment(j, seen) {
					pick[i] = it
					return true
				}
			}
		}
		return false
	}
	for i := range need {
		if !augment(i, map[int32]bool{}) {
			return nil
		}
	}
	return pick
}

// ghostRecipeFor is the display of a book recipe, for place_ghost_recipe.
func ghostRecipeFor(window, id int32) attachproto.GhostRecipe {
	g := attachproto.GhostRecipe{Window: window}
	switch {
	case int(id) < len(shapedRecipes):
		r := &shapedRecipes[id]
		g.Shaped = &attachproto.ShapedRecipe{ID: id, W: int32(r.W), H: int32(r.H), Cells: representatives(r.Cells), Result: r.Result, Count: int32(r.Count)}
	case int(id) < len(shapedRecipes)+len(shapelessRecipes):
		r := &shapelessRecipes[int(id)-len(shapedRecipes)]
		g.Shapeless = &attachproto.ShapelessRecipe{ID: id, Ingredients: representatives(r.Ingredients), Result: r.Result, Count: int32(r.Count)}
	}
	return g
}
