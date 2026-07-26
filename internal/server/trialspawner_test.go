package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The block ids are pinned against the vanilla state report: the 12 states are
// ominous(true,false) x six spawner states, state varying fastest, with the
// OMINOUS half first. Getting the halves the wrong way round would leave every
// chamber looking ominous, which is what the templates already do.
func TestTrialSpawnerBlockStates(t *testing.T) {
	lo := trialSpawnerMin
	for _, c := range []struct {
		ominous bool
		st      trialState
		want    uint32
	}{
		{true, trialInactive, lo},
		{true, trialWaitingPlayers, lo + 1},
		{true, trialCooldown, lo + 5},
		{false, trialInactive, lo + 6},
		{false, trialWaitingPlayers, lo + 7},
		{false, trialActive, lo + 8},
		{false, trialWaitingEjection, lo + 9},
		{false, trialEjecting, lo + 10},
		{false, trialCooldown, lo + 11},
	} {
		if got := trialSpawnerBlock(c.ominous, c.st); got != c.want {
			t.Errorf("ominous=%v state=%d -> %d, want %d", c.ominous, c.st, got, c.want)
		}
	}
	if trialSpawnerMax != lo+11 {
		t.Errorf("trial_spawner range is %d..%d, want 12 states", lo, trialSpawnerMax)
	}
}

// The wave arithmetic is vanilla's, scaled by how many players turned up.
func TestTrialSpawnerWaveMath(t *testing.T) {
	ts := &trialSpawner{}
	if got := ts.targetTotal(0); got != 6 {
		t.Errorf("one player faces %d mobs, want 6", got)
	}
	if got := ts.targetTotal(1); got != 8 {
		t.Errorf("two players face %d mobs, want 8", got)
	}
	if got := ts.targetSimultaneous(0); got != 3 {
		t.Errorf("one player faces %d at once, want 3", got)
	}
	if got := ts.targetSimultaneous(2); got != 4 {
		t.Errorf("three players face %d at once, want 4", got)
	}
}

// The fight only starts when someone is close, and the whole run is: detect →
// spawn waves → last mob dies → shutter → pay out → cooldown.
func TestTrialSpawnerRunsAFight(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	pos := blockPos{0, 180, 0}
	// A room to fight in, well above the terrain: floor, and air to stand in.
	w := h.worldFor(0)
	stone, _ := worldgen.BlockRange("stone")
	for dx := -6; dx <= 6; dx++ {
		for dz := -6; dz <= 6; dz++ {
			w.SetBlock(dx, pos.y-1, dz, stone)
			w.SetBlock(dx, pos.y, dz, worldgen.Air)
			w.SetBlock(dx, pos.y+1, dz, worldgen.Air)
		}
	}
	pl.x, pl.y, pl.z = float64(pos.x), float64(pos.y), float64(pos.z)
	players[pl.p.eid] = pl

	ts := &trialSpawner{pos: pos, kind: "zombie",
		detected: map[int32]bool{}, current: map[int32]bool{}}

	// Nobody near: it waits.
	far := survPlayer(h)
	far.x, far.y, far.z = 500, 70, 500
	h.tickTrialSpawner(map[int32]*tracked{far.p.eid: far}, ts) // inactive -> waiting
	h.tickTrialSpawner(map[int32]*tracked{far.p.eid: far}, ts)
	if ts.state != trialWaitingPlayers {
		t.Fatalf("state %d with nobody near, want waiting", ts.state)
	}

	// A player walks in: the fight starts.
	h.tickTrialSpawner(players, ts)
	if ts.state != trialActive {
		t.Fatalf("state %d with a player in range, want active", ts.state)
	}

	// Run it out: mobs spawn up to the total, and killing them ends the fight.
	for i := 0; i < 20000 && ts.state == trialActive; i++ {
		h.tick.Add(1)
		for eid := range ts.current { // the player wins every fight, instantly
			if m := h.mobs[eid]; m != nil {
				h.removeMob(players, m)
			}
		}
		h.tickTrialSpawner(players, ts)
	}
	if ts.state != trialWaitingEjection {
		t.Fatalf("state %d after clearing the waves, want waiting-for-ejection", ts.state)
	}
	if ts.spawned != 0 {
		t.Errorf("spawned counter %d after the fight, want reset", ts.spawned)
	}

	// The shutter opens, then it pays out once per player who fought.
	before := len(h.items)
	for i := 0; i < 500 && ts.state != trialCooldown; i++ {
		h.tick.Add(1)
		h.tickTrialSpawner(players, ts)
	}
	if ts.state != trialCooldown {
		t.Fatalf("state %d after ejecting, want cooldown", ts.state)
	}
	if len(h.items) <= before {
		t.Error("the spawner paid out nothing")
	}
}
