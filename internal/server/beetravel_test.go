package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The whole point: a bee that has picked up pollen flies home and goes in.
// Before the route existed it hovered where it stood, bobbing, because the
// altitude spring in flyMove pulled it back to cruising height every tick
// while the errand pushed it toward a hive up a tree — the two cancelled.

func beeTravelWorld(t *testing.T) (*hub, map[int32]*tracked, blockPos) {
	t.Helper()
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.hives = map[blockPos][]hiveOccupant{}
	airBox(t, h.world, -4, 70, -4, 30, 90, 12)
	hive := blockPos{20, 78, 2} // up in the air, as a nest in a tree is
	h.world.SetBlock(hive.x, hive.y, hive.z, beeNestMin)
	h.registerHive(hive)
	return h, players, hive
}

// movesPerBeeUpdate is how many movement steps fall between two bee updates in
// the real engine: updateBees runs on the 1 Hz survival pass, updateMobs every
// mobMoveInterval ticks. Ten to one. A harness that steps them together models
// a bee that flies a tenth as fast as the real one — which stayed invisible
// only while the trip deadline was wrong in the same direction.
const movesPerBeeUpdate = survivalTickN / mobMoveInterval

// flyBee ticks a bee's errand until it is inside the hive or the budget (in
// bee updates, i.e. seconds) runs out, returning how many it took.
func flyBee(h *hub, players map[int32]*tracked, m *mob, budget int) int {
	for i := 0; i < budget; i++ {
		if _, still := h.mobs[m.eid]; !still {
			return i // gone into the hive
		}
		h.updateBee(players, m, true, false)
		for j := 0; j < movesPerBeeUpdate; j++ {
			if _, still := h.mobs[m.eid]; !still {
				return i
			}
			dvx, dvz := m.behavior.steer(h, m)
			m.vx = m.vx*0.85 + dvx*0.15
			m.vz = m.vz*0.85 + dvz*0.15
			if sp := math.Hypot(m.vx, m.vz); sp > m.moveSpeed() {
				m.vx, m.vz = m.vx/sp*m.moveSpeed(), m.vz/sp*m.moveSpeed()
			}
			nx, nz := m.x+m.vx, m.z+m.vz
			h.flyMove(m, nx, nz, int(math.Floor(nx)), int(math.Floor(nz)))
		}
	}
	return budget
}

func TestBeeWithNectarFliesHomeAndEnters(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := h.spawnAnimal(players, entityBee, 0, 0)
	if m == nil {
		t.Fatal("no bee")
	}
	m.x, m.y, m.z = 0.5, 72, 0.5
	m.beeHome, m.beeHasHome = hive, true
	m.beeNectar = true

	startD := dist3(m.x, m.y, m.z, float64(hive.x), float64(hive.y), float64(hive.z))
	flyBee(h, players, m, 60)

	if len(h.hives[hive]) > 0 {
		return // went in: the errand completed
	}
	endD := dist3(m.x, m.y, m.z, float64(hive.x), float64(hive.y), float64(hive.z))
	t.Errorf("bee never reached the hive: %.1f blocks away after 60 s (started %.1f); "+
		"y=%.1f vs hive y=%d", endD, startD, m.y, hive.y)
}

// The specific failure Wesley saw: a bee that must CLIMB to its hive. The old
// code could not gain height at all, so this is the regression guard.
func TestBeeClimbsToAHiveAboveIt(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := h.spawnAnimal(players, entityBee, 0, 0)
	if m == nil {
		t.Fatal("no bee")
	}
	m.x, m.y, m.z = float64(hive.x)+0.5, 72, float64(hive.z)+0.5 // directly below it
	m.beeHome, m.beeHasHome = hive, true
	m.beeNectar = true

	flyBee(h, players, m, 60)
	if len(h.hives[hive]) > 0 {
		return
	}
	if m.y < float64(hive.y)-2 {
		t.Errorf("bee is still at y=%.1f under a hive at y=%d — it never climbed", m.y, hive.y)
	}
}

// A bee whose hive is walled off gives up on it rather than hovering for ever,
// and remembers not to try again (BeeGoToHiveGoal's blacklist).
func TestBeeGivesUpOnAnUnreachableHive(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	stone, _ := worldgen.BlockRange("stone")
	for dx := -1; dx <= 1; dx++ { // brick the hive in solid
		for dy := -1; dy <= 1; dy++ {
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dy == 0 && dz == 0 {
					continue
				}
				h.world.SetBlock(hive.x+dx, hive.y+dy, hive.z+dz, stone)
			}
		}
	}
	m := h.spawnAnimal(players, entityBee, 0, 0)
	if m == nil {
		t.Fatal("no bee")
	}
	m.x, m.y, m.z = 0.5, 72, 0.5
	m.beeHome, m.beeHasHome = hive, true
	m.beeNectar = true

	flyBee(h, players, m, 100)
	if !m.beeHiveBanned(hive) {
		t.Error("an unreachable hive was never blacklisted — the bee will hover at it for ever")
	}
}

// Bee.aiStep: a hive left far behind stops being this bee's hive.
func TestBeeDropsAHiveLeftTooFarBehind(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := h.spawnAnimal(players, entityBee, 0, 0)
	if m == nil {
		t.Fatal("no bee")
	}
	m.beeHome, m.beeHasHome = hive, true
	m.x, m.y, m.z = float64(hive.x)+beeTooFar+10, 75, float64(hive.z)
	h.updateBee(players, m, true, false)
	if m.beeHasHome && m.beeHome == hive {
		t.Errorf("a hive %.0f blocks away is still the bee's home (vanilla drops it past %.0f)",
			beeTooFar+10, beeTooFar)
	}
}

// BeeWanderGoal: past the leash the wander is biased back toward the hive.
func TestBeeWanderPullsBackTowardTheHive(t *testing.T) {
	h, players, hive := beeTravelWorld(t)
	m := h.spawnAnimal(players, entityBee, 0, 0)
	if m == nil {
		t.Fatal("no bee")
	}
	m.beeHome, m.beeHasHome = hive, true
	m.beeGoalKind = beeGoalKindNone
	// Just inside the drop distance, well outside the wander leash.
	m.x, m.y, m.z = float64(hive.x)+beeWanderLeashHome+8, 75, float64(hive.z)
	vx, _ := beeWander(h, m)
	if vx >= 0 {
		t.Errorf("wander steer x=%.3f — past the leash it should pull back toward the hive", vx)
	}
}
