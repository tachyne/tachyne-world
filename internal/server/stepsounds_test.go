package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Footfalls: a species with its own step plays it; one without plays the
// block's sound type at 0.15 volume; a carpet underfoot plays the carpet
// plus a muffled copy of the block beneath; powder snow wades; a camel on
// sand and a strider in lava have their surface variants; fliers are silent.
func TestFootstepChooser(t *testing.T) {
	w := world.New(1)
	x, y, z := 1300, 180, 1300
	clearAirBox(w, x, y, z, 2)
	w.SetBlock(x, y-1, z, worldgen.BlockID("grass_block"))
	at := func(m *mob) []stepSound { return footstepsFor(w, m, x, y-1, z) }

	if s := at(&mob{etype: entityCow}); len(s) != 1 || s[0].name != "minecraft:entity.cow.step" || s[0].volume != 0.15 {
		t.Fatalf("cow: %v", s)
	}
	if s := at(&mob{etype: entityZombifiedPiglin}); len(s) != 1 || s[0].name != "minecraft:entity.zombie.step" {
		t.Fatalf("zombified piglin walks like a zombie: %v", s)
	}
	if s := at(&mob{etype: entityCreeper}); len(s) != 1 || s[0].name != "minecraft:block.grass.step" || s[0].volume != 0.15 {
		t.Fatalf("creeper on grass: %v", s)
	}
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x, y, z, worldgen.BlockID("red_carpet"))
	if s := at(&mob{etype: entityCreeper}); len(s) != 2 || s[0].name != "minecraft:block.wool.step" || s[1].name != "minecraft:block.stone.step" || s[1].volume != 0.05 || s[1].pitch != 0.8 {
		t.Fatalf("creeper on a carpet over stone: %v", s)
	}
	w.SetBlock(x, y, z, worldgen.BlockID("powder_snow"))
	if s := at(&mob{etype: entityCreeper}); len(s) != 1 || s[0].name != "minecraft:block.powder_snow.step" {
		t.Fatalf("creeper in powder snow: %v", s)
	}
	w.SetBlock(x, y, z, worldgen.Air)
	w.SetBlock(x, y-1, z, worldgen.BlockID("sand"))
	if s := at(&mob{etype: entityCamel}); len(s) != 1 || s[0].name != "minecraft:entity.camel.step_sand" || s[0].volume != 1 {
		t.Fatalf("camel on sand: %v", s)
	}
	w.SetBlock(x, y, z, worldgen.LavaBase)
	if s := at(&mob{etype: entityStrider, x: float64(x) + 0.5, y: float64(y), z: float64(z) + 0.5}); len(s) != 1 || s[0].name != "minecraft:entity.strider.step_lava" {
		t.Fatalf("strider in lava: %v", s)
	}
	if s := at(&mob{etype: entityAllay}); s != nil {
		t.Fatalf("an allay makes no footsteps: %v", s)
	}
	if s := at(&mob{etype: entityTurtle, baby: true}); len(s) != 1 || s[0].name != "minecraft:entity.turtle.shamble_baby" {
		t.Fatalf("baby turtle: %v", s)
	}
}

// The cadence is vanilla's: 0.6 × distance accumulates and a step plays
// each time it passes a whole number — about every 1.67 blocks walked.
func TestFootstepCadence(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 1320, 180, 1320
	flatFloor(h.world, x, y, z, 6)
	m := &mob{etype: entityCreeper, x: float64(x) + 0.5, y: float64(y), z: float64(z) + 0.5}
	steps := 0
	for i := 0; i < 50; i++ { // 0.1 blocks per move: 5 blocks in all
		before := m.nextStep
		h.mobFootsteps(players, m, 0.1, 0, 0)
		m.x += 0.1
		if m.nextStep != before {
			steps++
		}
	}
	if steps != 3 { // 5 × 0.6 = 3.0 → passes 1, 2 and 3 (nextStep starts at 1)
		t.Fatalf("5 blocks walked should play 3 footsteps, got %d (moveDist %v next %v)", steps, m.moveDist, m.nextStep)
	}
	// Airborne: no step counts until something is underfoot.
	m.y = float64(y) + 3
	before := m.nextStep
	for i := 0; i < 30; i++ {
		h.mobFootsteps(players, m, 0.1, 0, 0)
	}
	if m.nextStep != before {
		t.Fatal("a mob in the air makes no footsteps")
	}
}
