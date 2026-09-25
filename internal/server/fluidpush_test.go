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
	h := newHub(world.New(1))
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
	h := newHub(world.New(1))
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
	h := newHub(world.New(1))
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
	want := lavaSlowPushPerTick * mobMoveInterval
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
