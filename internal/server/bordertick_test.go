package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// LevelChunk.isTicking: a lit furnace past the world border does not cook
// (block entities stop ticking there), while one inside does.
func TestBlockEntitiesStopPastTheBorder(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.border.Size = 64 // centred on 0,0: the wall is 32 out
	h.publishBorder()
	furnaceSt := worldgen.BlockBase("furnace")
	mk := func(x int) *furnace {
		h.world.SetBlock(x, 150, 0, furnaceSt)
		f := &furnace{kind: cookFurnace}
		f.slots[furnaceInput] = invStack{item: itemByName["raw_iron"], count: 1}
		f.burnLeft, f.burnMax = 1000, 1000
		h.furnaces[simPos{blockPos: blockPos{x, 150, 0}}] = f
		return f
	}
	in, out := mk(4), mk(100)
	for i := 0; i < 210; i++ {
		h.updateFurnaces(players)
	}
	if in.slots[furnaceOutput].count != 1 {
		t.Errorf("the furnace inside the border did not smelt: %+v", in.slots)
	}
	if out.slots[furnaceOutput].count != 0 || out.cook != 0 {
		t.Errorf("the furnace past the border cooked: %+v cook %d", out.slots, out.cook)
	}
}
