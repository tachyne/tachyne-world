package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A river carried dropped items and left every mob standing still: the fluid
// current was applied in itemphys.go and nowhere else. Entity.updateFluidHeight
// AndDoFluidPushing runs for every entity in the fluid.
func TestACurrentCarriesAMob(t *testing.T) {
	h := newTestHub(world.New(1))
	w := h.world
	y := 180
	// A one-cell-wide channel running east, with a source at the west end so
	// the flow has somewhere to fall away to.
	for x := 0; x < 10; x++ {
		w.SetBlock(x, y-1, 0, worldgen.Stone)
		w.SetBlock(x, y, 0, worldgen.Air)
	}
	w.SetBlock(0, y, 0, worldgen.WaterBase)
	for x := 1; x < 8; x++ { // flowing water, losing a level each cell
		w.SetBlock(x, y, 0, worldgen.WaterBase+uint32(x))
	}

	m := &mob{etype: entityCow, x: 3.5, y: float64(y), z: 0.5, dim: dimOverworld}
	before := m.pushX
	h.applyFluidPush(m)
	if m.pushX == before {
		t.Fatal("a mob standing in flowing water should be pushed along it")
	}
	if m.pushX <= 0 {
		t.Errorf("the push should run downstream (+x), got %v", m.pushX)
	}
	// Normalised then scaled: the magnitude is the vanilla constant, whatever
	// the size of the height difference.
	want := waterPushPerTick * mobMoveInterval
	if got := math.Hypot(m.pushX, m.pushZ); math.Abs(got-want) > 1e-9 {
		t.Errorf("push magnitude %v, want %v (normalised × the vanilla scale)", got, want)
	}
}

// Still water and dry land move nothing.
func TestStillWaterPushesNothing(t *testing.T) {
	h := newTestHub(world.New(1))
	w := h.world
	y := 180
	for x := -1; x <= 1; x++ {
		for z := -1; z <= 1; z++ {
			w.SetBlock(x, y-1, z, worldgen.Stone)
			w.SetBlock(x, y, z, worldgen.WaterBase) // all sources: no gradient
		}
	}
	m := &mob{etype: entityCow, x: 0.5, y: float64(y), z: 0.5, dim: dimOverworld}
	h.applyFluidPush(m)
	if m.pushX != 0 || m.pushZ != 0 {
		t.Errorf("still water pushed a mob by (%v,%v)", m.pushX, m.pushZ)
	}

	dry := &mob{etype: entityCow, x: 60.5, y: 180, z: 60.5, dim: dimOverworld}
	h.applyFluidPush(dry)
	if dry.pushX != 0 || dry.pushZ != 0 {
		t.Errorf("dry land pushed a mob by (%v,%v)", dry.pushX, dry.pushZ)
	}
}

// Lava has a current too (0.0023 a tick outside the Nether), and a fish is
// never pushed (isPushedByFluid).
func TestLavaCurrentAndUnpushedSpecies(t *testing.T) {
	h := newTestHub(world.New(1))
	w := h.world
	y := 180
	for x := 0; x < 6; x++ {
		w.SetBlock(x, y-1, 0, worldgen.Stone)
	}
	w.SetBlock(0, y, 0, worldgen.LavaBase)
	for x := 1; x < 4; x++ { // lava loses two levels a cell in the overworld
		w.SetBlock(x, y, 0, worldgen.LavaBase+uint32(2*x))
	}
	m := &mob{etype: entityZombie, x: 1.5, y: float64(y), z: 0.5, dim: dimOverworld}
	h.applyFluidPush(m)
	// Lava's 0.0023 is under the 0.0045 a still entity is always given
	// (CurrentAccumulator.applyTo), so a zombie at rest takes the floor.
	want := fluidPushMinImpulse * mobMoveInterval
	if m.pushX <= 0 || math.Abs(math.Hypot(m.pushX, m.pushZ)-want) > 1e-9 {
		t.Errorf("lava push (%v,%v), want %v downstream", m.pushX, m.pushZ, want)
	}
	for x := 0; x < 6; x++ {
		w.SetBlock(x, y, 0, worldgen.WaterBase+uint32(min(x, 7)))
	}
	fish := &mob{etype: entityByName["cod"], x: 2.5, y: float64(y), z: 0.5, dim: dimOverworld}
	h.applyFluidPush(fish)
	if fish.pushX != 0 || fish.pushZ != 0 {
		t.Error("a cod was pushed by the current")
	}
}

