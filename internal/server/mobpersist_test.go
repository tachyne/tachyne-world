package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestMobPersistRoundTrip: mobs bucketed to mobs.json come back with their
// per-instance state (age, health, tamed owner, gear) intact on a fresh hub
// when their chunk is reloaded.
func TestMobPersistRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mobs.json")
	players := map[int32]*tracked{}

	h := newHub(world.New(1))
	h.mobstore = newMobStore(path)

	cow := h.spawnMob(players, entityCow, 10.5, 70, 10.5)
	h.applySpecies(players, cow)
	cow.health, cow.baby, cow.growLeft = 7, true, 500

	wolf := h.spawnMob(players, entityWolf, 11.5, 70, 12.5)
	h.applySpecies(players, wolf)
	wolf.tamed, wolf.sitting = true, true
	wolf.ownerUUID = [16]byte{1, 2, 3, 4}

	zombie := h.spawnHostileY(players, entityZombie, 30.5, 70, 30.5)
	helmet := itemByName["iron_helmet"]
	zombie.gear[0] = invStack{item: helmet, count: 1}

	// Their chunks are "loaded"; snapshot the live set and flush.
	active := map[[2]int32]bool{{0, 0}: true, {1, 1}: true}
	h.activeChunks = active
	h.mobstore.bucketLive(h.mobs, h.persistMob, active)
	h.mobstore.flush()

	// A fresh hub reloads them when those chunks enter range.
	h2 := newHub(world.New(1))
	h2.mobstore = newMobStore(path)
	chunkSet := map[[2]int32]bool{{0, 0}: true, {1, 1}: true}
	h2.reconcileMobChunks(players, chunkSet)

	if len(h2.mobs) != 3 {
		t.Fatalf("reloaded %d mobs, want 3", len(h2.mobs))
	}
	var gotCow, gotWolf, gotZombie *mob
	for _, m := range h2.mobs {
		switch m.etype {
		case entityCow:
			gotCow = m
		case entityWolf:
			gotWolf = m
		case entityZombie:
			gotZombie = m
		}
	}
	if gotCow == nil || !gotCow.baby || gotCow.health != 7 || gotCow.growLeft != 500 {
		t.Fatalf("cow state lost: %+v", gotCow)
	}
	if gotWolf == nil || !gotWolf.tamed || !gotWolf.sitting || gotWolf.ownerUUID != [16]byte{1, 2, 3, 4} {
		t.Fatalf("tamed wolf state lost: %+v", gotWolf)
	}
	if gotWolf.owner != 0 {
		t.Fatal("restored pet owner eid must start unresolved (0) until the owner joins")
	}
	if gotZombie == nil || !gotZombie.hostile || gotZombie.gear[0].item != helmet {
		t.Fatalf("hostile zombie / gear lost: %+v", gotZombie)
	}
}

// TestMobUnloadReload: a mob whose chunk leaves range unloads after the grace
// window (saved to the store, dropped from the live set) and reloads on return.
func TestMobUnloadReload(t *testing.T) {
	h := newHub(world.New(1))
	h.mobstore = newMobStore(filepath.Join(t.TempDir(), "mobs.json"))
	players := map[int32]*tracked{}
	h.tick.Store(1000)

	cow := h.spawnMob(players, entityCow, 85.5, 70, 85.5) // chunk (5,5)
	cid := cow.eid
	inRange := map[[2]int32]bool{{5, 5}: true}

	h.reconcileMobChunks(players, inRange) // (5,5) becomes active, cow stays live
	if _, ok := h.mobs[cid]; !ok {
		t.Fatal("an in-range mob must stay live")
	}

	// Chunk leaves range: within the grace window the mob is retained.
	empty := map[[2]int32]bool{}
	h.reconcileMobChunks(players, empty)
	if _, ok := h.mobs[cid]; !ok {
		t.Fatal("a just-departed chunk keeps its mobs during the grace window")
	}

	// Past the grace window it unloads: gone from the live set, parked in the store.
	h.tick.Store(1000 + mobUnloadGrace)
	h.reconcileMobChunks(players, empty)
	if _, ok := h.mobs[cid]; ok {
		t.Fatal("past the grace window the mob should unload")
	}
	if !h.mobstore.has(5, 5) {
		t.Fatal("an unloaded mob must be saved to its chunk bucket")
	}

	// Returning to the chunk reloads it.
	h.reconcileMobChunks(players, inRange)
	got := 0
	for _, m := range h.mobs {
		if m.etype == entityCow {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("returning to the chunk should reload exactly one cow, got %d", got)
	}
	if h.mobstore.has(5, 5) {
		t.Fatal("reloading a chunk must clear its saved bucket")
	}
}

// TestPersistMobFilter: dying mobs, bosses and villagers are never persisted.
func TestPersistMobFilter(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	keep := h.spawnMob(players, entityCow, 0.5, 70, 0.5)
	dying := h.spawnMob(players, entityPig, 1.5, 70, 1.5)
	dying.dying = 20
	villager := h.spawnMob(players, entityVillager, 2.5, 70, 2.5)
	wither := h.spawnMob(players, entityWither, 3.5, 70, 3.5)

	if !h.persistMob(keep) {
		t.Fatal("a plain owned animal should persist")
	}
	if h.persistMob(dying) {
		t.Fatal("a dying mob must not persist")
	}
	if h.persistMob(villager) {
		t.Fatal("villagers are deferred — must not persist")
	}
	if h.persistMob(wither) {
		t.Fatal("bosses must not persist")
	}
}

// TestPetOwnerResolvesOnJoin: a restored pet re-links to its owner's live eid
// when a player with the matching UUID joins.
func TestPetOwnerResolvesOnJoin(t *testing.T) {
	h := newHub(world.New(1))
	uuid := [16]byte{9, 8, 7}
	pet := h.spawnMob(map[int32]*tracked{}, entityWolf, 5.5, 70, 5.5)
	pet.tamed, pet.ownerUUID = true, uuid

	owner := &tracked{p: newPlayer(h.allocEID(), "owner", uuid)}
	h.resolvePetOwners(owner)
	if pet.owner != owner.p.eid {
		t.Fatalf("pet owner not re-resolved: owner=%d want %d", pet.owner, owner.p.eid)
	}

	stranger := &tracked{p: newPlayer(h.allocEID(), "stranger", [16]byte{5, 5, 5})}
	pet2 := h.spawnMob(map[int32]*tracked{}, entityCat, 6.5, 70, 6.5)
	pet2.tamed, pet2.ownerUUID = true, uuid
	h.resolvePetOwners(stranger)
	if pet2.owner != 0 {
		t.Fatal("a non-owner must not adopt a restored pet")
	}
}
