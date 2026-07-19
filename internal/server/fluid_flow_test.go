package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Fluid-flow scenarios reported by real play (Legion): water off a pillar, a
// leveling trough, source regeneration, receding, and a walled drop. All are
// built high above any terrain/ocean in a cleared air pocket so the natural
// world never bleeds into the test.

const fluidTestY = 180 // well above terrain + sea level, below the ceiling

// clearAir empties a box to air so the scenario sits in a vacuum.
func clearAir(w *world.World, x0, x1, y0, y1, z0, z1 int) {
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
}

func isWater(h *hub, x, y, z int) bool { return worldgen.IsWater(h.world.Block(x, y, z)) }

// settleFluid triggers a cell and runs the sim until it stops moving.
func settleFluid(h *hub, pos blockPos) {
	h.tick.Store(1)
	h.schedule(pos, 0)
	runTicks(h, h.playersRef, 1, 400)
}

// TestWaterOffPillarFallsStraight — water on a 1×1 pillar top pours off the edges
// and falls straight down; it must not spread 2+ blocks in mid-air (Legion bug 1).
func TestWaterOffPillarFallsStraight(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		const x, z = 40, 40
		floor := fluidTestY
		top := floor + 6
		clearAir(w, x-6, x+6, floor-2, top+2, z-6, z+6)
		for y := floor; y < top; y++ { // a 1×1×6 pillar
			w.SetBlock(x, y, z, worldgen.Stone)
		}
		for dx := -5; dx <= 5; dx++ { // ground below to catch the fall
			for dz := -5; dz <= 5; dz++ {
				w.SetBlock(x+dx, floor-1, z+dz, worldgen.Stone)
			}
		}
		w.SetBlock(x, top, z, worldgen.WaterBase)
		settleFluid(h, blockPos{x, top, z})

		for dx := -5; dx <= 5; dx++ { // nothing floats 2+ out at the top
			for dz := -5; dz <= 5; dz++ {
				if abs(dx)+abs(dz) >= 2 && isWater(h, x+dx, top, z+dz) {
					t.Fatalf("water floating %d,%d out at the top — should have fallen", dx, dz)
				}
			}
		}
		if !isWater(h, x+1, floor, z) && !isWater(h, x, floor, z+1) {
			t.Fatal("water never reached the ground beside the pillar")
		}
	})
}

// trough builds a closed 1-wide, 1-deep stone channel x∈[x0,x1] at (z,y).
func trough(w *world.World, x0, x1, z, y int) {
	clearAir(w, x0-2, x1+2, y, y+3, z-2, z+2)
	for x := x0 - 1; x <= x1+1; x++ {
		w.SetBlock(x, y-1, z, worldgen.Stone)
		w.SetBlock(x, y-1, z-1, worldgen.Stone)
		w.SetBlock(x, y-1, z+1, worldgen.Stone)
		w.SetBlock(x, y, z-1, worldgen.Stone)
		w.SetBlock(x, y, z+1, worldgen.Stone)
	}
	w.SetBlock(x0-1, y, z, worldgen.Stone)
	w.SetBlock(x1+1, y, z, worldgen.Stone)
}

// TestTroughLevels — water in a closed 1-deep trough fills and levels within it,
// never climbing over the walls (Legion bug 3).
func TestTroughLevels(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		const z = 40
		y := fluidTestY
		trough(w, 0, 5, z, y)
		w.SetBlock(0, y, z, worldgen.WaterBase)
		settleFluid(h, blockPos{0, y, z})
		for x := 0; x <= 5; x++ {
			if isWater(h, x, y+1, z) {
				t.Fatalf("water climbed out of the trough at x=%d", x)
			}
		}
		if !isWater(h, 4, y, z) {
			t.Fatal("water did not flow along the trough")
		}
	})
}

