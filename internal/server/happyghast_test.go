package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// driedGhastCycle is one random tick (which schedules the step) and the
// step's own scheduled tick 5000 ticks later.
func driedGhastCycle(h *hub, players map[int32]*tracked, pos blockPos) {
	h.tickDriedGhast(players, 0, pos.x, pos.y, pos.z, h.world.At(pos.x, pos.y, pos.z))
	h.processUpdate(players, 0, pos) // a neighbour's update before the step is due does nothing
	h.tick.Add(driedGhastDelay)
	h.processUpdate(players, 0, pos)
}

// TestDriedGhastHatchesGhastling: a waterlogged dried_ghast takes a step of
// water every 5000 ticks and, once full, is consumed and hatches a baby
// happy ghast.
func TestDriedGhastHatchesGhastling(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	x, y, z := 100, 80, 100
	info, _ := worldgen.InfoForState(driedGhastBase)
	wet := worldgen.SetProperty(info, driedGhastBase, "waterlogged", "true")
	h.world.SetBlock(x, y, z, worldgen.SetProperty(info, wet, "hydration", "0"))
	pos := blockPos{x, y, z}
	for i := 0; i < 3; i++ {
		driedGhastCycle(h, players, pos)
		if got := driedGhastHydration(h.world.At(x, y, z)); got != i+1 {
			t.Fatalf("after %d steps hydration is %d", i+1, got)
		}
	}
	driedGhastCycle(h, players, pos)
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

// TestDriedGhastDriesWithoutWater: not waterlogged — water beside it does
// not count — a partly-hydrated dried ghast loses a step instead of hatching.
func TestDriedGhastDriesWithoutWater(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	x, y, z := 40, 80, 40
	info, _ := worldgen.InfoForState(driedGhastBase)
	dry := worldgen.SetProperty(info, driedGhastBase, "waterlogged", "false")
	h.world.SetBlock(x, y, z, worldgen.SetProperty(info, dry, "hydration", "2"))
	h.world.SetBlock(x+1, y, z, worldgen.WaterBase)
	driedGhastCycle(h, players, blockPos{x, y, z})
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
