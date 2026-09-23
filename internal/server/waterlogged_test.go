package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bug #25, rebuilt from its capture: a waterlogged bottom stair facing west,
// quartz bricks to its west and north, open to the east and south. Its water
// runs out of the open east side and past the south profile, as vanilla's
// does, and not through the back. Draining the stair takes that water away.
func TestWaterloggedStairPoursOutItsOpenSides(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	x, y, z := 3000, 180, 3000
	w.ForceLoad(x, z, 2)
	for dx := -6; dx <= 6; dx++ { // a floor in open air
		for dz := -6; dz <= 6; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			for dy := 0; dy <= 2; dy++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	info, _ := worldgen.InfoForState(worldgen.BlockBase("smooth_quartz_stairs"))
	stair := worldgen.BlockBase("smooth_quartz_stairs")
	for k, v := range map[string]string{"facing": "west", "half": "bottom", "shape": "straight", "waterlogged": "true"} {
		stair = worldgen.SetProperty(info, stair, k, v)
	}
	bricks := worldgen.BlockBase("quartz_bricks")
	w.SetBlock(x, y, z, stair)
	w.SetBlock(x-1, y, z, bricks) // behind it
	w.SetBlock(x, y, z-1, bricks) // to the north
	h.scheduleAround(blockPos{x, y, z}, 1)
	runTicks(h, players, 1, 200)

	if !worldgen.IsWater(w.Block(x+1, y, z)) {
		t.Errorf("no water out of the open east side (got %d)", w.Block(x+1, y, z))
	}
	if !worldgen.IsWater(w.Block(x, y, z+1)) {
		t.Errorf("no water past the south profile (got %d)", w.Block(x, y, z+1))
	}
	if w.Block(x-1, y, z) != bricks || w.Block(x, y, z-1) != bricks {
		t.Error("the walls behind the stair changed")
	}

	// Drain the stair: the water it fed recedes.
	w.SetBlock(x, y, z, worldgen.SetProperty(info, stair, "waterlogged", "false"))
	h.scheduleAround(blockPos{x, y, z}, 1)
	runTicks(h, players, 201, 800)
	for _, p := range [][3]int{{x + 1, y, z}, {x, y, z + 1}, {x + 2, y, z}} {
		if worldgen.IsWater(w.Block(p[0], p[1], p[2])) {
			t.Errorf("water at %v stayed after the stair was drained", p)
		}
	}
}