// TestSourceLineNoRegen — a 1×2 source pair is NOT infinite (removing one leaves
// only one source neighbour), but a 2×2 IS (Legion bug 4 / real vanilla).
func TestSourceLineNoRegen(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	box := func(x0, x1, z0, z1, y int) {
		clearAir(w, x0-2, x1+2, y, y+2, z0-2, z1+2)
		for x := x0 - 1; x <= x1+1; x++ {
			for z := z0 - 1; z <= z1+1; z++ {
				w.SetBlock(x, y-1, z, worldgen.Stone)
				if x < x0 || x > x1 || z < z0 || z > z1 {
					w.SetBlock(x, y, z, worldgen.Stone)
				}
			}
		}
	}
	onHub(t, h, func() {
		src := worldgen.WaterBase
		y := fluidTestY
		box(0, 1, 0, 0, y)
		w.SetBlock(0, y, 0, src)
		w.SetBlock(1, y, 0, src)
		w.SetBlock(0, y, 0, worldgen.Air)
		h.tick.Store(1)
		h.scheduleAround(blockPos{0, y, 0}, 0)
		runTicks(h, h.playersRef, 1, 120)
		if h.world.Block(0, y, 0) == src {
			t.Fatal("a 1×2 pair regenerated a source — needs ≥2 source neighbours")
		}

		box(20, 21, 0, 1, y)
		for _, p := range [][2]int{{20, 0}, {21, 0}, {20, 1}, {21, 1}} {
			w.SetBlock(p[0], y, p[1], src)
		}
		w.SetBlock(20, y, 0, worldgen.Air)
		h.tick.Store(121)
		h.scheduleAround(blockPos{20, y, 0}, 0)
		runTicks(h, h.playersRef, 121, 240)
		if h.world.Block(20, y, 0) != src {
			t.Fatalf("a 2×2 pool must regenerate the removed source (infinite water)")
		}
	})
}

// TestFallingWaterDoesNotGushOutWallHole — water dropping down a 1-wide shaft
// keeps falling when a wall block is broken beside it; a falling column does not
// spread sideways out the hole (Legion bug 2).
func TestFallingWaterDoesNotGushOutWallHole(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		const x, z = 40, 40
		top := fluidTestY
		bottom := top - 8
		clearAir(w, x-3, x+3, bottom-1, top+1, z-3, z+3)
		// A 1-wide vertical shaft: walls on all four sides down its height, a
		// stone floor at the bottom.
		for y := bottom; y <= top; y++ {
			w.SetBlock(x+1, y, z, worldgen.Stone)
			w.SetBlock(x-1, y, z, worldgen.Stone)
			w.SetBlock(x, y, z+1, worldgen.Stone)
			w.SetBlock(x, y, z-1, worldgen.Stone)
		}
		w.SetBlock(x, bottom-1, z, worldgen.Stone)
		w.SetBlock(x, top, z, worldgen.WaterBase) // source at the top → falls down
		settleFluid(h, blockPos{x, top, z})

		// Break a wall block partway down and let it settle.
		mid := bottom + 4
		w.SetBlock(x+1, mid, z, worldgen.Air)
		h.tick.Store(401)
		h.scheduleAround(blockPos{x + 1, mid, z}, 0)
		h.scheduleAround(blockPos{x, mid, z}, 0)
		runTicks(h, h.playersRef, 401, 700)

		// The falling column keeps going down; it must NOT gush out the hole.
		if isWater(h, x+1, mid, z) {
			t.Fatal("falling water gushed out the wall hole instead of continuing down")
		}
		if isWater(h, x+2, mid, z) {
			t.Fatal("water spread 2 blocks out of the wall hole")
		}
	})
}

// TestFlowingWaterSolidifiesPowder — concrete powder several blocks from a source
// turns to concrete as the water FLOWS over/beside it, not only where it is
// placed (Legion concrete report).
func TestFlowingWaterSolidifiesPowder(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		const z = 40
		y := fluidTestY
		powder := worldgen.BlockBase("white_concrete_powder")
		concrete := worldgen.ConcreteFor(powder)
		// A closed 1-wide channel whose FLOOR is concrete powder (x=1..6); the
		// source sits over stone at x=0 so the powder only meets FLOWING water.
		trough(w, 0, 6, z, y)
		for x := 1; x <= 6; x++ {
			w.SetBlock(x, y-1, z, powder)
		}
		w.SetBlock(0, y, z, worldgen.WaterBase)
		settleFluid(h, blockPos{0, y, z})

		for x := 1; x <= 6; x++ {
			if got := h.world.Block(x, y-1, z); got != concrete {
				t.Fatalf("powder floor at x=%d did not solidify under flowing water: got %d, want concrete %d", x, got, concrete)
			}
		}
	})
}

// TestFluidRecedesFully — remove a source and every flowing cell it fed dries up,
// leaving no floating remnants (Legion bug 5).
func TestFluidRecedesFully(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		const z = 40
		y := fluidTestY
		trough(w, 0, 5, z, y)
		w.SetBlock(0, y, z, worldgen.WaterBase)
		settleFluid(h, blockPos{0, y, z})
		if !isWater(h, 3, y, z) {
			t.Fatal("water did not spread along the trough first")
		}
		w.SetBlock(0, y, z, worldgen.Air)
		h.tick.Store(401)
		h.scheduleAround(blockPos{0, y, z}, 0)
		runTicks(h, h.playersRef, 401, 900)
		for x := 0; x <= 5; x++ {
			if isWater(h, x, y, z) {
				t.Fatalf("floating water remnant left at x=%d after the source was removed", x)
			}
		}
	})
}
