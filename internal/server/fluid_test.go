package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestInfiniteWaterSource(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		src := worldgen.WaterBase // level 0 = source
		y := 70
		// Two source blocks with a solid floor and a flowing cell between them.
		w.SetBlock(10, y-1, 0, worldgen.Stone)
		w.SetBlock(11, y-1, 0, worldgen.Stone)
		w.SetBlock(12, y-1, 0, worldgen.Stone)
		w.SetBlock(10, y, 0, src)
		w.SetBlock(12, y, 0, src)
		w.SetBlock(11, y, 0, worldgen.WaterBase+1) // flowing between two sources
		h.processUpdate(h.playersRef, blockPos{11, y, 0})
		if got := w.Block(11, y, 0); got != src {
			t.Errorf("middle cell = %d, want source %d (infinite water)", got-worldgen.WaterBase, 0)
		}
	})
}

func TestConcretePowderSolidifies(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		powder := worldgen.BlockBase("white_concrete_powder")
		y := 70
		w.SetBlock(20, y-1, 0, worldgen.Stone)
		w.SetBlock(20, y, 0, powder)
		w.SetBlock(21, y, 0, worldgen.WaterBase) // water beside it
		h.processUpdate(h.playersRef, blockPos{20, y, 0})
		want := worldgen.ConcreteFor(powder)
		if got := w.Block(20, y, 0); got != want {
			t.Errorf("powder = %d, want concrete %d", got, want)
		}
		if worldgen.BlockBase("white_concrete") != want {
			t.Errorf("ConcreteFor(white powder) = %d, want white_concrete %d", want, worldgen.BlockBase("white_concrete"))
		}
	})
}

func TestWaterlogging(t *testing.T) {
	stairs := worldgen.BlockBase("oak_stairs")
	info, ok := worldgen.InfoForState(stairs)
	if !ok || !info.HasProperty("waterlogged") {
		t.Skip("oak_stairs has no waterlogged property in this build")
	}
	dry := worldgen.SetProperty(info, stairs, "waterlogged", "false")
	wet := worldgen.SetProperty(info, stairs, "waterlogged", "true")
	if worldgen.IsWaterlogged(dry) {
		t.Error("IsWaterlogged true for a dry stair")
	}
	if !worldgen.IsWaterlogged(wet) {
		t.Error("IsWaterlogged false for a waterlogged stair")
	}
}
