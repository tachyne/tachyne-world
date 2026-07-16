package server

import (
	"path/filepath"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestMobPersistRoundTrip: mobs written to mobs.json come back with their
// per-instance state (age, health, tamed owner, gear) intact on a fresh hub.
func TestMobPersistRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mobs.json")
	players := map[int32]*tracked{}

	h := newHub(world.New(1))
	h.mobstore = newMobStore(path)

	cow := h.spawnMob(players, entityCow, 10.5, 70, 10.5)
	h.applySpecies(players, cow)
	cow.health, cow.baby, cow.growLeft = 7, true, 500

	wolf := h.spawnMob(players, entityWolf, 20.5, 70, 20.5)
	h.applySpecies(players, wolf)
	wolf.tamed, wolf.sitting = true, true
	wolf.ownerUUID = [16]byte{1, 2, 3, 4}

	zombie := h.spawnHostileY(players, entityZombie, 30.5, 70, 30.5)
	helmet := itemByName["iron_helmet"]
	zombie.gear[0] = invStack{item: helmet, count: 1}

	h.mobstore.recordMobs(h.mobs, h.persistMob)
	h.mobstore.flush()

	// A fresh hub loads them back.
	h2 := newHub(world.New(1))
	h2.mobstore = newMobStore(path)
	h2.loadMobs()

	if len(h2.mobs) != 3 {
		t.Fatalf("restored %d mobs, want 3", len(h2.mobs))
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
	// Fresh eids were minted (not the persisted ones).
	for _, m := range h2.mobs {
		if m.eid == 0 {
			t.Fatal("loaded mob has no eid")
		}
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
		t.Fatal("villagers are deferred — must not persist in v1")
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

	t2 := &tracked{p: newPlayer(h.allocEID(), "owner", uuid)}
	h.resolvePetOwners(t2)
	if pet.owner != t2.p.eid {
		t.Fatalf("pet owner not re-resolved: owner=%d want %d", pet.owner, t2.p.eid)
	}

	// A different player must not adopt the pet.
	other := &tracked{p: newPlayer(h.allocEID(), "stranger", [16]byte{5, 5, 5})}
	pet2 := h.spawnMob(map[int32]*tracked{}, entityCat, 6.5, 70, 6.5)
	pet2.tamed, pet2.ownerUUID = true, uuid
	h.resolvePetOwners(other)
	if pet2.owner != 0 {
		t.Fatal("a non-owner must not adopt a restored pet")
	}
}
