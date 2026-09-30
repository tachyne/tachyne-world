package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// liquidPad lays a stone floor at y-1 with air above, around the origin,
// and loads it.
func liquidPad(w *world.World, y int) {
	w.ForceLoad(0, 0, 2)
	for x := -4; x <= 4; x++ {
		for z := -4; z <= 4; z++ {
			w.SetBlock(x, y-1, z, worldgen.Stone)
			for dy := 0; dy <= 2; dy++ {
				w.SetBlock(x, y+dy, z, worldgen.Air)
			}
		}
	}
}

// LiquidBlock.onPlace: a fluid set down by a player flows when its tick comes
// round, getTickDelay after the placement — 5 for water, 30 for lava, 10 for
// lava in the Nether — not on the next tick.
func TestLiquidPlacedFlowsAfterItsTickDelay(t *testing.T) {
	cases := []struct {
		name  string
		dim   int
		fluid uint32
		delay uint64
	}{
		{"water", dimOverworld, worldgen.WaterBase, 5},
		{"lava", dimOverworld, worldgen.LavaBase, 30},
		{"nether lava", dimNether, worldgen.LavaBase, 10},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newTestHub(world.New(1))
			nw, _ := world.NewNether(1, nil)
			h.dims.set(dimNether, nw)
			players := map[int32]*tracked{}
			w := h.worldFor(c.dim)
			const y = 100
			liquidPad(w, y)
			const start = 50
			h.tick.Store(start)
			w.SetBlock(0, y, 0, c.fluid)
			h.onBlock(players, evBlock{dim: c.dim, x: 0, y: y, z: 0, state: c.fluid, placed: true})
			if due, ok := h.fluidTicks[simPos{c.dim, blockPos{0, y, 0}}]; !ok || due != start+c.delay {
				t.Fatalf("fluid tick due %d (pending %v), want %d", due, ok, start+c.delay)
			}
			for tick := uint64(start + 1); tick < start+c.delay; tick++ {
				h.tick.Store(tick)
				h.runUpdates(players, tick)
				if s := w.At(1, y, 0); s != worldgen.Air {
					t.Fatalf("tick +%d: the %s already spread (%s)", tick-start, c.name, describeState(s))
				}
			}
			h.tick.Store(start + c.delay)
			h.runUpdates(players, start+c.delay)
			if s := w.At(1, y, 0); !worldgen.IsFluid(s) {
				t.Fatalf("at +%d the %s did not spread: %s", c.delay, c.name, describeState(s))
			}
		})
	}
}

// LiquidBlock.neighborChanged: water set down beside lava turns it at once —
// a source to obsidian, a flow to cobblestone — without waiting for a tick.
func TestLavaSetsSolidWhenWaterArrives(t *testing.T) {
	for _, c := range []struct {
		lava, want uint32
	}{
		{worldgen.LavaBase, worldgen.Obsidian},
		{worldgen.LavaBase + 2, worldgen.Cobblestone},
	} {
		h := newTestHub(world.New(1))
		players := map[int32]*tracked{}
		w := h.world
		const y = 100
		liquidPad(w, y)
		h.tick.Store(10)
		w.SetBlock(0, y, 0, c.lava)
		w.SetBlock(1, y, 0, worldgen.WaterBase)
		h.onBlock(players, evBlock{dim: dimOverworld, x: 1, y: y, z: 0, state: worldgen.WaterBase, placed: true})
		if got := w.At(0, y, 0); got != c.want {
			t.Errorf("lava %d beside new water became %s, want %s", c.lava, describeState(got), describeState(c.want))
		}
	}
}

// The basalt generator: lava over soul soil turns to basalt the moment blue
// ice is set beside it (shouldSpreadLiquid's second rule).
func TestLavaOverSoulSoilBesideBlueIceMakesBasalt(t *testing.T) {
	h := newTestHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.world
	const y = 100
	liquidPad(w, y)
	w.SetBlock(0, y-1, 0, worldgen.SoulSoil)
	w.SetBlock(0, y, 0, worldgen.LavaBase+2)
	w.SetBlock(1, y, 0, worldgen.BlueIce)
	h.onBlock(players, evBlock{dim: dimOverworld, x: 1, y: y, z: 0, state: worldgen.BlueIce, placed: true})
	if got := w.At(0, y, 0); got != worldgen.Basalt {
		t.Fatalf("lava over soul soil beside blue ice is %s, want basalt", describeState(got))
	}
	// Without the soul soil it stays lava.
	w.SetBlock(2, y-1, 2, worldgen.Stone)
	w.SetBlock(2, y, 2, worldgen.LavaBase+2)
	w.SetBlock(3, y, 2, worldgen.BlueIce)
	h.onBlock(players, evBlock{dim: dimOverworld, x: 3, y: y, z: 2, state: worldgen.BlueIce, placed: true})
	if got := w.At(2, y, 2); !worldgen.IsLava(got) {
		t.Fatalf("lava over stone beside blue ice became %s", describeState(got))
	}
}

// A cell holds one fluid tick at most: a neighbour change while one waits
// does not bring it forward or add a second (LevelChunkTicks.schedule).
func TestFluidTickKeepsTheFirstOne(t *testing.T) {
	h := newTestHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.world
	const y = 100
	liquidPad(w, y)
	h.tick.Store(20)
	w.SetBlock(0, y, 0, worldgen.WaterBase)
	h.onBlock(players, evBlock{dim: dimOverworld, x: 0, y: y, z: 0, state: worldgen.WaterBase, placed: true})
	h.tick.Store(22)
	w.SetBlock(0, y+1, 0, worldgen.Stone) // a neighbour change two ticks later
	h.onBlock(players, evBlock{dim: dimOverworld, x: 0, y: y + 1, z: 0, state: worldgen.Stone, placed: true})
	if due := h.fluidTicks[simPos{dimOverworld, blockPos{0, y, 0}}]; due != 25 {
		t.Fatalf("the water's tick is due at %d, want the first one's 25", due)
	}
}
