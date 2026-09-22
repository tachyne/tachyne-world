package server

import (
	"path/filepath"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"github.com/tachyne/tachyne-world/internal/world"
)

func drainRB(pl *tracked) (out []attachproto.RecipeBook) {
	for {
		select {
		case pkt := <-pl.p.out:
			if rb, ok := pkt.ev.(attachproto.RecipeBook); ok {
				out = append(out, rb)
			}
		default:
			return
		}
	}
}

// TestRecipeUnlocks: obtaining an ingredient reveals its recipes with
// notify+highlight, idempotently; the initial send carries only known ones.
func TestRecipeUnlocks(t *testing.T) {
	pl := testTracked()
	pl.rbKnown, pl.rbHighlight = map[int32]bool{}, map[int32]bool{}
	h := newHub(world.New(1))

	oak := itemByName["oak_planks"]
	if len(rbIngredientIndex[oak]) == 0 {
		t.Fatal("oak planks should be an ingredient of something")
	}
	h.recipeUnlocks(pl, []int32{oak})
	n := len(pl.rbKnown)
	if n == 0 {
		t.Fatal("no recipes unlocked")
	}
	frames := drainRB(pl)
	if len(frames) != 1 || frames[0].Replace {
		t.Fatalf("want one increment frame, got %d (replace=%v)", len(frames), len(frames) > 0 && frames[0].Replace)
	}
	got := len(frames[0].Shaped) + len(frames[0].Shapeless)
	if got != n {
		t.Fatalf("frame carries %d entries, known %d", got, n)
	}
	for _, r := range frames[0].Shaped {
		if !r.Notify || !r.Highlight {
			t.Fatal("increment entries must notify+highlight")
		}
	}

	h.recipeUnlocks(pl, []int32{oak}) // idempotent
	if len(drainRB(pl)) != 0 {
		t.Fatal("re-unlock sent frames")
	}

	// initial send: known-only replace frame, highlights preserved, no notify
	h.recipeSendInitial(pl)
	frames = drainRB(pl)
	if len(frames) != 1 || !frames[0].Replace {
		t.Fatalf("initial: %d frames", len(frames))
	}
	if len(frames[0].Shaped)+len(frames[0].Shapeless) != n {
		t.Fatal("initial frame should carry exactly the known set")
	}
	for _, r := range frames[0].Shaped {
		if r.Notify || !r.Highlight {
			t.Fatal("initial entries: no notify, highlight preserved")
		}
	}
}

// TestRecipeBookStoreRoundTrip: known/highlight/settings persist by name.
func TestRecipeBookStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "recipebook.json")
	st := newRecipeBookStore(path)
	pl := testTracked()
	pl.rbKnown = map[int32]bool{3: true, 9: true}
	pl.rbHighlight = map[int32]bool{9: true}
	pl.rbSettings.Open[0] = true
	st.save("wesley", pl)

	pl2 := testTracked()
	newRecipeBookStore(path).loadInto(pl2, "wesley")
	if !pl2.rbKnown[3] || !pl2.rbKnown[9] || pl2.rbKnown[4] {
		t.Fatalf("known: %+v", pl2.rbKnown)
	}
	if !pl2.rbHighlight[9] || pl2.rbHighlight[3] {
		t.Fatalf("highlight: %+v", pl2.rbHighlight)
	}
	if !pl2.rbSettings.Open[0] || pl2.rbSettings.Filter[0] {
		t.Fatalf("settings: %+v", pl2.rbSettings)
	}
}

