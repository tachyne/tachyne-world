package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Lit redstone ore goes dark on a later random tick, and nylium covered by an
// opaque block reverts to netherrack.

func TestLitRedstoneOreGoesDark(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	for _, c := range []struct{ lit, dark uint32 }{
		{redstoneOreLit, redstoneOreDark},
		{deepslateRedstoneOreLit, deepslateRedstoneDark},
	} {
		x, y, z := 10+int(c.lit%16), 40, 10
		h.world.SetBlock(x, y, z, c.lit)
		if !h.tickRedstoneOre(players, 0, x, y, z, c.lit) {
			t.Fatalf("state %d was not recognised as redstone ore", c.lit)
		}
		if got := h.world.At(x, y, z); got != c.dark {
			t.Errorf("lit ore %d became %d, want dark %d", c.lit, got, c.dark)
		}
	}
}

// Dark ore is claimed (so it doesn't fall through to another handler) but must
// not change.
func TestDarkRedstoneOreStaysDark(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 30, 40, 30

	h.world.SetBlock(x, y, z, redstoneOreDark)
	if !h.tickRedstoneOre(players, 0, x, y, z, redstoneOreDark) {
		t.Fatal("dark ore should still be claimed by the handler")
	}
	if got := h.world.At(x, y, z); got != redstoneOreDark {
		t.Errorf("dark ore changed to %d", got)
	}
}

func TestCoveredNyliumRevertsToNetherrack(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	for i, name := range []string{"crimson_nylium", "warped_nylium"} {
		st := worldgen.BlockID(name)
		x, y, z := 50+i*4, 40, 50
		h.world.SetBlock(x, y, z, st)
		h.world.SetBlock(x, y+1, z, worldgen.Stone) // covered

		h.tickNylium(players, 0, x, y, z, st)
		if got := h.world.At(x, y, z); got != netherrackBlock {
			t.Errorf("%s under stone is state %d, want netherrack %d", name, got, netherrackBlock)
		}
	}
}

func TestUncoveredNyliumSurvives(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	st := worldgen.BlockID("crimson_nylium")
	x, y, z := 70, 40, 70
	h.world.SetBlock(x, y, z, st)
	h.world.SetBlock(x, y+1, z, worldgen.Air)

	for i := 0; i < 200; i++ {
		h.tickNylium(players, 0, x, y, z, h.world.At(x, y, z))
	}
	if got := h.world.At(x, y, z); got != st {
		t.Errorf("open nylium reverted: state %d", got)
	}
}
