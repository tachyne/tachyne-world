package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A warm frog eats a small magma cube for a pearlescent froglight and a
// small slime for nothing; a big slime is not food.
func TestFrogEatsForFroglight(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.rules.DoMobLoot = true
	frog := h.spawnSpecies(players, entityFrog, 0, 0.5, 70, 0.5)
	if frog == nil {
		t.Fatal("no frog")
	}
	frog.variant, frog.variantSet = frogWarm, true
	cube := h.spawnSpecies(players, entityMagmaCube, 0, 1.5, 70, 0.5)
	if cube == nil {
		t.Fatal("no magma cube")
	}
	cube.size, cube.y = 1, 70
	frog.y = 70
	if !h.frogStep(players, frog) || cube.dying == 0 {
		t.Fatalf("the frog should eat the cube beside it: dying=%d", cube.dying)
	}
	cube.dying = 1
	items := len(h.items)
	h.despawnMob(players, cube)
	var got int32
	for _, it := range h.items {
		got = it.item
	}
	if len(h.items) != items+1 || got != froglightFor(frogWarm) {
		t.Fatalf("a warm frog's magma cube should leave a pearlescent froglight: items %d→%d item %d", items, len(h.items), got)
	}
	// A big slime is left alone.
	big := h.spawnSpecies(players, entitySlime, 0, 1.5, 70, 0.5)
	big.size, big.y = 2, 70
	if h.frogStep(players, frog) {
		t.Fatal("a big slime is not frog food")
	}
	// The sneeze wind-up lands twenty ticks later.
	cub := h.spawnSpecies(players, entityPanda, 0, 5, 70, 5)
	cub.baby = true
	cub.sneezeAt = h.tick.Load()
	h.pandaSneezeTick(players, cub)
	if cub.sneezeAt != 0 {
		t.Fatal("a due sneeze should land")
	}
}

// Frogs do not have babies: a bred pair lays a clutch of frogspawn on the
// water beside them, and it hatches into a handful of tadpoles.
func TestFrogLaysSpawnThatHatches(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	// A bank at y=70 with a pool of water beside it.
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			h.world.SetBlock(dx, 69, dz, worldgen.Stone)
			h.world.SetBlock(dx, 70, dz, worldgen.Air)
		}
	}
	h.world.SetBlock(1, 69, 0, worldgen.WaterBase)
	h.world.SetBlock(1, 70, 0, worldgen.Air)

	frog := h.spawnSpecies(players, entityFrog, 0, 0.5, 70, 0.5)
	if frog == nil {
		t.Fatal("no frog")
	}
	frog.pregnant = true
	h.frogLaySpawn(players, frog)
	if frog.pregnant {
		t.Fatal("a frog beside water lays its clutch")
	}
	spawn := blockPos{1, 70, 0}
	if got := h.world.At(spawn.x, spawn.y, spawn.z); got != frogspawnBlock {
		t.Fatalf("frogspawn should sit on the water, got state %d", got)
	}

	// A neighbour's update before its time does nothing; its own tick hatches
	// it: the block goes, tadpoles arrive.
	h.tickFrogspawn(players, 0, spawn, h.world.At(spawn.x, spawn.y, spawn.z))
	if h.world.At(spawn.x, spawn.y, spawn.z) != frogspawnBlock {
		t.Fatal("a neighbour update hatched the clutch early")
	}
	h.tick.Add(frogspawnMaxHatch)
	h.tickFrogspawn(players, 0, spawn, h.world.At(spawn.x, spawn.y, spawn.z))
	if h.world.At(spawn.x, spawn.y, spawn.z) != worldgen.Air {
		t.Error("hatching clears the clutch")
	}
	tadpoles := 0
	for _, m := range h.mobs {
		if m.etype == entityTadpole {
			tadpoles++
		}
	}
	if tadpoles < 2 || tadpoles > 5 {
		t.Fatalf("a clutch hatches two to five tadpoles, got %d", tadpoles)
	}
}

// entities/slime's frog branch: a small slime a frog eats leaves exactly
// one slime ball.
func TestFrogEatenSlimeDropsOneBall(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.rules.DoMobLoot = true
	frog := h.spawnSpecies(players, entityFrog, 0, 0.5, 70, 0.5)
	slime := h.spawnSpecies(players, entitySlime, 0, 1.5, 70, 0.5)
	slime.size, slime.y, frog.y = 1, 70, 70
	h.frogEat(players, frog, slime)
	slime.dying = 1
	before := len(h.items)
	h.despawnMob(players, slime)
	balls := 0
	for _, it := range h.items {
		if it.item == itemByName["slime_ball"] {
			balls += it.count
		}
	}
	if balls != 1 || len(h.items) != before+1 {
		t.Fatalf("a frog-eaten slime should drop one slime ball, got %d (%d items)", balls, len(h.items)-before)
	}
}
