package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Daylight wipes the siege state so the next dusk rolls afresh (vanilla resets
// the state machine whenever it is day).
func TestVillageSiegeDayResets(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.siegeState, h.siegeRolled, h.siegeLeft = siegeActive, true, 5
	h.dayTime.Store(1000) // broad daylight

	h.updateVillageSiege(players)
	if h.siegeState != siegeDone || h.siegeRolled || h.siegeLeft != 0 {
		t.Fatalf("daylight should clear the siege: state=%d rolled=%v left=%d",
			h.siegeState, h.siegeRolled, h.siegeLeft)
	}
}

// The nightly roll happens once, and only in the dusk window.
func TestVillageSiegeRollsOnceAtDusk(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	// Deep night, past the dusk window: no roll is made.
	h.dayTime.Store(nightStart + siegeDuskWindow + 500)
	h.updateVillageSiege(players)
	if h.siegeRolled {
		t.Fatal("a siege may only be rolled during the dusk window")
	}

	// At dusk it rolls; with no players there is no village, so tonight passes.
	h.dayTime.Store(nightStart + 100)
	h.updateVillageSiege(players)
	if !h.siegeRolled {
		t.Fatal("dusk should make tonight's roll")
	}
	if h.siegeState != siegeDone {
		t.Fatalf("with no village to besiege the night should pass, got state %d", h.siegeState)
	}
}

// An active siege releases its zombies around the village and then finishes.
func TestVillageSiegeReleasesZombies(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	lx, lz := h.findLand(0, 0) // put the village on real ground
	h.siegeCenter = blockPos{lx, h.world.SurfaceFeet(lx, lz), lz}
	h.siegeState, h.siegeLeft = siegeActive, 4
	h.dayTime.Store(nightStart + 2000) // night

	before := len(h.mobs)
	for i := 0; i < 20 && h.siegeState == siegeActive; i++ {
		h.updateVillageSiege(players)
	}
	if h.siegeState != siegeDone {
		t.Fatalf("the siege should finish once its zombies are out, state=%d left=%d",
			h.siegeState, h.siegeLeft)
	}
	if len(h.mobs) <= before {
		t.Fatal("an active siege should have spawned zombies")
	}
}

// A village with no villagers left is not besieged.
func TestVillageSiegeCountsVillagers(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	c := blockPos{100, 70, 100}

	if n := h.countVillagersNear(c, siegeRadius); n != 0 {
		t.Fatalf("an empty village should count 0 villagers, got %d", n)
	}
	if m := h.spawnMob(players, entityVillager, float64(c.x)+2, float64(c.y), float64(c.z)+2); m == nil {
		t.Fatal("failed to spawn the test villager")
	}
	if n := h.countVillagersNear(c, siegeRadius); n != 1 {
		t.Fatalf("a villager beside the centre should be counted, got %d", n)
	}
	// Far outside the radius it must not count toward the village.
	if n := h.countVillagersNear(blockPos{c.x + 10*siegeRadius, c.y, c.z}, siegeRadius); n != 0 {
		t.Fatalf("a distant villager should not count, got %d", n)
	}
}
