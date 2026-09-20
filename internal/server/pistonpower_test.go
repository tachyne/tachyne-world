package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Every ordinary way a dust line drives a piston, pinned against vanilla's
// rules. Legion reported "pistons adjacent to a redstone dust line are still
// not working"; each of these was checked against
// PistonBaseBlock.getNeighborSignal and RedStoneWireBlock.getSignal, and the
// two that do NOT fire are the two vanilla does not fire either — they are
// here so that stays deliberate.
func TestPistonPowerFromDust(t *testing.T) {
	dust := worldgen.BlockBase("redstone_wire")
	piston := func(facing string) uint32 {
		b := worldgen.BlockBase("piston")
		i, _ := worldgen.InfoForState(b)
		return setBoolProp(worldgen.SetProperty(i, b, "facing", facing), "extended", false)
	}
	// A floor to build on, and always air in front of the piston so a failure
	// means "not powered" and never "blocked".
	build := func(t *testing.T) (*hub, map[int32]*tracked, int, int, int) {
		h, w, players, x, y, z := redSetup(t)
		for dx := -2; dx < 12; dx++ {
			for dz := -2; dz < 3; dz++ {
				for dy := 0; dy <= 3; dy++ {
					w.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
				}
				w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			}
		}
		return h, players, x, y, z
	}
	settle := func(h *hub, players map[int32]*tracked, x, y, z int) {
		for dx := -2; dx < 12; dx++ {
			for dz := -2; dz < 3; dz++ {
				for dy := 0; dy <= 2; dy++ {
					h.rsSchedule(blockPos{x + dx, y + dy, z + dz}, 1)
				}
			}
		}
		stepTicks(h, players, 20)
	}
	line := func(w *worldW, x, y, z, n int) {
		w.SetBlock(x, y, z, worldgen.BlockBase("redstone_block"))
		for i := 1; i <= n; i++ {
			w.SetBlock(x+i, y, z, dust)
		}
	}

	t.Run("line ends at the piston", func(t *testing.T) {
		h, players, x, y, z := build(t)
		w := &worldW{h.world}
		line(w, x, y, z, 3)
		w.SetBlock(x+4, y, z, piston("east"))
		settle(h, players, x, y, z)
		if !boolProp(h.world.At(x+4, y, z), "extended") {
			t.Error("dust running into a piston should extend it")
		}
	})

	t.Run("dust into a block, piston on the far side", func(t *testing.T) {
		h, players, x, y, z := build(t)
		w := &worldW{h.world}
		line(w, x, y, z, 3)
		w.SetBlock(x+4, y, z, worldgen.Stone) // the dust points into this
		w.SetBlock(x+5, y, z, piston("east"))
		settle(h, players, x, y, z)
		if !boolProp(h.world.At(x+5, y, z), "extended") {
			t.Error("dust strongly powers the block it points into, which should drive the piston")
		}
	})

	t.Run("dust on a block above (quasi-connectivity)", func(t *testing.T) {
		h, players, x, y, z := build(t)
		w := &worldW{h.world}
		w.SetBlock(x+3, y, z, piston("east"))
		w.SetBlock(x+3, y+1, z, worldgen.Stone) // the QC block
		w.SetBlock(x+2, y+2, z, worldgen.BlockBase("redstone_block"))
		w.SetBlock(x+3, y+2, z, dust)
		settle(h, players, x, y, z)
		if !boolProp(h.world.At(x+3, y, z), "extended") {
			t.Error("quasi-connectivity: dust above the block over a piston should drive it")
		}
	})

	// The two vanilla does NOT fire.
	t.Run("line runs past, side on", func(t *testing.T) {
		h, players, x, y, z := build(t)
		w := &worldW{h.world}
		line(w, x, y, z, 5)
		w.SetBlock(x+3, y, z+1, piston("south")) // beside the middle of the line
		settle(h, players, x, y, z)
		if boolProp(h.world.At(x+3, y, z+1), "extended") {
			t.Error("a dust line running PAST a piston does not power it in vanilla — " +
				"dust only connects toward a signal source, and a piston is not one")
		}
	})

	t.Run("piston sitting on the dust", func(t *testing.T) {
		h, players, x, y, z := build(t)
		w := &worldW{h.world}
		line(w, x, y, z, 4)
		w.SetBlock(x+4, y+1, z, piston("east")) // directly above the last dust
		settle(h, players, x, y, z)
		if boolProp(h.world.At(x+4, y+1, z), "extended") {
			t.Error("dust does not power the block ABOVE it (getSignal returns 0 downward)")
		}
	})
}

// worldW narrows the world to the one method these fixtures need.
type worldW struct {
	w interface {
		SetBlock(x, y, z int, s uint32)
		At(x, y, z int) uint32
	}
}

func (r *worldW) SetBlock(x, y, z int, s uint32) { r.w.SetBlock(x, y, z, s) }
