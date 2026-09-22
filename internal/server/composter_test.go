package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A composter is a container as far as a hopper is concerned: one above feeds
// it, one below takes the bone meal and empties it. That pair is the whole
// composter farm.
func TestHoppersFeedAndEmptyAComposter(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	x, y, z := 30, 70, 30
	pos := simPos{dim: 0, blockPos: blockPos{x, y, z}}

	// Feeding: an empty composter always takes the first item.
	h.world.SetBlock(x, y, z, composterBase)
	if !h.insertByFace(pos, -1, invStack{item: int32(itemByName["wheat"]), count: 1}) {
		t.Fatal("a hopper should be able to feed a composter")
	}
	if lvl, _ := composterLevel(h.world.At(x, y, z)); lvl != 1 {
		t.Fatalf("the composter should have risen to 1, got %d", lvl)
	}
	// Something it will not compost is refused.
	if h.insertByFace(pos, -1, invStack{item: int32(itemByName["stone"]), count: 1}) {
		t.Fatal("a composter takes no stone")
	}

	// Taking: a ready composter hands its bone meal to the hopper below and
	// empties itself.
	h.world.SetBlock(x, y, z, composterBase+uint32(composterReady))
	c := &bin{slots: make([]invStack, 5)}
	h.bins[simPos{dim: 0, blockPos: blockPos{x, y - 1, z}}] = c
	if !h.hopperPull(players, simPos{dim: 0, blockPos: blockPos{x, y - 1, z}}, c) {
		t.Fatal("the hopper below should have taken the bone meal")
	}
	if c.slots[0].item != itemBoneMeal {
		t.Fatalf("the hopper should hold bone meal, got %+v", c.slots[0])
	}
	if lvl, _ := composterLevel(h.world.At(x, y, z)); lvl != 0 {
		t.Fatalf("the composter should be empty again, got level %d", lvl)
	}
}