// TestCookingRecipesEnterTheBook: picking up a raw ore unlocks its furnace
// AND blast-furnace entries, and they render as cooking entries carrying the
// right station, cook time and book category — the three cooker tabs of the
// green book were empty before these existed.
func TestCookingRecipesEnterTheBook(t *testing.T) {
	pl := testTracked()
	pl.rbKnown, pl.rbHighlight = map[int32]bool{}, map[int32]bool{}
	h := newHub(world.New(1))

	rawIron := itemByName["raw_iron"]
	h.recipeUnlocks(pl, []int32{rawIron})

	var cook []attachproto.CookingRecipe
	for _, rb := range drainRB(pl) {
		cook = append(cook, rb.Cooking...)
	}
	if len(cook) != 2 {
		t.Fatalf("raw iron unlocked %d cooking entries, want 2 (furnace + blast)", len(cook))
	}
	stations := map[int32]attachproto.CookingRecipe{}
	for _, c := range cook {
		if c.Ingredient != rawIron {
			t.Fatalf("entry %d has ingredient %d, want raw iron", c.ID, c.Ingredient)
		}
		if c.Result != itemByName["iron_ingot"] {
			t.Fatalf("raw iron cooks to %d, want an iron ingot", c.Result)
		}
		stations[c.Station] = c
	}
	f, ok := stations[itemByName["furnace"]]
	if !ok {
		t.Fatal("no furnace entry")
	}
	if f.Cook != 200 {
		t.Fatalf("furnace cook time = %d, want 200", f.Cook)
	}
	// The vanilla recipe json files raw ore under "misc", not "blocks" — the
	// blocks tab is for the ore BLOCK recipes.
	if f.Category != 6 { // furnace_misc
		t.Fatalf("furnace category = %d, want 6 (furnace_misc)", f.Category)
	}
	b, ok := stations[itemByName["blast_furnace"]]
	if !ok {
		t.Fatal("no blast-furnace entry")
	}
	if b.Cook != 100 {
		t.Fatalf("blast cook time = %d, want 100", b.Cook)
	}
	if b.Category != 8 { // blast_furnace_misc
		t.Fatalf("blast category = %d, want 8 (blast_furnace_misc)", b.Category)
	}
	if f.ID == b.ID {
		t.Fatal("the two cooker entries share a display id")
	}
}

// TestCookingDisplayIDsAreDisjoint: cooking ids sit above every crafting id,
// so a craft_recipe_request for a crafting recipe can never resolve to a
// cooker entry (and the limited_crafting gate keeps matching on grids alone).
func TestCookingDisplayIDsAreDisjoint(t *testing.T) {
	if len(cookBookRecipes) == 0 {
		t.Fatal("no cooking book recipes built")
	}
	if cookBookRecipes[0].ID != cookBookFirstID {
		t.Fatalf("first cooking id = %d, want %d", cookBookRecipes[0].ID, cookBookFirstID)
	}
	if int(cookBookFirstID) != len(shapedRecipes)+len(shapelessRecipes) {
		t.Fatalf("cooking ids start at %d, overlapping the %d crafting recipes",
			cookBookFirstID, len(shapedRecipes)+len(shapelessRecipes))
	}
	for i := range cookBookRecipes {
		if want := cookBookFirstID + int32(i); cookBookRecipes[i].ID != want {
			t.Fatalf("cooking entry %d has id %d, want %d", i, cookBookRecipes[i].ID, want)
		}
	}
}

// TestFurnaceBookClickLoadsTheInput: clicking a furnace-book entry with the
// cooker open moves one ingredient out of the inventory and into the input
// slot (vanilla ServerPlaceRecipe for a cooker menu), and a blasting entry
// clicked at an ordinary furnace is refused.
func TestFurnaceBookClickLoadsTheInput(t *testing.T) {
	h, players, pl, f := furnaceSetup()
	pl.inv.slots[0] = invStack{item: tRawIron, count: 3}

	var smelt, blast int32 = -1, -1
	for _, r := range cookBookRecipes {
		if r.Ingredient != tRawIron {
			continue
		}
		if r.Station == itemByName["furnace"] {
			smelt = r.ID
		} else if r.Station == itemByName["blast_furnace"] {
			blast = r.ID
		}
	}
	if smelt < 0 || blast < 0 {
		t.Fatal("raw iron should have both a furnace and a blast-furnace entry")
	}

	h.placeRecipe(players, pl, evCraftRequest{eid: pl.p.eid, windowID: pl.winID, recipeID: blast})
	if f.slots[furnaceInput].item != 0 {
		t.Fatalf("a blasting entry loaded an ordinary furnace: %+v", f.slots[furnaceInput])
	}

	h.placeRecipe(players, pl, evCraftRequest{eid: pl.p.eid, windowID: pl.winID, recipeID: smelt})
	if f.slots[furnaceInput].item != tRawIron || f.slots[furnaceInput].count != 1 {
		t.Fatalf("input = %+v, want one raw iron", f.slots[furnaceInput])
	}
	if pl.inv.slots[0].count != 2 {
		t.Fatalf("inventory = %+v, want 2 left", pl.inv.slots[0])
	}

	// A second click stacks; a third empties nothing that isn't there.
	h.placeRecipe(players, pl, evCraftRequest{eid: pl.p.eid, windowID: pl.winID, recipeID: smelt})
	if f.slots[furnaceInput].count != 2 {
		t.Fatalf("second click should stack, input = %+v", f.slots[furnaceInput])
	}
}
