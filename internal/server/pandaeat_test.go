package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestPandaSitsAndEats: an adult panda fetches bamboo lying nearby, sits
// with it, chews, and finishes it; a cub leaves it alone.
func TestPandaSitsAndEats(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -2; x <= 6; x++ {
		for z := -2; z <= 2; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone) // a floor, so the bamboo lands beside the panda
		}
	}
	p := h.spawnMob(players, entityPanda, 0.5, 180, 0.5)
	p.baby = false
	it := h.spawnItem(players, itemByName["bamboo"], 1, 3.5, 180, 0.5)
	it.noPickupUntil = 0
	now := h.tick.Load()
	trait := pandaTrait(p.variant)
	picked := false
	for i := 0; i < 200 && !picked; i++ {
		if !h.pandaSitEat(players, p, trait, now) {
			t.Fatalf("the panda should be after the bamboo (step %d)", i)
		}
		p.x += p.vx
		p.z += p.vz
		picked = p.held != 0
	}
	if !picked || p.pandaFlags&pandaFlagSit == 0 {
		t.Fatalf("picked up and sat: held %d flags %d", p.held, p.pandaFlags)
	}
	if _, ok := h.items[it.eid]; ok {
		t.Fatal("the bamboo is in its mouth, not on the ground")
	}
	chewed := false
	for i := 0; i < 20000 && p.held != 0; i++ {
		h.pandaSitEat(players, p, trait, now)
		if p.pandaEat > 0 {
			chewed = true
		}
		if len(h.items) > 0 { // got bored and dropped it: pick it back up for the test
			for _, d := range h.items {
				delete(h.items, d.eid)
			}
			p.held, p.pandaSitCD = itemByName["bamboo"], 0
			h.setPandaFlag(players, p, pandaFlagSit, true)
		}
	}
	if p.held != 0 || !chewed || p.pandaFlags&pandaFlagSit != 0 || p.pandaEat != 0 {
		t.Fatalf("eaten and up: held %d chewed %v flags %d eat %d", p.held, chewed, p.pandaFlags, p.pandaEat)
	}
	// A cub does not fetch.
	p.baby = true
	h.spawnItem(players, itemByName["cake"], 1, 3.5, 180, 0.5).noPickupUntil = 0
	if h.pandaSitEat(players, p, trait, now) {
		t.Fatal("cubs leave the food")
	}
}
