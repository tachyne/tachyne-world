package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A frog put in a pond makes for the bank (TryFindLand) and is out of the
// water within a few seconds.
func TestFrogFindsLand(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			if x >= -3 && x <= 3 && z >= -3 && z <= 3 {
				h.world.SetBlock(x, 180, z, worldgen.Water)
			}
		}
	}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 180, 40.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	f := h.spawnMob(players, entityFrog, 0.5, 180, 0.5)
	h.gridDirty()
	out := -1
	for i := 0; i < 300; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		if !worldgen.IsWater(h.world.At(floorInt(f.x), floorInt(f.y), floorInt(f.z))) {
			out = i
			break
		}
	}
	if out < 0 {
		t.Fatalf("the frog should have left the pond: at %.1f %.1f %.1f", f.x, f.y, f.z)
	}
}

// frogLandNear takes the nearest dry spot on a solid top, never the water.
func TestFrogLandNearest(t *testing.T) {
	w := world.New(1)
	w.ForceLoad(0, 0, 1)
	for x := -9; x <= 9; x++ {
		for z := -9; z <= 9; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			w.SetBlock(x, 180, z, worldgen.Water)
		}
	}
	w.SetBlock(5, 180, 0, worldgen.Air) // the one dry cell, five steps east
	p, ok := frogLandNear(w, 0, 180, 0)
	if !ok || p != (blockPos{5, 180, 0}) {
		t.Fatalf("want the dry cell at 5,180,0, got %v %v", p, ok)
	}
}

// Out in open water, with no bank in reach, a frog swims about at 0.75 of
// its pace (RandomStroll.swim(0.75F)), not its full walking stroll.
func TestFrogSwimStrollSpeed(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 3)
	for x := -24; x <= 24; x++ {
		for z := -24; z <= 24; z++ {
			h.world.SetBlock(x, 178, z, worldgen.Stone)
			h.world.SetBlock(x, 179, z, worldgen.Water)
			h.world.SetBlock(x, 180, z, worldgen.Water)
		}
	}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 181, 60.5
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	f := h.spawnMob(players, entityFrog, 0.5, 179, 0.5)
	h.gridDirty()
	limit := f.moveSpeed() * 0.75 * 1.05
	moved := false
	for i := 0; i < 400; i++ {
		px, pz := f.x, f.z
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		d := dist3(f.x, 0, f.z, px, 0, pz)
		if d > limit && f.kb == 0 {
			t.Fatalf("update %d: a swimming frog strolls at 0.75 (%.3f), moved %.3f", i, limit, d)
		}
		moved = moved || d > 0
	}
	if !moved {
		t.Fatal("the frog never swam anywhere")
	}
}
