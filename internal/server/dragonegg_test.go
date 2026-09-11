package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Using or hitting the dragon egg moves it to an empty cell within vanilla's
// reach; a cell that is not an egg does nothing.
func TestDragonEggBlinks(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[pl.p.eid] = pl
	x, z := h.findLand(60, 60)
	y := h.world.SurfaceFeet(x, z) + 20 // up in the air, where every cell around is empty
	h.world.SetBlock(x, y, z, worldgen.DragonEgg)
	h.onDragonEgg(players, evDragonEgg{eid: pl.p.eid, x: x, y: y, z: z})
	if h.world.At(x, y, z) != worldgen.Air {
		t.Fatal("the egg should have left its cell")
	}
	found := 0
	for dx := -15; dx <= 15; dx++ {
		for dy := -7; dy <= 7; dy++ {
			for dz := -15; dz <= 15; dz++ {
				if h.world.At(x+dx, y+dy, z+dz) == worldgen.DragonEgg {
					found++
					if dx == 0 && dy == 0 && dz == 0 {
						t.Error("the egg stayed put")
					}
				}
			}
		}
	}
	if found != 1 {
		t.Fatalf("found %d eggs within reach, want 1", found)
	}
	h.onDragonEgg(players, evDragonEgg{eid: pl.p.eid, x: x, y: y, z: z}) // not an egg any more
	if h.world.At(x, y, z) != worldgen.Air {
		t.Error("an empty cell must not spawn an egg")
	}
}
