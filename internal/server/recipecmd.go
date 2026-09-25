package server

import (
	"fmt"
	"strings"
)

// /recipe give|take <targets> *|<recipe> (RecipeCommand): unlock recipes in a
// player's book, or forget them. Given recipes arrive the way an unlock does
// (toast + highlight badge). Taken ones leave the client's book by a full
// re-send of what is still known — the book frame's replace form — and stay
// taken: the ingredient poll will not hand them back, as vanilla's recipe
// advancement stays done. Recipe names are vanilla's recipe names; a cooking
// recipe covers one book entry per input item (its tag's members), and the
// book's own cook/<station>/<input> names are taken too.

type evRecipeCmd struct {
	by     int32
	give   bool
	target string
	ids    []int32
}

func (evRecipeCmd) isHubEvent() {}

func (s *Server) cmdRecipe(p *player, args []string) {
	if !s.isOp(p.name) { // RecipeCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	if len(args) != 3 || (args[0] != "give" && args[0] != "take") {
		p.tell("Usage: /recipe give|take <targets> *|<recipe>")
		return
	}
	e := evRecipeCmd{by: p.eid, give: args[0] == "give", target: args[1]}
	if args[2] == "*" {
		e.ids = make([]int32, len(recipeNames))
		for i := range recipeNames {
			e.ids[i] = int32(i)
		}
	} else {
		ids := recipeIDsByName(strings.TrimPrefix(args[2], "minecraft:"))
		if len(ids) == 0 {
			p.tell("Unknown recipe: " + nsID(args[2]))
			return
		}
		e.ids = ids
	}
	s.hub.post(e)
}

// recipeIDsByName resolves a recipe name to its book entries: a crafting
// recipe or a saved cook/ key is one entry, a vanilla cooking recipe one per
// input item.
func recipeIDsByName(name string) []int32 {
	if id, ok := recipeIDByName[name]; ok {
		return []int32{id}
	}
	var ids []int32
	for _, k := range cookRecipeKeys[name] {
		if id, ok := recipeIDByName[k]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// applyRecipeCommand runs /recipe on the hub.
func (h *hub) applyRecipeCommand(players map[int32]*tracked, e evRecipeCmd) {
	tell := cmdTeller(players, e.by)
	okTell := h.cmdOK(players, e.by) // sendSuccess(…, true)
	targets := h.commandTargets(players, e.by, e.target)
	if len(targets) == 0 {
		tell("No player was found")
		return
	}
	var tally cmdTally
	for _, t := range targets {
		if t.rbKnown == nil {
			t.rbKnown, t.rbHighlight = map[int32]bool{}, map[int32]bool{}
		}
		n := 0
		if e.give {
			n = h.awardRecipes(t, e.ids)
		} else {
			n = h.resetRecipes(t, e.ids)
		}
		tally.track(t.p.name, n)
	}
	switch who := tally.single(true); {
	case tally.nonZero == 0 && e.give:
		tell("No new recipes were learned")
	case tally.nonZero == 0:
		tell("No recipes could be forgotten")
	case who != "" && e.give:
		okTell(fmt.Sprintf("Unlocked %d recipe(s) for %s", tally.total, who))
	case who != "":
		okTell(fmt.Sprintf("Took %d recipe(s) from %s", tally.total, who))
	case e.give:
		okTell(fmt.Sprintf("Unlocked %d recipe(s) for %d players", tally.total, tally.nonZero))
	default:
		okTell(fmt.Sprintf("Took %d recipe(s) from %d players", tally.total, tally.nonZero))
	}
}

// awardRecipes is ServerPlayer.awardRecipes: the ones not yet known join the
// book with the toast and the badge. Returns how many were new.
func (h *hub) awardRecipes(t *tracked, ids []int32) int {
	var fresh []int32
	hl := map[int32]bool{}
	for _, id := range ids {
		delete(t.rbTaken, id)
		if t.rbKnown[id] {
			continue
		}
		t.rbKnown[id], t.rbHighlight[id], hl[id] = true, true, true
		fresh = append(fresh, id)
	}
	if len(fresh) > 0 {
		t.p.trySendEv(rbBuildEntries(fresh, false, true, hl))
	}
	return len(fresh)
}

// resetRecipes is ServerPlayer.resetRecipes: the known ones leave the book.
// Returns how many were known.
func (h *hub) resetRecipes(t *tracked, ids []int32) int {
	n := 0
	for _, id := range ids {
		if !t.rbKnown[id] {
			continue
		}
		if t.rbTaken == nil {
			t.rbTaken = map[int32]bool{}
		}
		t.rbTaken[id] = true
		delete(t.rbKnown, id)
		delete(t.rbHighlight, id)
		n++
	}
	if n > 0 {
		known := make([]int32, 0, len(t.rbKnown))
		for id := range t.rbKnown {
			known = append(known, id)
		}
		t.p.trySendEv(rbBuildEntries(known, true, false, t.rbHighlight))
	}
	return n
}
