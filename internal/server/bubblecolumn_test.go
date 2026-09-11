package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestBubbleColumnFormsAndCollapses: source water over soul sand becomes an
// updraft after the 20-tick tick and climbs the water above; over magma a
// whirlpool; flowing water never; and mining the source drops it all back
// to water.
func TestBubbleColumnFormsAndCollapses(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	w.SetBlock(0, 180, 0, worldgen.SoulSand)
	for y := 181; y <= 183; y++ {
		w.SetBlock(0, y, 0, worldgen.WaterBase)
	}
	w.SetBlock(0, 184, 0, worldgen.WaterBase+1) // flowing: the column stops under it
	src := blockPos{0, 180, 0}
	if !h.tickBubbleSource(players, 0, src, worldgen.SoulSand) || w.At(0, 181, 0) != worldgen.WaterBase {
		t.Fatal("the first update only schedules the 20-tick tick")
	}
	due := h.bubbleDue[simPos{0, src}]
	if due != h.tick.Load()+bubbleSourceDelay {
		t.Fatalf("due %d, now %d", due, h.tick.Load())
	}
	h.tick.Store(due)
	h.tickBubbleSource(players, 0, src, worldgen.SoulSand)
	for y := 181; y <= 183; y++ {
		if got := w.At(0, y, 0); got != worldgen.BubbleColumnUp {
			t.Fatalf("y=%d: %d, want an updraft column (%d)", y, got, worldgen.BubbleColumnUp)
		}
	}
	if w.At(0, 184, 0) != worldgen.WaterBase+1 {
		t.Fatal("flowing water is not part of a column")
	}
	// The column is water: the fluid sim leaves it be and spreads from it.
	h.updateFluid(players, 0, blockPos{0, 181, 0}, w.At(0, 181, 0))
	if w.At(0, 181, 0) != worldgen.BubbleColumnUp {
		t.Fatal("the fluid sim must not rewrite a column cell")
	}
	if got := w.At(1, 181, 0); got != worldgen.WaterBase+1 {
		t.Fatalf("a column spreads water sideways like a source: %d", got)
	}
	// Mine the soul sand: the whole column falls back to water.
	w.SetBlock(0, 180, 0, worldgen.Air)
	if h.updateBubbleColumn(players, 0, blockPos{0, 181, 0}) {
		t.Fatal("without its source the cell is no column")
	}
	for y := 181; y <= 183; y++ {
		if got := w.At(0, y, 0); got != worldgen.WaterBase {
			t.Fatalf("y=%d after mining: %d, want water", y, got)
		}
	}
	// Magma makes a whirlpool.
	w.SetBlock(5, 180, 0, magmaBlockState)
	w.SetBlock(5, 181, 0, worldgen.WaterBase)
	h.updateBubbleColumn(players, 0, blockPos{5, 181, 0})
	got := w.At(5, 181, 0)
	if got != worldgen.BubbleColumnDrag {
		t.Fatalf("magma: %d, want a whirlpool (%d)", got, worldgen.BubbleColumnDrag)
	}
	if !worldgen.IsWater(got) || worldgen.FluidLevel(got, worldgen.WaterBase) != 0 {
		t.Fatal("a column reads as source water")
	}
}

// TestBubbleColumnBreathable: eyes in a column do not drown.
func TestBubbleColumnBreathable(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl
	w := h.worldFor(0)
	for y := 180; y <= 183; y++ {
		w.SetBlock(0, y, 0, worldgen.BubbleColumnUp)
	}
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.air = 300
	h.environmentDamage(players, pl)
	if pl.air != 300 {
		t.Fatalf("air fell to %d inside a bubble column", pl.air)
	}
	w.SetBlock(0, 181, 0, worldgen.WaterBase)
	h.environmentDamage(players, pl)
	if pl.air == 300 {
		t.Fatal("plain water at the eyes drains air")
	}
}
