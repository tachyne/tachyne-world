package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A save taken while a piston is sliding a block keeps the block: the
// moving cell's record goes out with the containers and comes back at boot
// (PistonMovingBlockEntity.saveAdditional/loadAdditional), so the block lands
// where it was going. Before, the record lived only in memory and the first
// neighbour update after a restart cleared the cell to air — the block the
// piston was moving was simply gone.
func TestMovingBlockSurvivesRestart(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	noFloor(w, x, y, z)
	stone := uint32(worldgen.Stone)
	w.SetBlock(x, y, z, pistonEast(false))
	w.SetBlock(x+1, y, z, stone)
	w.SetBlock(x, y, z-1, worldgen.BlockBase("redstone_block"))
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, 1)
	if !isMovingPiston(w.At(x+2, y, z)) {
		t.Fatalf("setup: the stone is not sliding (%d)", w.At(x+2, y, z))
	}

	// The save, through the store's file.
	path := filepath.Join(t.TempDir(), "containers.json")
	cs := newContainerStore(path)
	cs.recordMoving(h.snapshotMoving())
	cs.flush()

	// The restart: the hub's memory is gone, the world still holds the cells.
	h.movingBlocks, h.movingOrder = map[simPos]movingBlock{}, nil
	h.restoreMoving(newContainerStore(path).loadMoving())
	h.scheduleAround(blockPos{x + 2, y, z}, 1) // a neighbour update reaches the cell first
	stepTicks(h, players, movingPistonTicks+1)

	if got := w.At(x+2, y, z); got != stone {
		t.Fatalf("the stone's cell holds %d after the restart, want the stone", got)
	}
	if !isPistonHead(w.At(x+1, y, z)) {
		t.Fatalf("the head's cell holds %d, want the piston head", w.At(x+1, y, z))
	}
}

// A block a piston moves lands in the shape its new neighbours give it, and
// without its water (PistonMovingBlockEntity.tick: updateFromNeighbourShapes,
// then WATERLOGGED false). A fence pushed beside a stone joins it; a
// waterlogged slab arrives dry.
func TestPistonLandingShapeAndWater(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	noFloor(w, x, y, z)
	fence := withProps(t, worldgen.BlockBase("oak_fence"), map[string]string{
		"north": "false", "south": "false", "east": "false", "west": "false", "waterlogged": "false"})
	w.SetBlock(x, y, z, pistonEast(false))
	w.SetBlock(x+1, y, z, fence)
	w.SetBlock(x+2, y, z+1, worldgen.Stone) // beside where the fence arrives
	w.SetBlock(x, y, z-1, worldgen.BlockBase("redstone_block"))
	h.scheduleAround(blockPos{x, y, z}, 1)
	stepTicks(h, players, movingPistonTicks+2)
	got := w.At(x+2, y, z)
	if !inRanges(rangesOf([]string{"oak_fence"}), got) {
		t.Fatalf("the fence did not land: %d", got)
	}
	if props := worldgen.StateProps(got); props["south"] != "true" {
		t.Errorf("the landed fence does not join the stone beside it: %v", props)
	}

	h2, w2, players2, x2, y2, z2 := redSetup(t)
	noFloor(w2, x2, y2, z2)
	slab := withProps(t, worldgen.BlockBase("stone_slab"), map[string]string{"type": "bottom", "waterlogged": "true"})
	w2.SetBlock(x2, y2, z2, pistonEast(false))
	w2.SetBlock(x2+1, y2, z2, slab)
	w2.SetBlock(x2, y2, z2-1, worldgen.BlockBase("redstone_block"))
	h2.scheduleAround(blockPos{x2, y2, z2}, 1)
	stepTicks(h2, players2, movingPistonTicks+2)
	landed := w2.At(x2+2, y2, z2)
	if !inRanges(rangesOf([]string{"stone_slab"}), landed) {
		t.Fatalf("the slab did not land: %d", landed)
	}
	if worldgen.IsWaterlogged(landed) {
		t.Error("the slab carried its water along; a moved block arrives dry")
	}
}
