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
