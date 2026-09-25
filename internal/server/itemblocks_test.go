package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// An item in the top cell of an updraft gets onAboveBubbleColumn's harder
// push, past the 0.7 cap of the cells below.
func TestItemThrownFromBubbleColumnTop(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	shaft(w, 0, 0, 180, 190, worldgen.SoulSand, worldgen.Air)
	for y := 180; y <= 185; y++ {
		w.SetBlock(0, y, 0, worldgen.BubbleColumnUp)
	}
	it := h.spawnItemAt(players, 0, itemByName["cobblestone"], 1, 0.5, 185.1, 0.5, 0, 0.7, 0)
	h.tickItems(players)
	if it.vy <= 0.7 {
		t.Errorf("the top of the column should push past 0.7: vy=%.3f", it.vy)
	}
}

// An item in a cobweb creeps down at 0.05 of its fall.
func TestItemStuckInCobweb(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	shaft(w, 0, 0, 180, 184, worldgen.Stone, worldgen.Air)
	w.SetBlock(0, 181, 0, cobwebState)
	it := h.spawnItemAt(players, 0, itemByName["cobblestone"], 1, 0.5, 181.3, 0.5, 0, 0, 0)
	for i := 0; i < 20; i++ {
		h.tickItems(players)
	}
	if it.y < 181.2 {
		t.Errorf("an item in a web should barely sink: y=%.3f after a second", it.y)
	}
}

// An item dropped on slime bounces (0.8 of its speed).
func TestItemBouncesOnSlime(t *testing.T) {
	h, players := mobBlocksHub()
	w := h.world
	shaft(w, 0, 0, 180, 190, slimeMin, worldgen.Air)
	it := h.spawnItemAt(players, 0, itemByName["cobblestone"], 1, 0.5, 184, 0.5, 0, 0, 0)
	fell, bounced := false, false
	for i := 0; i < 60; i++ {
		h.tickItems(players)
		if it.vy < -0.1 {
			fell = true
		}
		if fell && it.vy > 0.1 {
			bounced = true
		}
	}
	if !bounced {
		t.Error("an item dropped on slime should bounce")
	}
}
