package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestTurtleEggLifeCycle: breeding turtles gives one an egg instead of a
// hatchling, she carries it home and lays a clutch on sand after two
// hundred ticks, and the clutch cracks and hatches on random ticks in the
// hour before dawn.
func TestTurtleEggLifeCycle(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			w.SetBlock(x, 179, z, worldgen.Sand)
			w.SetBlock(x, 180, z, worldgen.Air)
		}
	}
	a := h.spawnAnimal(players, entityTurtle, 0, 0)
	b := h.spawnAnimal(players, entityTurtle, 1, 0)
	a.x, a.y, a.z = 0.5, 180, 0.5
	b.x, b.y, b.z = 1.5, 180, 0.5
	a.home = blockPos{0, 180, 0}
	a.loveTicks, b.loveTicks = loveTicks, loveTicks
	before := len(h.mobs)
	h.updateBreeding(players)
	if len(h.mobs) != before || !(a.hasEgg || b.hasEgg) {
		t.Fatalf("turtles breed an egg, not a hatchling: mobs %d→%d, eggs %v/%v", before, len(h.mobs), a.hasEgg, b.hasEgg)
	}
	mother := a
	if b.hasEgg {
		mother = b
		mother.home = blockPos{0, 180, 0}
	}
	// Far from home: she heads back.
	mother.x, mother.z = 30.5, 0.5
	if !h.turtleStep(players, mother) || mother.vx >= 0 {
		t.Fatalf("with an egg and far from home the turtle heads home: vx %.3f", mother.vx)
	}
	// Home, on sand: two hundred ticks of digging, then the clutch.
	mother.x, mother.z = 0.5, 0.5
	laid := false
	for i := 0; i < 110 && !laid; i++ {
		h.turtleStep(players, mother)
		_, _, laid = turtleEggOf(w.At(0, 180, 0))
	}
	if !laid || mother.hasEgg {
		t.Fatalf("no clutch after two hundred ticks: egg %v block %d", mother.hasEgg, w.At(0, 180, 0))
	}
	eggs, hatch, _ := turtleEggOf(w.At(0, 180, 0))
	if eggs < 1 || eggs > 4 || hatch != 0 {
		t.Fatalf("clutch %d eggs hatch %d", eggs, hatch)
	}
	// Random ticks in the pre-dawn window crack it twice, then hatch it.
	h.dayTime.Store(22000)
	h.turtleEggRandomTick(players, 0, 0, 180, 0, w.At(0, 180, 0))
	if _, hatch, _ = turtleEggOf(w.At(0, 180, 0)); hatch != 1 {
		t.Fatalf("first crack: hatch %d", hatch)
	}
	h.turtleEggRandomTick(players, 0, 0, 180, 0, w.At(0, 180, 0))
	before = len(h.mobs)
	h.turtleEggRandomTick(players, 0, 0, 180, 0, w.At(0, 180, 0))
	if w.At(0, 180, 0) != worldgen.Air || len(h.mobs) != before+eggs {
		t.Fatalf("hatching: block %d, mobs %d→%d for %d eggs", w.At(0, 180, 0), before, len(h.mobs), eggs)
	}
	for _, m := range h.mobs {
		if m.etype == entityTurtle && m.baby && m.home != (blockPos{0, 180, 0}) {
			t.Fatalf("a hatchling's home is the nest: %+v", m.home)
		}
	}
	// By day, off the window, a tick almost never cracks an egg.
	w.SetBlock(0, 180, 0, turtleEggState(2, 0))
	h.dayTime.Store(6000)
	h.rng.Seed(1)
	cracked := 0
	for i := 0; i < 50; i++ {
		h.turtleEggRandomTick(players, 0, 0, 180, 0, w.At(0, 180, 0))
		if _, hatch, _ := turtleEggOf(w.At(0, 180, 0)); hatch > 0 {
			cracked++
			w.SetBlock(0, 180, 0, turtleEggState(2, 0))
		}
	}
	if cracked > 3 {
		t.Fatalf("daytime cracks should be rare: %d of 50", cracked)
	}
}

// A turtle out of the water heads back to it — a hatchling twice as fast —
// and a grown one far from its beach swims home.
func TestTurtleGoesToWaterAndHome(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	for x := -10; x <= 10; x++ {
		for z := -10; z <= 10; z++ {
			h.world.SetBlock(x, 69, z, worldgen.Sand)
			h.world.SetBlock(x, 70, z, worldgen.Air)
		}
	}
	for z := 5; z <= 8; z++ { // a pool to the +z side
		for x := -3; x <= 3; x++ {
			h.world.SetBlock(x, 70, z, worldgen.WaterBase)
		}
	}
	m := h.spawnMob(players, entityTurtle, 0.5, 70, 0.5)
	m.setMoveSpeed(0.1)
	m.home = blockPos{0, 70, 0}
	if !h.turtleWaterStep(m) {
		t.Fatal("a dry turtle should head for the water")
	}
	if m.vz <= 0 {
		t.Errorf("the pool is at +z, so should the turtle be going: vz=%v", m.vz)
	}
	// A hatchling hurries at double pace.
	adult := math.Hypot(m.vx, m.vz)
	m.baby, m.vx, m.vz = true, 0, 0
	h.turtleWaterStep(m)
	if baby := math.Hypot(m.vx, m.vz); baby < adult*1.9 {
		t.Errorf("a hatchling moves at twice the pace: %v vs %v", baby, adult)
	}
	// In the water, far from home, it eventually turns for the beach.
	m.baby, m.vx, m.vz = false, 0, 0
	m.x, m.z = 0.5, 6.5
	m.home = blockPos{0, 70, 200}
	homing := false
	for i := 0; i < 2000 && !homing; i++ {
		homing = h.turtleWaterStep(m)
	}
	if !homing {
		t.Fatal("a turtle far from its beach should head home eventually")
	}
	if m.vz <= 0 {
		t.Errorf("home is at +z: vz=%v", m.vz)
	}
}
