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
		seenJump[m.jumpStrength()] = true

		if m.maxHP() < 15 || m.maxHP() > 30 {
			t.Fatalf("health %d outside vanilla's 15-30", m.maxHP())
		}
		if m.jumpStrength() < 0.4 || m.jumpStrength() > 1.0 {
			t.Fatalf("jump %v outside vanilla's 0.4-1.0", m.jumpStrength())
		}
		if sp := m.moveSpeed() / attrToStep; sp < 0.11 || sp > 0.34 { // the map holds per-step units
			t.Fatalf("speed %v outside vanilla's range", sp)
		}
	}
	if len(seenHP) < 5 || len(seenSpeed) < 5 || len(seenJump) < 5 {
		t.Errorf("horses barely vary: %d healths, %d speeds, %d jumps",
			len(seenHP), len(seenSpeed), len(seenJump))
	}
}

// AbstractChestedHorse.randomizeAttributes: donkeys, mules and llamas roll
// their health (15–30) and nothing else; a skeleton horse rolls its jump; a
// zombie horse rolls its own 0.5–0.7 jump and its speed.
func TestChestedHorsesRollHealth(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	for _, et := range []int{entityDonkey, entityMule, entityLlama, entityTraderLlama} {
		first := h.spawnSpecies(players, et, 0, 0, 70, 0)
		hps := map[int]bool{}
		for i := 0; i < 40; i++ {
			m := h.spawnSpecies(players, et, 0, float64(i), 70, 5)
			if m.maxHP() < 15 || m.maxHP() > 30 || m.health != m.maxHP() {
				t.Fatalf("%s: health %d/%d outside 15-30", entityNameByID[et], m.health, m.maxHP())
			}
			if m.jumpStrength() != first.jumpStrength() || m.moveSpeed() != first.moveSpeed() {
				t.Fatalf("%s: only health rolls", entityNameByID[et])
			}
			hps[m.maxHP()] = true
		}
		if len(hps) < 4 {
			t.Errorf("%s: health barely varies (%d values)", entityNameByID[et], len(hps))
		}
	}
	sk := h.spawnSpecies(players, entitySkeletonHorse, 0, 0, 70, 9)
	if sk.jumpStrength() < 0.4 || sk.jumpStrength() > 1.0 {
		t.Errorf("skeleton horse jump %v outside 0.4-1.0", sk.jumpStrength())
	}
	speeds := map[float64]bool{}
	for i := 0; i < 30; i++ {
		z := h.spawnSpecies(players, entityZombieHorse, 0, float64(i), 70, 12)
		if j := z.jumpStrength(); j < 0.5 || j > 0.7 {
			t.Fatalf("zombie horse jump %v outside 0.5-0.7", j)
		}
		if sp := z.moveSpeed() / attrToStep; sp < 0.2134 || sp > 0.2847 {
			t.Fatalf("zombie horse speed %v outside (9..12)/42.16", sp)
		}
		speeds[z.moveSpeed()] = true
	}
	if len(speeds) < 5 {
		t.Error("zombie horse speed should roll")
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
	a.setJumpStrength(1.0)
	b.setJumpStrength(1.0)
	a.setMaxHP(30)
	b.setMaxHP(30)
	h.breedHorseAttributes(a, b, foal)
	if foal.jumpStrength() < 0.7 {
		t.Errorf("two maximum parents gave a foal jumping %v", foal.jumpStrength())
	}
	if foal.maxHP() < 22 {
		t.Errorf("two maximum parents gave a foal with %d health", foal.maxHP())
	}
	// …and everything stays inside vanilla's range.
	a.setJumpStrength(0.4)
	b.setJumpStrength(0.4)
	h.breedHorseAttributes(a, b, foal)
	if foal.jumpStrength() < 0.4 || foal.jumpStrength() > 1.0 {
		t.Errorf("foal jump %v escaped the range", foal.jumpStrength())
	}
}
