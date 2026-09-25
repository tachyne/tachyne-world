package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// EnderDragon.checkWalls: flying into a wall of wood tears it out; end stone
// (#dragon_immune) stands, and so does everything when mob_griefing is off.
func TestDragonBreaksWhatItFliesThrough(t *testing.T) {
	h, pl, players := endHub(t)
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	m := h.dragon
	m.x, m.y, m.z, m.yaw = 0.5, 120, 0.5, 0
	w := h.worldFor(2)
	// A slab of planks around the dragon's body, and end stone beside it.
	for x := -3; x <= 3; x++ {
		for y := 119; y <= 124; y++ {
			for z := -3; z <= 3; z++ {
				w.SetBlock(x, y, z, worldgen.BlockID("oak_planks"))
			}
		}
	}
	w.SetBlock(0, 121, 1, worldgen.BlockID("end_stone"))
	h.rules.MobGriefing = true
	if h.dragonCheckWalls(players, m, false) != true {
		t.Fatal("the end stone should count as a wall")
	}
	if st := w.At(0, 121, 0); st != worldgen.Air {
		t.Fatalf("planks in the body box should be torn out, got %d", st)
	}
	if st := w.At(0, 121, 1); st != worldgen.BlockID("end_stone") {
		t.Fatal("end stone is #dragon_immune and must stand")
	}
	if st := w.At(3, 124, 3); st != worldgen.BlockID("oak_planks") {
		t.Fatal("a corner outside the head/neck/body boxes should be untouched")
	}

	w.SetBlock(0, 121, 0, worldgen.BlockID("oak_planks"))
	h.rules.MobGriefing = false
	if !h.dragonCheckWalls(players, m, false) {
		t.Fatal("without mob_griefing every block is a wall")
	}
	if w.At(0, 121, 0) != worldgen.BlockID("oak_planks") {
		t.Fatal("without mob_griefing the dragon must break nothing")
	}
}

// The dragon's own movement runs the check: a flight through a tree line
// leaves a tunnel.
func TestDragonFlightCarvesThroughTerrain(t *testing.T) {
	h, pl, players := endHub(t)
	h.onDimSwitch(players, pl, evDim{eid: 1, dim: 2, x: 100.5, y: 49, z: 0.5})
	h.rules.MobGriefing = true
	m := h.dragon
	w := h.worldFor(2)
	x, y, z := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	w.SetBlock(x, y+1, z, worldgen.BlockID("oak_log"))
	h.tick.Store(1)
	h.updateDragon(players)
	if st := w.At(x, y+1, z); st == worldgen.BlockID("oak_log") {
		t.Fatal("a log inside the dragon's body should be torn out by its flight")
	}
}
