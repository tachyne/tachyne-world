package server

import "testing"

// A vault's claim ledger and a spent spawner's cooldown are the two pieces of
// trial-chamber progress a player can lose money on: without them a restart
// hands out every reward again.

func TestVaultClaimsSurviveRestart(t *testing.T) {
	path := t.TempDir() + "/containers.json"
	pos := blockPos{40, -20, 91}
	claimant := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	other := [16]byte{9}

	s := newContainerStore(path)
	v := &vaultRecord{pos: pos, ominous: true}
	v.addRewarded(claimant)
	s.recordVaults(map[blockPos]*vaultRecord{pos: v})
	s.flush()

	got := newContainerStore(path).loadVaults()[pos]
	if got == nil {
		t.Fatal("vault record not restored")
	}
	if !got.hasRewarded(claimant) {
		t.Error("a player who already claimed this vault could claim it again after a restart")
	}
	if got.hasRewarded(other) {
		t.Error("a player who never claimed it is recorded as paid")
	}
}

// The cap keeps one vault's ledger bounded, dropping the oldest claim first.
func TestVaultRewardedCap(t *testing.T) {
	v := &vaultRecord{}
	first := [16]byte{0, 0, 0, 1}
	v.addRewarded(first)
	for i := 0; i < vaultMaxRewarded; i++ {
		v.addRewarded([16]byte{byte(i >> 8), byte(i), 0, 2})
	}
	if len(v.rewarded) != vaultMaxRewarded {
		t.Errorf("ledger holds %d claims, want the cap of %d", len(v.rewarded), vaultMaxRewarded)
	}
	if v.hasRewarded(first) {
		t.Error("the oldest claim should have been evicted, not a later one")
	}
	// Claiming twice is not two entries.
	dup := [16]byte{7, 7}
	v.addRewarded(dup)
	n := len(v.rewarded)
	v.addRewarded(dup)
	if len(v.rewarded) != n {
		t.Error("a repeat claim grew the ledger")
	}
}

func TestTrialCooldownSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/containers.json"
	pos := blockPos{12, -30, -44}
	fighter := [16]byte{2, 4, 6, 8}

	// A spawner that has just paid out: 10 minutes of its half-hour left.
	const now, left = 5000, 12000
	s := newContainerStore(path)
	ts := &trialSpawner{
		pos: pos, state: trialCooldown, cooldown: now + left,
		detected: map[[16]byte]bool{fighter: true}, current: map[int32]bool{},
	}
	s.recordTrials(map[blockPos]*trialSpawner{pos: ts}, now)
	s.flush()

	// The world age restarts at zero, so the deadline has to be rebased.
	got := newContainerStore(path).loadTrials(0)[pos]
	if got == nil {
		t.Fatal("spawner record not restored")
	}
	if got.state != trialCooldown {
		t.Errorf("state %d after a restart, want cooldown — a spent spawner re-armed", got.state)
	}
	if got.cooldown != left {
		t.Errorf("cooldown ends at %d, want %d ticks from now", got.cooldown, left)
	}
	if !got.detected[fighter] {
		t.Error("the player who fought was forgotten")
	}
}

// A fight in progress cannot resume — its mobs no longer answer to the spawner
// — so it falls back to waiting rather than to a half-counted round.
func TestTrialFightDoesNotResume(t *testing.T) {
	path := t.TempDir() + "/containers.json"
	pos := blockPos{1, 2, 3}
	s := newContainerStore(path)
	s.recordTrials(map[blockPos]*trialSpawner{pos: {
		pos: pos, state: trialActive, spawned: 4,
		detected: map[[16]byte]bool{}, current: map[int32]bool{},
	}}, 0)
	s.flush()

	got := newContainerStore(path).loadTrials(0)[pos]
	if got == nil {
		t.Fatal("spawner record not restored")
	}
	if got.state != trialWaitingPlayers || got.spawned != 0 {
		t.Errorf("state %d spawned %d, want a clean waiting spawner", got.state, got.spawned)
	}
}

// An untouched spawner writes nothing, so a world nobody has been near does not
// accumulate a record per chamber.
func TestTrialSleepingSpawnerNotRecorded(t *testing.T) {
	s := newContainerStore("")
	s.recordTrials(map[blockPos]*trialSpawner{{}: {
		state: trialWaitingPlayers, detected: map[[16]byte]bool{}, current: map[int32]bool{},
	}}, 100)
	if len(s.m.Trials) != 0 {
		t.Errorf("%d records for an untouched spawner, want none", len(s.m.Trials))
	}
}
