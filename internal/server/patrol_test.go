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

// PatrolSpawner.tick: no patrol for a player within two sections of a
// village's points of interest, or for a spectator; one out in the open is
// eventually visited.
func TestPatrolVillageAndSpectatorGates(t *testing.T) {
	try := func(setup func(h *hub, pl *tracked)) bool {
		h := newHub(world.New(7))
		h.rules.Difficulty = diffNormal
		h.rules.SpawnPatrols, h.rules.DoMobSpawning = true, true
		pl := testTracked()
		players := map[int32]*tracked{pl.p.eid: pl}
		lx, lz := landRing(h)
		pl.x, pl.y, pl.z = float64(lx)+0.5, float64(h.world.SurfaceFeet(lx, lz)), float64(lz)+0.5
		h.world.ForceLoad(lx, lz, 5)
		setup(h, pl)
		h.dayTime.Store(6*dayLengthTicks + 1000) // day 6, morning
		for i := 0; i < 400; i++ {
			h.patrolNextAt = 1 // due now (0 is the first arming after boot)
			h.tick.Store(uint64(100 + i))
			h.updatePatrols(players)
			if len(h.mobs) > 0 {
				return true
			}
		}
		return false
	}
	if !try(func(*hub, *tracked) {}) {
		t.Skip("no patrol found room to spawn here in 400 tries")
	}
	if try(func(h *hub, pl *tracked) {
		raidVillage(h, blockPos{floorInt(pl.x) + 20, floorInt(pl.y), floorInt(pl.z)})
	}) {
		t.Fatal("a patrol came to a player next to a village")
	}
	if try(func(h *hub, pl *tracked) { pl.gamemode = gmSpectator }) {
		t.Fatal("a patrol came to a spectator")
	}
}

// landRing finds a spot whose patrol ring (24-48 blocks out, every
// diagonal) is dry, spawnable land.
func landRing(h *hub) (int, int) {
	for x := 0; x < 4000; x += 96 {
		for z := 0; z < 4000; z += 96 {
			ok := true
			for _, d := range [][2]int{{30, 30}, {-30, 30}, {30, -30}, {-30, -30}, {40, 40}, {-40, -40}} {
				if !h.world.Spawnable(x+d[0], z+d[1]) {
					ok = false
					break
				}
			}
			if ok && h.world.Spawnable(x, z) {
				return x, z
			}
		}
	}
	return 0, 0
}
