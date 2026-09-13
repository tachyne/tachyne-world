package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A dolphin goes to a floating item, takes it, and tosses it back into the
// water ahead of itself with a pickup hold; an item on land is not a toy.
func TestDolphinPlaysWithItems(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	w := h.world
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 182; y++ {
				w.SetBlock(x, y, z, worldgen.Water)
			}
		}
	}
	d := h.spawnMob(players, entityDolphin, 0.5, 181, 0.5)
	if d == nil {
		t.Fatal("dolphin spawn returned nil")
	}
	it := h.spawnItem(players, int32(itemByName["stick"]), 1, 4.5, 181, 0.5)
	if it == nil {
		t.Fatal("item spawn returned nil")
	}
	it.noPickupUntil = 0
	h.tick.Store(100)

	if !h.dolphinPlay(players, d) {
		t.Fatal("a floating stick within eight blocks did not interest the dolphin")
	}
	if d.vx <= 0 {
		t.Errorf("the dolphin did not head for the stick: vx %.3f", d.vx)
	}
	// Reaching it: the stick goes into its mouth.
	d.x = 3.5
	if !h.dolphinPlay(players, d) || d.held != int32(itemByName["stick"]) {
		t.Fatalf("the dolphin did not pick the stick up: held %d", d.held)
	}
	if _, still := h.items[it.eid]; still {
		t.Error("the stick is still floating")
	}
	// Next update: tossed ahead, with a hold before anyone can take it.
	d.yaw = 0 // facing +z
	if !h.dolphinPlay(players, d) || d.held != 0 {
		t.Fatal("the dolphin kept the stick")
	}
	var tossed *itemEntity
	for _, cand := range h.items {
		tossed = cand
	}
	if tossed == nil {
		t.Fatal("no stick came back out")
	}
	if math.Abs(tossed.z-(d.z+dolphinTossReach)) > 0.01 || tossed.noPickupUntil != 100+dolphinTossHold {
		t.Errorf("tossed to z=%.2f (dolphin at %.2f) hold until %d", tossed.z, d.z, tossed.noPickupUntil)
	}
	// While the hold lasts it is nothing to play with; afterwards it is.
	if h.dolphinPlay(players, d) {
		t.Error("the dolphin went after a stick still under its pickup hold")
	}
	h.tick.Store(100 + dolphinTossHold)
	if !h.dolphinPlay(players, d) {
		t.Error("the dolphin lost interest once the hold passed")
	}
	// A stick on dry stone is out of the game.
	w.SetBlock(9, 180, 9, worldgen.Stone)
	w.SetBlock(9, 181, 9, worldgen.Air)
	w.SetBlock(9, 182, 9, worldgen.Air)
	delete(h.items, tossed.eid)
	dry := h.spawnItem(players, int32(itemByName["stick"]), 1, 9.5, 181, 9.5)
	dry.noPickupUntil = 0
	if h.dolphinPlay(players, d) {
		t.Error("a stick on land drew the dolphin")
	}
}