// A swimmer in a waterfall is pulled down: the vertical part of the current
// (FlowingFluid.getFlow's -6 beside a solid face) reaches a mob that moves
// vertically.
func TestWaterfallPullsASwimmerDown(t *testing.T) {
	h := newTestHub(world.New(1))
	w := h.world
	x, y, z := 300, 180, 300
	w.ForceLoad(x, z, 1)
	for dy := -1; dy <= 2; dy++ {
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Stone)
			}
		}
	}
	for dy := 0; dy <= 1; dy++ {
		w.SetBlock(x, y+dy, z, worldgen.WaterBase+8) // falling water, walled in
	}
	m := &mob{etype: entityGuardian, swims: true, x: float64(x) + 0.5, y: float64(y), z: float64(z) + 0.5, dim: dimOverworld}
	h.applyFluidPush(m)
	if want := -waterPushPerTick * mobMoveInterval; math.Abs(m.vy-want) > 1e-9 {
		t.Errorf("a swimmer in a waterfall: vy %v, want %v", m.vy, want)
	}
	if m.pushX != 0 || m.pushZ != 0 {
		t.Errorf("a straight fall pushed sideways (%v,%v)", m.pushX, m.pushZ)
	}
}

// CurrentAccumulator.applyTo: a player takes the AVERAGE of the cells'
// flows where anything else takes the normalised sum, and an entity at
// rest is always given at least 0.0045.
func TestFluidImpulsePlayerAverageAndFloor(t *testing.T) {
	a := fluidAcc{fx: 1, n: 4} // four cells, one of them flowing
	if x, _, _, ok := a.impulse(waterPushPerTick, true, 1, 1); !ok || math.Abs(x-waterPushPerTick/4) > 1e-12 {
		t.Errorf("player impulse %v, want the average %v", x, waterPushPerTick/4)
	}
	if x, _, _, ok := a.impulse(waterPushPerTick, false, 1, 1); !ok || math.Abs(x-waterPushPerTick) > 1e-12 {
		t.Errorf("mob impulse %v, want the normalised %v", x, waterPushPerTick)
	}
	if x, _, _, _ := a.impulse(lavaSlowPushPerTick, false, 0, 0); math.Abs(x-fluidPushMinImpulse) > 1e-12 {
		t.Errorf("an entity at rest in slow lava: %v, want the 0.0045 floor", x)
	}
	if x, _, _, _ := a.impulse(lavaSlowPushPerTick, false, 0.1, 0); math.Abs(x-lavaSlowPushPerTick) > 1e-12 {
		t.Errorf("a moving entity in slow lava: %v, want %v", x, lavaSlowPushPerTick)
	}
	if _, _, _, ok := (fluidAcc{fx: 1e-3, n: 1}).impulse(waterPushPerTick, false, 0, 0); ok {
		t.Error("a current under 1e-5 squared must not push")
	}
}

// A lava current carries a dropped item (one lava will not burn), through
// the item tick: the engine used to push items in water only.
func TestLavaCurrentCarriesAnItem(t *testing.T) {
	h := newTestHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.world
	x, y, z := 1100, 180, 1100
	w.ForceLoad(x, z, 1)
	for dx := -1; dx <= 8; dx++ {
		w.SetBlock(x+dx, y-1, z, worldgen.Stone)
		w.SetBlock(x+dx, y, z-1, worldgen.Stone)
		w.SetBlock(x+dx, y, z+1, worldgen.Stone)
		for dy := 0; dy <= 3; dy++ {
			w.SetBlock(x+dx, y+dy, z, worldgen.Air)
		}
	}
	w.SetBlock(x-1, y, z, worldgen.Stone)
	w.SetBlock(x, y, z, worldgen.LavaBase)
	for dx := 1; dx <= 3; dx++ { // overworld lava loses two levels a cell
		w.SetBlock(x+dx, y, z, worldgen.LavaBase+uint32(2*dx))
	}
	start := float64(x) + 1.5
	it := h.spawnItemAt(players, 0, itemByName["netherite_ingot"], 1, start, float64(y), float64(z)+0.5, 0, 0, 0)
	for i := 0; i < 80; i++ {
		h.tickItems(players)
	}
	if _, alive := h.items[it.eid]; !alive {
		t.Fatal("a netherite ingot burnt in lava")
	}
	if it.x < start+0.3 {
		t.Errorf("the lava current should carry the item east: x=%v (start %v)", it.x, start)
	}
}
