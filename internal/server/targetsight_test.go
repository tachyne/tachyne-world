package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A zombie behind a wall does not come for you; one that saw you keeps
// after you for three seconds out of sight (fifteen once you hit it) and
// then gives up.
func TestHostilesHuntBySight(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	for x := -2; x <= 12; x++ {
		for z := -3; z <= 3; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	wall := func(on bool) {
		b := worldgen.Air
		if on {
			b = worldgen.Stone
		}
		for y := 180; y <= 183; y++ {
			for z := -3; z <= 3; z++ {
				w.SetBlock(4, y, z, b)
			}
		}
	}
	pl.x, pl.y, pl.z = 8.5, 180, 0.5
	z := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	z.hostile = true
	wall(true)
	h.acquireTarget(players, z)
	if z.hasTarget {
		t.Fatal("the zombie aggroed through a stone wall")
	}
	wall(false)
	h.acquireTarget(players, z)
	if !z.hasTarget || z.targetEID != pl.p.eid {
		t.Fatal("in the open the zombie did not aggro")
	}
	wall(true)
	for i := 0; i < targetUnseenMemory/mobMoveInterval; i++ {
		h.acquireTarget(players, z)
		if !z.hasTarget {
			t.Fatalf("the zombie gave up after %d ticks out of sight, before its memory ran out", (i+1)*mobMoveInterval)
		}
	}
	h.acquireTarget(players, z)
	if z.hasTarget {
		t.Fatal("the zombie kept hunting past its sixty-tick memory")
	}
	// Hit it and hide: it remembers you for three hundred ticks.
	h.provoke(z, pl)
	for i := 0; i < hurtByUnseenMemory/mobMoveInterval; i++ {
		h.acquireTarget(players, z)
		if !z.hasTarget {
			t.Fatalf("a hurt zombie gave up after %d ticks, before its memory ran out", (i+1)*mobMoveInterval)
		}
	}
	z.anger = 1 // still angry, but the memory is what runs out
	h.acquireTarget(players, z)
	if z.hasTarget {
		t.Fatal("the hurt zombie kept hunting past its three-hundred-tick memory")
	}
}
