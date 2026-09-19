package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Right-clicking a ripe bush picks its berries back to age 1, a berried
// cave vine drops one glow berry and empties, a copper golem statue cycles
// its pose, and a lone dust toggles between cross and dot (which the wire
// update then preserves).
func TestBlockClicks(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	w := h.worldFor(0)
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			w.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
		}
	}
	click := func(x, y, z int) { h.clickBlock(players, evClickBlock{eid: pl.p.eid, x: x, y: y, z: z}) }
	have := func(id int32) int { // items of one kind on the ground
		n := 0
		for _, it := range h.items {
			if it.item == id {
				n += it.count
			}
		}
		return n
	}

	w.SetBlock(0, 180, 0, berryBase+1) // age 1: nothing to pick
	before := len(h.items)
	click(0, 180, 0)
	if len(h.items) != before || w.At(0, 180, 0) != berryBase+1 {
		t.Fatal("an unripe bush should not be picked")
	}
	w.SetBlock(0, 180, 0, berryBase+3)
	click(0, 180, 0)
	if got := have(itemSweetBerries); w.At(0, 180, 0) != berryBase+1 || got < 2 || got > 3 {
		t.Fatalf("a ripe bush should drop 2-3 sweet berries and return to age 1 (state %d, berries %d)", w.At(0, 180, 0), got)
	}

	info, _ := worldgen.InfoForState(caveVinesLo)
	vine := worldgen.SetProperty(info, caveVinesLo, "berries", "true")
	w.SetBlock(1, 180, 0, vine)
	click(1, 180, 0)
	if got := w.At(1, 180, 0); worldgen.GetProperty(info, got, "berries") != "false" || have(itemGlowBerries) != 1 {
		t.Fatalf("a berried cave vine should drop a glow berry and empty (state %d)", got)
	}
	click(1, 180, 0)
	if have(itemGlowBerries) != 1 {
		t.Fatal("an empty vine has nothing to pick")
	}

	statue := worldgen.BlockBase("copper_golem_statue")
	sinfo, _ := worldgen.InfoForState(statue)
	w.SetBlock(-1, 180, 0, statue)
	want := []string{"sitting", "running", "star", "standing"}
	for _, p := range want {
		click(-1, 180, 0)
		if got := worldgen.GetProperty(sinfo, w.At(-1, 180, 0), "copper_golem_pose"); got != p {
			t.Fatalf("statue pose %q, want %q", got, p)
		}
	}
	pl.inv.slots[0] = invStack{item: itemByName["iron_axe"], count: 1}
	pl.p.setHotbarSlot(0, int32(itemByName["iron_axe"]))
	pl.p.held = 0
	click(-1, 180, 0)
	if got := worldgen.GetProperty(sinfo, w.At(-1, 180, 0), "copper_golem_pose"); got != "standing" {
		t.Fatalf("an axe click must not cycle the pose (got %q)", got)
	}

	w.SetBlock(0, 180, 2, wireStateMin)
	h.setBlock(players, blockPos{0, 180, 2}, h.connectWire(0, 180, 2, wireStateMin))
	if wireIsDot(w.At(0, 180, 2)) {
		t.Fatal("a lone dust is placed as a cross")
	}
	click(0, 180, 2)
	if !wireIsDot(w.At(0, 180, 2)) {
		t.Fatal("clicking a lone cross should make a dot")
	}
	if st := h.connectWire(0, 180, 2, w.At(0, 180, 2)); !wireIsDot(st) {
		t.Fatal("the wire update must preserve a dot with nothing to connect to")
	}
	click(0, 180, 2)
	if wireIsDot(w.At(0, 180, 2)) {
		t.Fatal("clicking a dot should make a cross again")
	}
	w.SetBlock(1, 180, 2, wireStateMin) // a real neighbour: no toggling
	h.setBlock(players, blockPos{1, 180, 2}, h.connectWire(1, 180, 2, wireStateMin))
	h.setBlock(players, blockPos{0, 180, 2}, h.connectWire(0, 180, 2, w.At(0, 180, 2)))
	click(0, 180, 2)
	if wireIsDot(w.At(0, 180, 2)) {
		t.Fatal("a connected dust must not become a dot")
	}
}
