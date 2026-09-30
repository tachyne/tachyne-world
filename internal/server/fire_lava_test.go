package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Lava next to flammable material eventually ignites nearby air (vanilla
// LavaFluid.randomTick). Deterministic enough over many attempts.
func TestLavaIgnition(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		y := 70
		// Lava only lights what a player is near enough to see burn
		// (fire_spread_radius_around_player), so park the session's player
		// beside the pool.
		for _, pl := range h.playersRef {
			pl.dim, pl.x, pl.y, pl.z = 0, 30.5, float64(y), 0.5
		}
		planks := worldgen.BlockBase("oak_planks")
		w.SetBlock(30, y, 0, worldgen.LavaBase) // lava source
		w.SetBlock(31, y, 0, planks)            // flammable beside it
		w.SetBlock(31, y+1, 0, worldgen.Air)    // open above the planks
		lit := false
		for i := 0; i < 3000 && !lit; i++ {
			h.lavaIgnite(h.playersRef, 0, 30, y, 0)
			for dx := -1; dx <= 2; dx++ {
				for dy := 0; dy <= 3; dy++ {
					for dz := -1; dz <= 1; dz++ {
						if isFire(w.At(30+dx, y+dy, dz)) {
							lit = true
						}
					}
				}
			}
		}
		if !lit {
			t.Error("lava never ignited nearby flammable material in 3000 tries")
		}
	})
}

// Lava in the Nether runs as far as water and three times as fast as the
// overworld's (LavaFluid in an ultra-warm dimension: delay 10, drop-off 1).
func TestNetherLavaRunsFarther(t *testing.T) {
	reach := func(dim int) (int, uint64) {
		h := newTestHub(world.New(1))
		nw, _ := world.NewNether(1, nil)
		h.dims.set(dimNether, nw)
		w := h.worldFor(dim)
		w.ForceLoad(0, 0, 2)
		const y = 100
		// A walled channel one wide along +x, floored to x=14: the lava can
		// only run one way, and no drop pulls it off its course.
		for x := -1; x <= 14; x++ {
			for z := -1; z <= 1; z++ {
				w.SetBlock(x, y-1, z, worldgen.Stone)
				for dy := 0; dy <= 2; dy++ {
					cell := worldgen.Stone
					if z == 0 && x >= 0 {
						cell = worldgen.Air
					}
					w.SetBlock(x, y+dy, z, cell)
				}
			}
		}
		w.SetBlock(0, y, 0, worldgen.LavaBase)
		h.scheduleIn(dim, blockPos{0, y, 0}, 1)
		far, when := 0, uint64(0)
		for tick := uint64(1); tick <= 1500; tick++ {
			h.tick.Store(tick)
			h.runUpdates(nil, tick)
			for x := far + 1; x <= 12; x++ {
				if worldgen.IsLava(w.At(x, y, 0)) {
					far = x
					if x == 3 {
						when = tick // the time to the third block, which both reach
					}
				}
			}
		}
		return far, when
	}
	ow, owAt := reach(dimOverworld)
	nt, ntAt := reach(dimNether)
	if ow != 3 {
		t.Errorf("overworld lava reached %d blocks, want 3", ow)
	}
	if nt != 7 {
		t.Errorf("nether lava reached %d blocks, want 7", nt)
	}
	if ntAt >= owAt {
		t.Errorf("nether lava was no faster: %d ticks to three blocks, overworld %d", ntAt, owAt)
	}
}

// LavaFluid.getSpreadDelay asks a rising flow to wait four times as long,
// three times in four — but the tick's own write runs LiquidBlock.onPlace
// first, which schedules the usual 30, and a cell keeps the first fluid
// tick it is given. So a rise waits 30, every time, as vanilla's does.
func TestRisingLavaKeepsOnPlaceTick(t *testing.T) {
	for i := 0; i < 20; i++ {
		h := newTestHub(world.New(1))
		h.world.ForceLoad(0, 0, 1)
		h.rng.Seed(int64(i))
		players := map[int32]*tracked{}
		y := 180
		for x := -2; x <= 3; x++ {
			for z := -2; z <= 2; z++ {
				h.world.SetBlock(x, y-1, z, worldgen.Stone)
			}
		}
		h.world.SetBlock(0, y, 0, worldgen.LavaBase)   // a source
		h.world.SetBlock(1, y, 0, worldgen.LavaBase+6) // a thin flow beside it: it should rise to 2
		h.pending = map[uint64][]simPos{}
		now := h.tick.Load()
		h.updateFluid(players, 0, blockPos{1, y, 0}, worldgen.LavaBase+6)
		if got := worldgen.FluidLevel(h.world.At(1, y, 0), worldgen.LavaBase); got != 2 {
			t.Fatalf("the flow went to level %d, want 2", got)
		}
		due, ok := h.fluidTicks[simPos{0, blockPos{1, y, 0}}]
		if !ok || due-now != uint64(lavaDelay) {
			t.Fatalf("seed %d: the risen flow's fluid tick is due in %d (pending %v), want %d", i, due-now, ok, lavaDelay)
		}
	}
}

// LavaFluid.randomTick asks ignitedByLava, not the fire odds: a pool ringed
// with hay bales (flammable, but not lit by lava) never lights, and one
// ringed with banners (lit by lava, with no fire odds at all) does.
func TestLavaIgnitesByTheBlockProperty(t *testing.T) {
	lights := func(ring uint32) bool {
		h := newTestHub(world.New(1))
		h.world.ForceLoad(0, 0, 1)
		pl := testTracked()
		pl.x, pl.y, pl.z = 0.5, 180, 0.5
		players := map[int32]*tracked{1: pl}
		w := h.world
		for dx := -4; dx <= 4; dx++ {
			for dz := -4; dz <= 4; dz++ {
				w.SetBlock(dx, 179, dz, worldgen.Stone)
				for y := 180; y <= 184; y++ {
					w.SetBlock(dx, y, dz, worldgen.Air)
				}
				if dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1 {
					w.SetBlock(dx, 180, dz, ring)
				}
			}
		}
		w.SetBlock(0, 180, 0, worldgen.LavaBase)
		for i := 0; i < 3000; i++ {
			h.randomTickBlock(players, 0, 0, 180, 0)
			for dx := -4; dx <= 4; dx++ {
				for dz := -4; dz <= 4; dz++ {
					for y := 180; y <= 184; y++ {
						if isFire(w.At(dx, y, dz)) {
							return true
						}
					}
				}
			}
		}
		return false
	}
	hay := worldgen.BlockBase("hay_block")
	banner := worldgen.BlockBase("white_banner")
	if !worldgen.IgnitedByLava(banner) || worldgen.IgnitedByLava(hay) {
		t.Fatal("ignitedByLava: banners yes, hay no")
	}
	if lights(hay) {
		t.Error("lava lit a fire beside hay bales, which it does not ignite")
	}
	if !lights(banner) {
		t.Error("lava never lit a fire beside banners in 3000 random ticks")
	}
}
