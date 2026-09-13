package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestItemsFloatAndRideColumns: an item on the bed of a pool rises to the
// surface and floats; one in an upward bubble column shoots clear of the
// water and falls back; one on dry ground never moves.
func TestItemsFloatAndRideColumns(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -2; x <= 6; x++ {
		for z := -2; z <= 2; z++ {
			w.SetBlock(x, 175, z, worldgen.Stone)
		}
	}
	for y := 176; y <= 179; y++ {
		w.SetBlock(0, y, 0, worldgen.Water)
		w.SetBlock(2, y, 0, worldgen.BubbleColumnUp)
	}
	ground := h.spawnItem(players, itemByName["stone"], 1, 5.5, 176, 0.5)
	pool := &itemEntity{eid: h.allocEID(), dim: 0, x: 0.5, y: 176, z: 0.5, item: itemByName["stick"], count: 1}
	h.items[pool.eid] = pool
	col := &itemEntity{eid: h.allocEID(), dim: 0, x: 2.5, y: 176, z: 0.5, item: itemByName["stick"], count: 1}
	h.items[col.eid] = col
	gy := ground.y
	peak := col.y
	for i := 0; i < 400; i++ {
		h.floatItems(players)
		if col.y > peak {
			peak = col.y
		}
	}
	if ground.y != gy {
		t.Fatalf("an item on dry ground stays put: %.2f", ground.y)
	}
	if pool.y < 179.9 || pool.y > 180.2 || pool.vy != 0 {
		t.Fatalf("the pool item floats at the surface: y %.2f vy %.3f", pool.y, pool.vy)
	}
	if peak < 181 {
		t.Fatalf("the column item should be thrown clear of the water: peak %.2f", peak)
	}
	if col.y < 176 || col.y > 183 {
		t.Fatalf("the column item cycles inside the column: y %.2f", col.y)
	}
}
