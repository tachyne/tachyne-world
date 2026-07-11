package server

// End-to-end engine flow: criteria fire from gameplay hooks, progress frames
// reach the player's out channel, completions announce + pay XP, and the
// store round-trips grants through JSON.

import (
	"os"
	"path/filepath"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"

	"tachyne/internal/world"
)

// drainAdvFrames pulls everything queued on the player's out channel and
// returns the advancement progress + tree-reveal frames (others ignored).
func drainAdvFrames(pl *tracked) (frames []attachproto.AdvProgress, trees []attachproto.AdvTree) {
	for {
		select {
		case pkt := <-pl.p.out:
			switch v := pkt.ev.(type) {
			case attachproto.AdvProgress:
				frames = append(frames, v)
			case attachproto.AdvTree:
				trees = append(trees, v)
			}
		default:
			return
		}
	}
}

func TestAdvanceEatAndInventory(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.adv = advState{}
	players[1] = pl

	// consume_item: eating an apple is husbandry/balanced_diet progress
	pl.food = 10
	pl.inv.slots[0] = invStack{item: itemByName["apple"], count: 1}
	h.eat(players, pl, 0)
	if _, ok := pl.adv["minecraft:husbandry/balanced_diet"]["apple"]; !ok {
		t.Fatalf("eating an apple should grant balanced_diet/apple: %+v", pl.adv)
	}

	// inventory_changed via the 1 Hz poll: cobblestone in hand = Stone Age,
	// and its parent chain criteria stay independent
	pl.inv.slots[1] = invStack{item: itemByName["cobblestone"], count: 3}
	h.advTick(players)
	if !pl.adv.done(advByID["minecraft:story/mine_stone"]) {
		t.Fatalf("cobblestone should complete Stone Age: %+v", pl.adv)
	}
	// completion emitted progress frames AND revealed the frontier around the
	// newly-done nodes (vanilla visibility: nothing was visible before)
	frames, trees := drainAdvFrames(pl)
	if len(frames) == 0 {
		t.Fatal("no progress frames queued")
	}
	if len(trees) == 0 {
		t.Fatal("no tree-reveal frames queued")
	}
	revealed := map[string]bool{}
	for _, tr := range trees {
		for _, n := range tr.Nodes {
			revealed[n.ID] = true
		}
	}
	if !revealed["minecraft:story/mine_stone"] || !revealed["minecraft:story/root"] {
		t.Fatalf("story frontier not revealed: %v", revealed)
	}
	if revealed["minecraft:adventure/voluntary_exile"] {
		t.Fatal("hidden node leaked in reveal")
	}

	// idempotent: another tick grants nothing new
	h.advTick(players)
	if f2, t2 := drainAdvFrames(pl); len(f2) != 0 || len(t2) != 0 {
		t.Fatalf("re-tick re-granted: %d frames %d trees", len(f2), len(t2))
	}
}

func TestAdvanceKillAndDimension(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.adv = advState{}
	players[1] = pl

	h.advance(players, pl, "player_killed_entity", advMatch{entity: "zombie"})
	// adventure/root requires killed_something OR killed_by_something
	if !pl.adv.done(advByID["minecraft:adventure/root"]) {
		t.Fatalf("a zombie kill should complete adventure/root: %+v", pl.adv)
	}
	// kill_a_mob (monster hunter) got its zombie criterion but isn't done
	if _, ok := pl.adv["minecraft:adventure/kill_a_mob"]["minecraft:zombie"]; !ok {
		t.Fatal("kill_a_mob zombie criterion missing")
	}

	h.advance(players, pl, "changed_dimension", advMatch{dim: 1})
	if !pl.adv.done(advByID["minecraft:story/enter_the_nether"]) {
		t.Fatal("dim 1 should complete We Need to Go Deeper")
	}
	if pl.adv.done(advByID["minecraft:story/enter_the_end"]) {
		t.Fatal("dim 1 must not complete The End?")
	}
}

func TestAdvStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "advancements.json")
	st := newAdvStore(path)
	state := advState{"minecraft:story/mine_stone": {"get_stone": 12345}}
	st.save("wesley", state)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no file written: %v", err)
	}

	st2 := newAdvStore(path)
	got := st2.load("wesley")
	if got["minecraft:story/mine_stone"]["get_stone"] != 12345 {
		t.Fatalf("round trip lost the grant: %+v", got)
	}
	// load returns a copy — mutating it must not touch the store
	got["minecraft:story/mine_stone"]["other"] = 1
	if _, ok := st2.load("wesley")["minecraft:story/mine_stone"]["other"]; ok {
		t.Fatal("load returned a shared map")
	}
}
