package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Every horse was a clone: 22 health, one speed, no jump at all. Vanilla rolls
// all three, which is the entire reason anyone breeds for a good one.
func TestHorsesVary(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	seenHP, seenSpeed, seenJump := map[int]bool{}, map[float64]bool{}, map[float64]bool{}
	for i := 0; i < 200; i++ {
		m := h.spawnSpecies(players, entityHorse, 0, float64(i), 70, 0)
		if m == nil {
			t.Fatal("spawn returned nil")
		}
		seenHP[m.maxHP()] = true
		seenSpeed[m.moveSpeed()] = true
		seenJump[m.jumpStrength] = true

		if m.maxHP() < 15 || m.maxHP() > 30 {
			t.Fatalf("health %d outside vanilla's 15-30", m.maxHP())
		}
		if m.jumpStrength < 0.4 || m.jumpStrength > 1.0 {
			t.Fatalf("jump %v outside vanilla's 0.4-1.0", m.jumpStrength)
		}
		if m.moveSpeed() < 0.11 || m.moveSpeed() > 0.34 {
			t.Fatalf("speed %v outside vanilla's range", m.moveSpeed())
		}
	}
	if len(seenHP) < 5 || len(seenSpeed) < 5 || len(seenJump) < 5 {
		t.Errorf("horses barely vary: %d healths, %d speeds, %d jumps",
			len(seenHP), len(seenSpeed), len(seenJump))
	}
}

// Donkeys and mules are the dependable ones — vanilla rolls nothing for them.
func TestDonkeysDoNotVary(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	first := h.spawnSpecies(players, entityDonkey, 0, 0, 70, 0)
	for i := 0; i < 30; i++ {
		m := h.spawnSpecies(players, entityDonkey, 0, float64(i), 70, 5)
		if m.maxHP() != first.maxHP() || m.jumpStrength != 0 {
			t.Fatalf("a donkey varied: hp=%d jump=%v", m.maxHP(), m.jumpStrength)
		}
	}
	// A skeleton horse rolls its jump but nothing else.
	sk := h.spawnSpecies(players, entitySkeletonHorse, 0, 0, 70, 9)
	if sk.jumpStrength < 0.4 || sk.jumpStrength > 1.0 {
		t.Errorf("skeleton horse jump %v outside 0.4-1.0", sk.jumpStrength)
	}
}

// A foal lands between its parents rather than being rolled from scratch.
func TestFoalInheritsFromItsParents(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	a := h.spawnSpecies(players, entityHorse, 0, 0, 70, 0)
	b := h.spawnSpecies(players, entityHorse, 0, 1, 70, 0)
	foal := h.spawnSpecies(players, entityHorse, 0, 2, 70, 0)

	// Two excellent parents should not produce a hopeless foal.
	a.jumpStrength, b.jumpStrength = 1.0, 1.0
	a.setMaxHP(30)
	b.setMaxHP(30)
	h.breedHorseAttributes(a, b, foal)
	if foal.jumpStrength < 0.7 {
		t.Errorf("two maximum parents gave a foal jumping %v", foal.jumpStrength)
	}
	if foal.maxHP() < 22 {
		t.Errorf("two maximum parents gave a foal with %d health", foal.maxHP())
	}
	// …and everything stays inside vanilla's range.
	a.jumpStrength, b.jumpStrength = 0.4, 0.4
	h.breedHorseAttributes(a, b, foal)
	if foal.jumpStrength < 0.4 || foal.jumpStrength > 1.0 {
		t.Errorf("foal jump %v escaped the range", foal.jumpStrength)
	}
}
