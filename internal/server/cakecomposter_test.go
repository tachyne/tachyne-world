package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Seven bites eat a cake, a full player can't take one, and the comparator
// counts down as it goes.
func TestCakeIsEatenSliceBySlice(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pos := blockPos{0, 180, 0}
	h.worldFor(0).SetBlock(pos.x, pos.y, pos.z, cakeBase)

	if got := h.analogSignal(simPos{blockPos: pos}); got != 14 {
		t.Fatalf("a whole cake should read 14, got %d", got)
	}
	pl.food = maxFood
	h.eatCake(players, pl, pos)
	if h.worldFor(0).At(pos.x, pos.y, pos.z) != cakeBase {
		t.Fatal("a full player took a slice")
	}

	pl.food = 10
	for i := 0; i < cakeMaxBites; i++ {
		h.eatCake(players, pl, pos)
		bites, ok := cakeBites(h.worldFor(0).At(pos.x, pos.y, pos.z))
		if !ok || bites != i+1 {
			t.Fatalf("after bite %d the cake is %v/%d", i+1, ok, bites)
		}
		if got, want := h.analogSignal(simPos{blockPos: pos}), (7-(i+1))*2; got != want {
			t.Fatalf("comparator reads %d after bite %d, want %d", got, i+1, want)
		}
		pl.food = 10 // hungry again for the next slice
	}
	h.eatCake(players, pl, pos)
	if h.worldFor(0).At(pos.x, pos.y, pos.z) != worldgen.Air {
		t.Fatal("the seventh bite should finish the cake")
	}
	if pl.food <= 10 {
		t.Fatalf("eating cake should feed: food=%d", pl.food)
	}
}

// A candle in an untouched cake makes a candle cake; a bitten one refuses.
func TestCandleGoesIntoAnUntouchedCake(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pos := blockPos{0, 180, 0}
	candle := int32(itemByName["blue_candle"])
	h.worldFor(0).SetBlock(pos.x, pos.y, pos.z, cakeBase)
	pl.inv.slots[0] = invStack{item: candle, count: 2}
	pl.p.setHotbarSlot(0, candle)
	pl.p.held = 0

	h.eatCake(players, pl, pos)
	if got, want := h.worldFor(0).At(pos.x, pos.y, pos.z), worldgen.BlockBase("blue_candle_cake")+1; got != want {
		t.Fatalf("candle cake state %d, want %d", got, want)
	}
	if pl.inv.slots[0].count != 1 {
		t.Fatalf("the candle should be consumed, %d left", pl.inv.slots[0].count)
	}

	// A cake someone has already bitten takes no candle.
	h.worldFor(0).SetBlock(pos.x, pos.y, pos.z, cakeBase+1)
	pl.food = 10
	h.eatCake(players, pl, pos)
	if b, _ := cakeBites(h.worldFor(0).At(pos.x, pos.y, pos.z)); b != 2 {
		t.Fatal("a bitten cake should take a bite, not a candle")
	}
}

// A composter fills, composts on its own a second later, and pays out bone meal.
func TestComposterFillsAndPaysOut(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pos := blockPos{0, 180, 0}
	h.worldFor(0).SetBlock(pos.x, pos.y, pos.z, composterBase)
	wheat := int32(itemByName["cake"]) // chance 1.0, so the fill is deterministic
	pl.p.setHotbarSlot(0, wheat)
	pl.p.held = 0

	for want := 1; want <= composterFull; want++ {
		pl.inv.slots[0] = invStack{item: wheat, count: 1}
		h.useComposter(players, pl, pos)
		lvl, _ := composterLevel(h.worldFor(0).At(pos.x, pos.y, pos.z))
		if lvl != want {
			t.Fatalf("after item %d the level is %d", want, lvl)
		}
		if got := h.analogSignal(simPos{blockPos: pos}); got != want {
			t.Fatalf("comparator reads %d at level %d", got, want)
		}
	}
	// Full but not yet ready: another item does nothing.
	pl.inv.slots[0] = invStack{item: wheat, count: 1}
	h.useComposter(players, pl, pos)
	if lvl, _ := composterLevel(h.worldFor(0).At(pos.x, pos.y, pos.z)); lvl != composterFull {
		t.Fatalf("a full composter accepted more, level %d", lvl)
	}

	h.tickComposter(players, 0, pos, h.worldFor(0).At(pos.x, pos.y, pos.z))
	if lvl, _ := composterLevel(h.worldFor(0).At(pos.x, pos.y, pos.z)); lvl != composterReady {
		t.Fatalf("the composter never became ready, level %d", lvl)
	}

	before := len(h.items)
	h.useComposter(players, pl, pos)
	if h.worldFor(0).At(pos.x, pos.y, pos.z) != composterBase {
		t.Fatal("emptying should return the composter to level 0")
	}
	if len(h.items) != before+1 {
		t.Fatal("emptying a ready composter should drop bone meal")
	}
}

// The comparator readings vanilla gives blocks that are not containers.
func TestComparatorReadsNonContainers(t *testing.T) {
	h := newHub(world.New(1))
	w := h.worldFor(0)
	cases := []struct {
		name  string
		state uint32
		want  int
	}{
		{"empty cauldron", cauldronState, 0},
		{"full water cauldron", waterCauldronBase + 2, 3},
		{"lava cauldron", lavaCauldronState, 3},
		{"hive full of honey", beehiveMin + uint32(beeMaxHoney), beeMaxHoney},
		{"respawn anchor, one charge", worldgen.BlockBase("respawn_anchor") + 1, 3},
		{"respawn anchor, full", worldgen.BlockBase("respawn_anchor") + 4, 15},
		{"end portal frame with its eye", worldgen.BlockBase("end_portal_frame"), 15},
		{"end portal frame, empty", worldgen.BlockBase("end_portal_frame") + 4, 0},
		{"detector rail, occupied", worldgen.BlockBase("detector_rail"), 15},
		{"detector rail, clear", worldgen.BlockBase("detector_rail") + 12, 0},
	}
	for _, c := range cases {
		pos := blockPos{0, 180, 0}
		w.SetBlock(pos.x, pos.y, pos.z, c.state)
		if got := h.analogSignal(simPos{blockPos: pos}); got != c.want {
			t.Errorf("%s reads %d, want %d", c.name, got, c.want)
		}
	}
}

// A jukebox reads out which song is playing, not merely that one is.
func TestJukeboxComparatorReadsTheSong(t *testing.T) {
	h := newHub(world.New(1))
	pos := blockPos{0, 180, 0}
	h.worldFor(0).SetBlock(pos.x, pos.y, pos.z, jukeboxBase)
	if got := h.analogSignal(simPos{blockPos: pos}); got != 0 {
		t.Fatalf("an empty jukebox reads %d", got)
	}
	h.jukeboxes[simPos{blockPos: pos}] = &jukebox{disc: invStack{item: int32(itemByName["music_disc_5"]), count: 1}}
	if got := h.analogSignal(simPos{blockPos: pos}); got != 15 {
		t.Fatalf("disc 5 should read 15, got %d", got)
	}
	h.jukeboxes[simPos{blockPos: pos}] = &jukebox{disc: invStack{item: int32(itemByName["music_disc_13"]), count: 1}}
	if got := h.analogSignal(simPos{blockPos: pos}); got != 1 {
		t.Fatalf("disc 13 should read 1, got %d", got)
	}
}
