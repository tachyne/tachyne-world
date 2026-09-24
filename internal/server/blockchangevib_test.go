package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestSculkHearsABell: BellBlock.attemptToRing is a BLOCK_CHANGE game
// event, so a sculk sensor in range hears a bell being rung.
func TestSculkHearsABell(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	x, y, z := 4, 180, 4
	w.ForceLoad(x, z, 2)
	for dx := -2; dx <= 6; dx++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			w.SetBlock(x+dx, y, z+dz, worldgen.Air)
			w.SetBlock(x+dx, y+1, z+dz, worldgen.Air)
		}
	}
	sensor := worldgen.BlockBase("sculk_sensor") + 1
	w.SetBlock(x, y, z, sensor)
	h.onBlock(players, evBlock{dim: dimOverworld, x: x, y: y, z: z, state: sensor})
	bell := worldgen.BlockBase("bell")
	w.SetBlock(x+3, y, z, bell)
	stepSculk(h, players, sensorActiveTicks+sensorCooldownTicks+2) // let the placements settle
	if !h.ringBell(players, dimOverworld, blockPos{x + 3, y, z}, -1) {
		t.Fatal("the bell did not ring")
	}
	stepSculk(h, players, 5)
	if f := h.sculkFreq[simPos{dim: dimOverworld, blockPos: blockPos{x, y, z}}]; f != freqBlockChange {
		t.Fatalf("the sensor heard frequency %d, want the bell's BLOCK_CHANGE %d", f, freqBlockChange)
	}
}
