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
	h := newTestHub(world.New(1))
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

// TestBubbleColumnWaterFillsEmptiedCell: take the block under a column away
// and the column falls back to water — which then runs down into the empty
// cell (BubbleColumnBlock.updateShape schedules the water tick either way).
// The engine dropped that tick once the cell stopped being a column, so a
// sticky piston that pulled a lift's magma stopper back left a hole of air
// under a column of still water for good.
func TestBubbleColumnWaterFillsEmptiedCell(t *testing.T) {
	w := world.New(1)
	h := newTestHub(w)
	players := map[int32]*tracked{}
	w.ForceLoad(0, 0, 1)
	q := worldgen.BlockBase("quartz_bricks")
	for y := 178; y <= 186; y++ { // a sealed one-block shaft
		for x := -1; x <= 1; x++ {
			for z := -1; z <= 1; z++ {
				w.SetBlock(x, y, z, q)
			}
		}
	}
	w.SetBlock(0, 180, 0, magmaBlockState)
	for y := 181; y <= 184; y++ {
		w.SetBlock(0, y, 0, worldgen.BubbleColumnDrag)
	}
	w.SetBlock(0, 185, 0, worldgen.Air)
	w.SetBlock(0, 180, 0, worldgen.Air) // the magma pulled away
	h.scheduleAround(blockPos{0, 180, 0}, 1)
	stepTicks(h, players, 20)
	if got := w.At(0, 181, 0); got != worldgen.WaterBase {
		t.Fatalf("the cell over the emptied one is %s, want still source water", describeState(got))
	}
	if got := w.At(0, 180, 0); !worldgen.IsWater(got) || worldgen.IsFluidSource(got, worldgen.WaterBase) {
		t.Errorf("the emptied cell is %s, want water falling into it", describeState(got))
	}
}

// TestBubbleColumnBreathable: eyes in a column do not drown.
func TestBubbleColumnBreathable(t *testing.T) {
	h := newTestHub(world.New(1))
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

// TestBubbleColumnMovesSwimmers: a fish in an updraft rises, in a whirlpool
// it sinks (Entity.onInsideBubbleColumn).
func TestBubbleColumnMovesSwimmers(t *testing.T) {
	h := newTestHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	for y := 170; y <= 190; y++ {
		w.SetBlock(0, y, 0, worldgen.BubbleColumnUp)
		w.SetBlock(3, y, 0, worldgen.BubbleColumnDrag)
	}
	up := h.spawnMob(players, entityCod, 0.5, 180, 0.5)
	down := h.spawnMob(players, entityCod, 3.5, 180, 0.5)
	for i := 0; i < 10; i++ {
		h.swimMove(up, up.x, up.z, 0, 0)
		h.swimMove(down, down.x, down.z, 3, 0)
	}
	if up.y <= 180 || up.vy <= 0 {
		t.Fatalf("the updraft should lift the cod: y %.2f vy %.3f", up.y, up.vy)
	}
	if down.y >= 180 || down.vy >= 0 {
		t.Fatalf("the whirlpool should pull the cod down: y %.2f vy %.3f", down.y, down.vy)
	}
}
