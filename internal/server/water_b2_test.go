package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// IceBlock.playerDestroy: ice melts to water only over something that stops
// movement or over a liquid; over air it just goes.
func TestIceMeltsOnlyOverAFloorOrLiquid(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	players := map[int32]*tracked{}
	const x, y, z = 70, 180, 70
	w.SetBlock(x, y-1, z, worldgen.Air)
	w.SetBlock(x, y, z, worldgen.Air) // the ice was just broken
	h.iceMeltsOnBreak(players, 0, blockPos{x, y, z}, iceBlock)
	if got := w.At(x, y, z); got != worldgen.Air {
		t.Fatalf("ice broken over air left %d", got)
	}
	w.SetBlock(x, y-1, z, worldgen.Stone)
	h.iceMeltsOnBreak(players, 0, blockPos{x, y, z}, iceBlock)
	if got := w.At(x, y, z); got != worldgen.WaterBase {
		t.Fatalf("ice broken over stone left %d, want water", got)
	}
	w.SetBlock(x+2, y-1, z, worldgen.WaterBase+3)
	h.iceMeltsOnBreak(players, 0, blockPos{x + 2, y, z}, iceBlock)
	if got := w.At(x+2, y, z); got != worldgen.WaterBase {
		t.Fatalf("ice broken over running water left %d, want water", got)
	}
}

// Coral stays alive beside a waterlogged block (scanForWater reads the
// fluid state).
func TestCoralWetFromAWaterloggedNeighbour(t *testing.T) {
	w := world.New(1)
	const x, y, z = 96, 180, 96
	clearAirBox(w, x, y, z, 1)
	fan := worldgen.BlockID("tube_coral_fan")
	fi, _ := worldgen.InfoForState(fan)
	fan = worldgen.SetProperty(fi, fan, "waterlogged", "false")
	slab := worldgen.BlockID("stone_slab")
	si, _ := worldgen.InfoForState(slab)
	w.SetBlock(x+1, y, z, worldgen.SetProperty(si, slab, "waterlogged", "true"))
	if !coralTouchesWater(w, blockPos{x, y, z}, fan) {
		t.Fatal("coral beside a waterlogged slab reads as dry")
	}
}
