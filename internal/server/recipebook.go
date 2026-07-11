package server

import ()

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
	if t.inv == nil || e.windowID != t.winID || (t.winKind != winPlayer && t.winKind != winCraft) {
		return
	}
	w := gridSize(t)

	// Work out which grid cell needs which item (top-left aligned for shaped).
	type needCell struct {
		cell int
		item int32
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
				if item := r.Cells[row*int(r.W)+col]; item != 0 {
					need = append(need, needCell{row*w + col, item})
				}
			}
		}
	case int(e.recipeID) < len(shapedRecipes)+len(shapelessRecipes):
		r := &shapelessRecipes[int(e.recipeID)-len(shapedRecipes)]
		if len(r.Ingredients) > w*w {
			return
		}
		for i, item := range r.Ingredients {
			need = append(need, needCell{i, item})
		}
	default:
		return
	}

	// Everything must be available: count demand vs inventory supply.
	demand := map[int32]int{}
	for _, n := range need {
		demand[n.item]++
	}
	h.reclaimCraft(players, t) // return whatever's in the grid first
	for item, count := range demand {
		have := 0
		for _, s := range t.inv.slots {
			if s.item == item {
				have += s.count
			}
		}
		if have < count {
			return // missing ingredients — leave the ghost preview to the client
		}
	}

	// Pull one of each needed item out of the inventory into its grid cell.
	for _, n := range need {
		for i := range t.inv.slots {
			s := &t.inv.slots[i]
			if s.item == n.item && s.count > 0 {
				if s.count--; s.count == 0 {
					s.item = 0
				}
				h.sendSlot(t, i)
				break
			}
		}
		t.craft[n.cell] = invStack{item: n.item, count: t.craft[n.cell].count + 1}
	}
	for i := 0; i < w*w; i++ {
		h.sendWinSlot(t, int16(i+1), t.craft[i])
	}
	h.sendCraftResult(t)
}
