package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestSpawnPatrolHasCaptain(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	h.rules.Difficulty = diffNormal
	lx, lz := h.findLand(40, 40)
	before := len(h.mobs)
	h.spawnPatrol(players, lx, lz)
	if len(h.mobs) <= before {
		t.Fatal("patrol spawned no pillagers")
	}
	caps, pill := 0, 0
	for _, m := range h.mobs {
		if m.etype == entityPillager {
			pill++
			if m.patrolCaptain {
				caps++
			}
		}
	}
	if pill < 2 {
		t.Fatalf("patrol should be a squad, got %d pillagers", pill)
	}
	if caps != 1 {
		t.Fatalf("a patrol has exactly one captain, got %d", caps)
	}
}

func TestPatrolGatedBeforeDay5(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{1: testTracked()}
	h.patrolNextAt = 1 // due immediately
	h.tick.Store(100)
	h.dayTime.Store(2 * dayLengthTicks) // day 2 — too early
	before := len(h.mobs)
	h.updatePatrols(players)
	if len(h.mobs) != before {
		t.Fatal("no patrol should spawn before day 5")
	}
}
