package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Sapling growth, ported from SaplingBlock + TreeGrower.
//
// The bug these pin: saplingRanges matched only oak/spruce/birch, and growTree
// hard-coded oak logs and leaves — so five species never grew at all and the
// three that did all produced an OAK tree.

// growUntilGone random-ticks a sapling until it is no longer a sapling,
// returning whether it grew. The 1-in-7 roll plus the light gate means this
// needs plenty of attempts.
func growUntilGone(h *hub, players map[int32]*tracked, x, y, z int, lo, hi uint32) bool {
	for i := 0; i < 8000; i++ {
		s := h.world.At(x, y, z)
		if s < lo || s > hi {
			return true
		}
		h.tickSapling(players, 0, x, y, z, s)
	}
	return false
}

// Each species must grow ITS OWN tree, not an oak.
func TestSaplingsGrowTheirOwnSpecies(t *testing.T) {
	cases := []struct {
		sapling, log, leaves string
	}{
		{"oak_sapling", "oak_log", "oak_leaves"},
		{"spruce_sapling", "spruce_log", "spruce_leaves"},
		{"birch_sapling", "birch_log", "birch_leaves"},
		{"jungle_sapling", "jungle_log", "jungle_leaves"},
		{"acacia_sapling", "acacia_log", "acacia_leaves"},
		{"cherry_sapling", "cherry_log", "cherry_leaves"},
	}
	for i, c := range cases {
		h := newHub(world.New(1))
		players := map[int32]*tracked{}
		x, y, z := 100+i*16, 200, 100

		lo, hi := worldgen.BlockRange(c.sapling)
		h.world.SetBlock(x, y-1, z, worldgen.Dirt)
		h.world.SetBlock(x, y, z, lo)

		if !growUntilGone(h, players, x, y, z, lo, hi) {
			t.Errorf("%s never grew", c.sapling)
			continue
		}

		// The trunk base is the species' log, not oak.
		wantLog, _ := worldgen.BlockRange(c.log)
		got := h.world.At(x, y, z)
		if got < wantLog || got > wantLog+2 { // log axis states
			oak, _ := worldgen.BlockRange("oak_log")
			hint := ""
			if got >= oak && got <= oak+2 {
				hint = " (grew an OAK)"
			}
			t.Errorf("%s: trunk state %d, want %s (%d..%d)%s",
				c.sapling, got, c.log, wantLog, wantLog+2, hint)
		}

		// And its canopy is the species' leaves.
		leafLo, leafHi := worldgen.BlockRange(c.leaves)
		found := false
		for dy := 1; dy <= 12 && !found; dy++ {
			for dx := -3; dx <= 3 && !found; dx++ {
				for dz := -3; dz <= 3 && !found; dz++ {
					s := h.world.At(x+dx, y+dy, z+dz)
					if s >= leafLo && s <= leafHi {
						found = true
					}
				}
			}
		}
		if !found {
			t.Errorf("%s: no %s anywhere above the trunk", c.sapling, c.leaves)
		}
	}
}

// TreeGrower gives dark oak and pale oak a mega feature and NO single tree, so
// one sapling on its own must never grow.
func TestDarkOakNeedsFourSaplings(t *testing.T) {
	for _, name := range []string{"dark_oak_sapling", "pale_oak_sapling"} {
		h := newHub(world.New(1))
		players := map[int32]*tracked{}
		x, y, z := 40, 200, 40

		lo, hi := worldgen.BlockRange(name)
		h.world.SetBlock(x, y-1, z, worldgen.Dirt)
		h.world.SetBlock(x, y, z, lo+1) // already stage 1

		for i := 0; i < 4000; i++ {
			h.tickSapling(players, 0, x, y, z, h.world.At(x, y, z))
		}
		if got := h.world.At(x, y, z); got < lo || got > hi {
			t.Errorf("%s: a lone sapling grew (state %d); vanilla requires 2x2", name, got)
		}
	}
}

// Four in a square do grow, and consume all four.
func TestDarkOakSquareGrows(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 60, 200, 60

	lo, hi := worldgen.BlockRange("dark_oak_sapling")
	for _, c := range [4][2]int{{x, z}, {x + 1, z}, {x, z + 1}, {x + 1, z + 1}} {
		h.world.SetBlock(c[0], y-1, c[1], worldgen.Dirt)
		h.world.SetBlock(c[0], y, c[1], lo+1) // stage 1
	}

	if !growUntilGone(h, players, x, y, z, lo, hi) {
		t.Fatal("a 2x2 of dark oak saplings never grew")
	}
	// Every cell of the square is consumed, not just the ticked one.
	for _, c := range [4][2]int{{x, z}, {x + 1, z}, {x, z + 1}, {x + 1, z + 1}} {
		if s := h.world.At(c[0], y, c[1]); s >= lo && s <= hi {
			t.Errorf("sapling left behind at (%d,%d) after the tree grew", c[0], c[1])
		}
	}
	wantLog, _ := worldgen.BlockRange("dark_oak_log")
	if got := h.world.At(x, y, z); got < wantLog || got > wantLog+2 {
		t.Errorf("trunk state %d, want dark_oak_log (%d..%d)", got, wantLog, wantLog+2)
	}
}

// A sapling's canopy must not eat a player's build: leaves only replace air.
func TestGrowingTreeDoesNotOverwriteBlocks(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 80, 200, 80

	lo, hi := worldgen.BlockRange("oak_sapling")
	h.world.SetBlock(x, y-1, z, worldgen.Dirt)
	h.world.SetBlock(x, y, z, lo+1)

	// A stone marker inside the future canopy.
	mx, my, mz := x+2, y+5, z+2
	h.world.SetBlock(mx, my, mz, worldgen.Stone)

	growUntilGone(h, players, x, y, z, lo, hi)

	if got := h.world.At(mx, my, mz); got != worldgen.Stone {
		t.Errorf("tree overwrote a placed block: state %d, want stone", got)
	}
}
