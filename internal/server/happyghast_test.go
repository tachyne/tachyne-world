package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestDriedGhastHatchesGhastling: a dried_ghast beside water hydrates over
// random ticks and, once full, is consumed and hatches a baby happy ghast.
func TestDriedGhastHatchesGhastling(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	x, y, z := 100, 80, 100
	h.world.SetBlock(x, y, z, driedGhastBase)       // hydration 0
	h.world.SetBlock(x+1, y, z, worldgen.WaterBase) // wet: water source beside it

	hatched := false
	for i := 0; i < 400; i++ { // probabilistic step gate (~1-in-4), so loop generously
		st := h.world.At(x, y, z)
		if !isDriedGhast(st) {
			hatched = true
			break
		}
		h.tickDriedGhast(players, x, y, z, st)
	}
	if !hatched {
		t.Fatal("dried ghast never hatched despite adjacent water")
	}
	if got := h.world.At(x, y, z); got != worldgen.Air {
		t.Fatalf("dried ghast should be consumed on hatch, got state %d", got)
	}
	var g *mob
	for _, m := range h.mobs {
		if m.etype == entityHappyGhast {
			g = m
		}
	}
	if g == nil {
		t.Fatal("no happy ghast hatched")
	}
	if !g.baby || g.growLeft <= 0 {
		t.Errorf("hatched mob must be a growing ghastling (baby=%v growLeft=%d)", g.baby, g.growLeft)
	}
}

// TestDriedGhastDriesWithoutWater: with no water and no rain, a partly-hydrated
// dried ghast loses hydration instead of hatching.
func TestDriedGhastDriesWithoutWater(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	x, y, z := 40, 80, 40
	info, _ := worldgen.InfoForState(driedGhastBase)
	wet := worldgen.SetProperty(info, driedGhastBase, "hydration", "2")
	h.world.SetBlock(x, y, z, wet) // hydration 2, dry surroundings

	for i := 0; i < 100 && worldgen.GetProperty(info, h.world.At(x, y, z), "hydration") == "2"; i++ {
		h.tickDriedGhast(players, x, y, z, h.world.At(x, y, z))
	}
	got := h.world.At(x, y, z)
	if !isDriedGhast(got) {
		t.Fatal("a dry dried ghast must not hatch")
	}
	if v := worldgen.GetProperty(info, got, "hydration"); v != "1" {
		t.Errorf("hydration should drop 2->1 when dry, got %q", v)
	}
	if len(h.mobs) != 0 {
		t.Error("no ghastling should hatch while drying")
	}
}
