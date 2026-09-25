package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// HopperBlockEntity.suckInItems takes the items above it in the order
// getEntitiesOfClass meets them — the order they came into the world —
// not at random: with room for one, the older drop goes in every time.
func TestHopperTakesItemsInEntityOrder(t *testing.T) {
	for trial := 0; trial < 30; trial++ {
		h := newHub(world.New(1))
		players := map[int32]*tracked{}
		h.world.ForceLoad(0, 0, 1)
		h.world.SetBlock(0, 180, 0, worldgen.BlockBase("hopper"))
		h.world.SetBlock(0, 181, 0, worldgen.Air)
		pos := simPos{blockPos: blockPos{0, 180, 0}}
		c := &bin{slots: make([]invStack, 5)}
		for i := 0; i < 4; i++ {
			c.slots[i] = invStack{item: itemByName["cobblestone"], count: 64}
		}
		first := h.spawnItemAt(players, 0, itemByName["dirt"], 1, 0.5, 181, 0.5, 0, 0, 0)
		for i := 0; i < 6; i++ {
			h.spawnItemAt(players, 0, itemByName["sand"], 64, 0.5, 181, 0.5, 0, 0, 0)
		}
		h.hopperTakeItems(players, pos, c, true)
		if c.slots[4].item != first.item {
			t.Fatalf("trial %d: the hopper took %d, not the oldest drop", trial, c.slots[4].item)
		}
	}
}
