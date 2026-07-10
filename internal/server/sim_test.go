package server

import (
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

func runTicks(h *hub, players map[int32]*tracked, from, to uint64) {
	for tick := from; tick <= to; tick++ {
		h.tick.Store(tick)
		h.runUpdates(players, tick)
	}
}

func TestFallingBlock(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}

	const x, z = 100, 100
	surf := int(w.SurfaceY(x, z))
	for y := surf; y <= surf+10; y++ { // clear a column
		w.SetBlock(x, y, z, worldgen.Air)
	}
	w.SetBlock(x, surf+8, z, worldgen.Sand)
	h.scheduleAround(blockPos{x, surf + 8, z}, 1)
	runTicks(h, players, 1, 40)

	if got := w.Block(x, surf, z); got != worldgen.Sand {
		t.Errorf("sand should rest at y=%d, got block %d", surf, got)
	}
	if got := w.Block(x, surf+8, z); got != worldgen.Air {
		t.Errorf("original sand cell should be air, got %d", got)
	}
}

func TestWaterSpreadAndRecede(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}

	const fy = 100
	for dx := -3; dx <= 3; dx++ {
		for dz := -3; dz <= 3; dz++ {
			w.SetBlock(dx, fy, dz, worldgen.Stone) // floor
			for y := fy + 1; y <= fy+3; y++ {
				w.SetBlock(dx, y, dz, worldgen.Air)
			}
		}
	}
	src := blockPos{0, fy + 1, 0}
	w.SetBlock(src.x, src.y, src.z, worldgen.Water) // source (level 0)
	h.scheduleAround(src, 1)
	runTicks(h, players, 1, 80)

	if !worldgen.IsWater(w.Block(1, fy+1, 0)) {
		t.Errorf("water did not spread to the adjacent cell")
	}

	// Remove the source — water should recede.
	w.SetBlock(src.x, src.y, src.z, worldgen.Air)
	h.scheduleAround(src, 1)
	runTicks(h, players, 81, 400)

	if got := w.Block(1, fy+1, 0); worldgen.IsWater(got) {
		t.Errorf("water did not recede after source removed (block %d)", got)
	}
}

// TestLavaWaterInteraction covers vanilla's water/lava contact rules:
// source lava beside water → obsidian, flowing lava beside water → cobblestone,
// lava above water → stone below.
func TestLavaWaterInteraction(t *testing.T) {
	src := worldgen.LavaBase      // lava source (level 0)
	flow := worldgen.LavaBase + 2 // flowing lava (level 2)
	water := worldgen.WaterBase   // water source

	t.Run("source->obsidian", func(t *testing.T) {
		w := world.New(1)
		h := newHub(w)
		players := map[int32]*tracked{}
		w.SetBlock(50, 70, 50, src)
		w.SetBlock(51, 70, 50, water) // water beside
		h.updateFluid(players, blockPos{50, 70, 50}, src)
		if got := w.At(50, 70, 50); got != worldgen.Obsidian {
			t.Fatalf("source lava beside water = %d, want obsidian %d", got, worldgen.Obsidian)
		}
	})
	t.Run("flowing->cobblestone", func(t *testing.T) {
		w := world.New(1)
		h := newHub(w)
		players := map[int32]*tracked{}
		w.SetBlock(50, 70, 50, flow)
		w.SetBlock(50, 70, 51, water)
		h.updateFluid(players, blockPos{50, 70, 50}, flow)
		if got := w.At(50, 70, 50); got != worldgen.Cobblestone {
			t.Fatalf("flowing lava beside water = %d, want cobblestone %d", got, worldgen.Cobblestone)
		}
	})
	t.Run("above-water->stone", func(t *testing.T) {
		w := world.New(1)
		h := newHub(w)
		players := map[int32]*tracked{}
		w.SetBlock(50, 71, 50, src)   // lava above
		w.SetBlock(50, 70, 50, water) // water below
		h.updateFluid(players, blockPos{50, 71, 50}, src)
		if got := w.At(50, 70, 50); got != worldgen.Stone {
			t.Fatalf("water under falling lava = %d, want stone %d", got, worldgen.Stone)
		}
	})
}

// TestFluidSlopeFinding checks FlowingFluid.getSpread: on a flat floor with a
// hole two blocks east, the fluid flows only toward the hole, not all ways.
func TestFluidSlopeFinding(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	// A large stone platform high above the terrain (so the surroundings are
	// generated air, out of slope range) with air on top and one gap two east.
	const fy = 100
	for dx := -5; dx <= 5; dx++ {
		for dz := -5; dz <= 5; dz++ {
			w.SetBlock(60+dx, fy, 60+dz, worldgen.Stone)
			w.SetBlock(60+dx, fy+1, 60+dz, worldgen.Air)
		}
	}
	w.SetBlock(62, fy, 60, worldgen.Air) // the only drop-off, two blocks east
	dirs := h.flowDirections(blockPos{60, fy + 1, 60}, 4)
	if len(dirs) != 1 || dirs[0] != (blockPos{1, 0, 0}) {
		t.Fatalf("expected fluid to flow only east toward the hole, got %v", dirs)
	}
}
