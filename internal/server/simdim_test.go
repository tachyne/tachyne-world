package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The block simulation must run per dimension.
//
// Before this, runRandomTicks iterated every player, took their x/z, and ticked
// the OVERWORLD regardless of which dimension they were standing in. Two bugs
// fell out of that: nothing ever ticked in the Nether or the End, and a player
// in the Nether drove overworld ticks at the same x/z.

// netherHub returns a hub with a real Nether world attached.
func dimHub() *hub {
	h := newHub(world.New(1))
	h.nether = world.New(2)
	return h
}

// A player in the Nether must not cause overworld blocks to tick.
func TestNetherPlayerDoesNotTickOverworld(t *testing.T) {
	h := dimHub()

	x, y, z := 300, 70, 300
	base := worldgen.BlockBase("wheat")
	h.world.SetBlock(x, y-1, z, farmlandMin+7) // moist farmland: fastest growth
	h.world.SetBlock(x, y, z, base)

	// The only player is in the Nether, at the same x/z as the crop.
	p := testTracked()
	p.x, p.y, p.z, p.dim = float64(x), 70, float64(z), 1
	players := map[int32]*tracked{1: p}

	for i := 0; i < 400; i++ {
		h.runRandomTicks(players)
	}
	if got := h.world.At(x, y, z); got != base {
		t.Errorf("overworld crop grew (%d -> %d) with the only player in the Nether", base, got)
	}
}

// And the same player must tick the Nether's own world.
func TestNetherPlayerTicksTheNether(t *testing.T) {
	h := dimHub()

	x, y, z := 300, 70, 300
	// Sugar cane is the probe: it advances on a random tick with NO light gate,
	// which matters because the Nether has no skylight at all — a sapling would
	// never grow there and would prove nothing.
	h.nether.SetBlock(x, y-1, z, worldgen.Dirt)
	h.nether.SetBlock(x, y, z, caneMin)
	h.nether.SetBlock(x, y+1, z, worldgen.Air)

	p := testTracked()
	p.x, p.y, p.z, p.dim = float64(x), 70, float64(z), 1
	players := map[int32]*tracked{1: p}

	for i := 0; i < 4000; i++ {
		if h.nether.At(x, y, z) != caneMin {
			return // it advanced: the Nether is being simulated
		}
		h.runRandomTicks(players)
	}
	t.Error("nothing in the Nether ever random-ticked")
}

// Scheduled updates must land in the dimension they were scheduled for.
func TestScheduledTickRespectsDimension(t *testing.T) {
	h := dimHub()

	x, y, z := 400, 70, 400
	// Sand floating in the Nether should fall when its scheduled tick runs.
	h.nether.SetBlock(x, y, z, worldgen.Sand)
	h.nether.SetBlock(x, y-1, z, worldgen.Air)

	players := map[int32]*tracked{}
	h.scheduleIn(1, blockPos{x, y, z}, 1)
	for i := uint64(1); i < 20; i++ {
		h.tick.Store(i)
		h.runUpdates(players, i)
	}

	if h.nether.At(x, y, z) == worldgen.Sand {
		t.Error("sand in the Nether never fell: scheduled ticks skip the dimension")
	}
	if h.world.At(x, y, z) != worldgen.Air {
		t.Error("a Nether-scheduled tick wrote into the overworld")
	}
}
