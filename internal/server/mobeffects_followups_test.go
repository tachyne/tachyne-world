package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A lingering cloud doses the mobs standing in it, not only players.
func TestLingeringCloudDosesMobs(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	m := h.spawnSpecies(players, entityZombie, 0, 50.5, 70, 50.5)
	h.spawnPotionCloud(0, 50.5, 70, 50.5, potSwiftness)
	h.updateClouds(players)
	if m.effects == nil || m.effects[effSpeed] == nil {
		t.Fatalf("the zombie in a Swiftness cloud should be sped up: %+v", m.effects)
	}
	far := h.spawnSpecies(players, entityZombie, 0, 80.5, 70, 50.5)
	h.tick.Store(h.tick.Load() + cloudReapply + 1)
	h.updateClouds(players)
	if far.effects != nil && far.effects[effSpeed] != nil {
		t.Error("a zombie thirty blocks away must not be dosed")
	}
}

// A hostile that picks up gear becomes persistent: the despawn sweep leaves
// it alone however far away it is, and the flag survives the store.
func TestPickedUpGearMakesMobPersistent(t *testing.T) {
	h := newHub(world.New(1))
	pl := riderAt(1, 10, 70, 10)
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	m := h.spawnSpecies(players, entityZombie, 0, 5000.5, 70, 5000.5)
	m.hostile = true // a monster (the legacy zombie takes its stance elsewhere): despawn distance 128
	m.canPickup = true
	h.spawnItemIn(players, 0, itemByName["iron_helmet"], 1, 5000.5, 70, 5000.5)
	h.mobPickupScan(players, m)
	if !m.persistent {
		t.Fatal("a zombie that took a helmet must be flagged persistent")
	}
	h.despawnSweep(players)
	if h.mobs[m.eid] == nil {
		t.Fatal("a persistent mob must survive the despawn sweep")
	}
	if sm := toSavedMob(m); !sm.Persistent {
		t.Error("persistence must ride the mob store")
	}
	// The control: an identical zombie without the flag is swept.
	plain := h.spawnSpecies(players, entityZombie, 0, 5000.5, 70, 5000.5)
	plain.hostile = true
	h.despawnSweep(players)
	if h.mobs[plain.eid] != nil {
		t.Error("a plain far-away zombie should despawn")
	}
}
