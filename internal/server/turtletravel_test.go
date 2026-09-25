package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// At sea a turtle is always going somewhere (TurtleTravelGoal): leg after
// leg, never idling, where a land animal in the water would stand about.
func TestTurtleTravelsAtSea(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.world.ForceLoad(0, 0, 5)
	for x := -40; x <= 40; x++ {
		for z := -40; z <= 40; z++ {
			h.world.SetBlock(x, 178, z, worldgen.Stone)
			h.world.SetBlock(x, 179, z, worldgen.WaterBase)
			h.world.SetBlock(x, 180, z, worldgen.WaterBase)
			h.world.SetBlock(x, 181, z, worldgen.Air)
		}
	}
	m := h.spawnMob(players, entityTurtle, 0.5, 180, 0.5)
	m.x, m.y, m.z = 0.5, 180, 0.5
	m.home = blockPos{0, 180, 0} // at home: GoHome has nothing to do
	h.gridDirty()
	legs, try := 0, 0
	for i := 0; i < 500; i++ {
		h.gridDirty()
		h.updateMobs(players)
		if m.turtleLeg && m.turtleLegTry <= try {
			legs++ // a fresh leg
		}
		try = m.turtleLegTry
		if m.rest > 0 {
			t.Fatalf("update %d: a turtle at sea does not stand about (rest=%d)", i, m.rest)
		}
	}
	if legs < 2 {
		t.Errorf("it should swim leg after leg: %d legs", legs)
	}
	if d := math.Hypot(m.x-0.5, m.z-0.5); d < 1 {
		t.Errorf("it should have gone somewhere: %.2f from the start", d)
	}
}

// Ashore, a turtle's strolls come round on an interval of 100 rather than
// the usual 120 (TurtleRandomStrollGoal(1.0, 100)): its rests are shorter.
func TestTurtleStrollsOftenerAshore(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.world.ForceLoad(0, 0, 3)
	for x := -30; x <= 30; x++ {
		for z := -30; z <= 30; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Sand)
			for y := 180; y <= 183; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	m := h.spawnMob(players, entityTurtle, 0.5, 180, 0.5)
	m.home = blockPos{0, 180, 0}
	h.gridDirty()
	longest, rests := 0, 0
	for i := 0; i < 6000; i++ {
		before := m.rest
		m.x, m.z = 0.5, 0.5 // keep it in the middle of the beach
		h.gridDirty()
		h.updateMobs(players)
		if m.rest > before {
			rests++
			longest = max(longest, m.rest)
		}
	}
	if rests < 10 {
		t.Fatalf("too few rests sampled: %d", rests)
	}
	if limit := turtleRest(restMax); longest > limit {
		t.Errorf("longest rest %d ticks; at an interval of 100 it is at most %d", longest, limit)
	}
}
