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
	if !h.persistMob(villager) {
		t.Fatal("villagers persist as of v2.1")
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

// TestVillagerPersistRoundTrip: a leveled merchant comes back with its
// profession, tier, XP, exact offer list (with uses) and schedule anchors —
// and with the villager stance (behavior/doors/speed) reapplied.
func TestVillagerPersistRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mobs.json")
	players := map[int32]*tracked{}

	h := newHub(world.New(1))
	h.mobstore = newMobStore(path)
	v := h.spawnMob(players, entityVillager, 10.5, 70, 10.5)
	h.initVillagerTrades(v, 4) // librarian
	h.awardTradeXP(v, 15)      // novice → apprentice, unlocks tier-2 stock
	v.offers[0].uses = 3
	wantOffers := append([]mobOffer(nil), v.offers...)
	v.home, v.bed, v.work, v.meet = blockPos{10, 70, 10}, blockPos{9, 70, 10}, blockPos{11, 70, 9}, blockPos{40, 70, 40}

	active := map[[2]int32]bool{{0, 0}: true}
	h.activeChunks = active
	h.mobstore.bucketLive(h.mobs, h.persistMob, active)
	h.mobstore.flush()

	h2 := newHub(world.New(1))
	h2.mobstore = newMobStore(path)
	h2.reconcileMobChunks(players, map[[2]int32]bool{{0, 0}: true})

	var got *mob
	for _, m := range h2.mobs {
		if m.etype == entityVillager {
			got = m
		}
	}
	if got == nil {
		t.Fatal("villager did not reload")
	}
	if got.profession != 4 || got.tradeLevel != 2 || got.tradeXP != 15 {
		t.Fatalf("merchant identity lost: prof=%d tier=%d xp=%d", got.profession, got.tradeLevel, got.tradeXP)
	}
	if len(got.offers) != len(wantOffers) {
		t.Fatalf("offer count changed: %d, want %d", len(got.offers), len(wantOffers))
	}
	for i := range wantOffers {
		if got.offers[i] != wantOffers[i] {
			t.Fatalf("offer %d changed: %+v, want %+v", i, got.offers[i], wantOffers[i])
		}
	}
	if _, ok := got.behavior.(villagerBehavior); !ok {
		t.Fatalf("villager stance lost: behavior=%T", got.behavior)
	}
	if !got.usesDoors || got.speed != 0.135 {
		t.Fatalf("villager movement lost: doors=%v speed=%v", got.usesDoors, got.speed)
	}
	if got.bed != (blockPos{9, 70, 10}) || got.work != (blockPos{11, 70, 9}) || got.meet != (blockPos{40, 70, 40}) {
		t.Fatalf("schedule anchors lost: bed=%v work=%v meet=%v", got.bed, got.work, got.meet)
	}
}

// TestPreV21VillagerRowGetsStock: a saved villager row with no offers (or a
// villager never dealt stock) reloads with tier-1 trades instead of an empty
// merchant screen.
func TestPreV21VillagerRowGetsStock(t *testing.T) {
	players := map[int32]*tracked{}
	h := newHub(world.New(1))
	sm := savedMob{Etype: entityVillager, X: 10.5, Y: 70, Z: 10.5, Health: 20, Profession: 2}
	m := h.reloadMob(players, &sm)
	if m == nil {
		t.Fatal("villager did not reload")
	}
	if m.tradeLevel != 1 || len(m.offers) == 0 {
		t.Fatalf("legacy villager should deal tier-1 stock: tier=%d offers=%d", m.tradeLevel, len(m.offers))
	}
}

// TestGolemReloadKeepsGuardianStance: a reloaded iron golem is still the
// village guardian (behavior + knockback immunity + home anchor).
func TestGolemReloadKeepsGuardianStance(t *testing.T) {
	players := map[int32]*tracked{}
	h := newHub(world.New(1))
	sm := savedMob{Etype: entityIronGolem, X: 10.5, Y: 70, Z: 10.5, Health: 80, Home: [3]int{12, 70, 12}}
	m := h.reloadMob(players, &sm)
	if m == nil {
		t.Fatal("golem did not reload")
	}
	if _, ok := m.behavior.(golemBehavior); !ok {
		t.Fatalf("guardian stance lost: behavior=%T", m.behavior)
	}
	if !m.noKB || m.home != (blockPos{12, 70, 12}) {
		t.Fatalf("golem attributes lost: noKB=%v home=%v", m.noKB, m.home)
	}
}

// TestNPCMobNotPersisted: an LLM NPC's villager body must never enter
// mobs.json — the npc registry owns it.
func TestNPCMobNotPersisted(t *testing.T) {
	players := map[int32]*tracked{}
	h := newHub(world.New(1))
	n := h.spawnNPC(players, "Testy", "a test persona", 10.5, 10.5)
	if n == nil {
		t.Fatal("npc spawn failed")
	}
	if h.persistMob(n.mob) {
		t.Fatal("NPC-backed mob must not persist")
	}
	v := h.spawnMob(players, entityVillager, 12.5, 70, 12.5)
	if !h.persistMob(v) {
		t.Fatal("an ordinary villager must persist now")
	}
}

// TestVillageMarkersPersist: populated villages survive a store round trip so
// a restart does not spawn a second population.
func TestVillageMarkersPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mobs.json")
	s := newMobStore(path)
	s.recordVillages(map[blockPos]bool{{100, 64, -200}: true})
	s.flush()
	s2 := newMobStore(path)
	vs := s2.villages()
	if len(vs) != 1 || unpackPos(vs[0]) != (blockPos{100, 64, -200}) {
		t.Fatalf("village markers lost: %v", vs)
	}
}

// TestCullAnimals verifies the one-time density cull: species capped per chunk,
// cows thinned to a fraction of chunks, pets and villagers always kept.
func TestCullAnimals(t *testing.T) {
	s := newMobStore("")
	// A dense chunk: 20 cows, 2 sheep, a tamed wolf, a villager.
	var dense []savedMob
	for i := 0; i < 20; i++ {
		dense = append(dense, savedMob{Etype: entityCow, X: float64(i), Y: 70, Z: 0})
	}
	dense = append(dense,
		savedMob{Etype: entitySheep, X: 1, Y: 70, Z: 1},
		savedMob{Etype: entitySheep, X: 2, Y: 70, Z: 1},
		savedMob{Etype: entityWolf, X: 3, Y: 70, Z: 1, Tamed: true, OwnerUUID: "abcd"},
		savedMob{Etype: entityVillager, X: 4, Y: 70, Z: 1},
	)
	// Fill several chunks with cows so coverage-thinning has something to thin.
	s.m.Chunks = map[string][]savedMob{}
	for cx := 0; cx < 10; cx++ {
		key := mobChunkKey(int32(cx), 0)
		var b []savedMob
		for i := 0; i < 8; i++ {
			b = append(b, savedMob{Etype: entityCow, X: float64(cx*16 + i), Y: 70, Z: 0})
		}
		s.m.Chunks[key] = b
	}
	s.m.Chunks[mobChunkKey(0, 0)] = dense // overwrite chunk 0 with the mixed dense one

	before, after := s.cullAnimals(4, 5)
	if after >= before {
		t.Fatalf("cull should reduce mobs: %d -> %d", before, after)
	}

	// Chunk 0 keeps cows (0%5==0): ≤4 cows, both sheep, the pet, the villager.
	c0 := s.m.Chunks[mobChunkKey(0, 0)]
	cows, sheep, wolf, vill := 0, 0, 0, 0
	for _, m := range c0 {
		switch m.Etype {
		case entityCow:
			cows++
		case entitySheep:
			sheep++
		case entityWolf:
			wolf++
		case entityVillager:
			vill++
		}
	}
	if cows != 4 {
		t.Fatalf("dense chunk should cap cows at 4, got %d", cows)
	}
	if sheep != 2 || wolf != 1 || vill != 1 {
		t.Fatalf("cull must keep sheep(2)/pet(1)/villager(1), got %d/%d/%d", sheep, wolf, vill)
	}

	// Cow coverage thinned: not every chunk still has cows.
	chunksWithCows := 0
	for _, b := range s.m.Chunks {
		for _, m := range b {
			if m.Etype == entityCow {
				chunksWithCows++
				break
			}
		}
	}
	if chunksWithCows >= 10 {
		t.Fatalf("cow coverage should thin below 10 chunks, still %d", chunksWithCows)
	}

	// Idempotent: a second pass changes nothing more.
	b2, a2 := s.cullAnimals(4, 5)
	if b2 != a2 {
		t.Fatalf("second cull pass should be a no-op, %d -> %d", b2, a2)
	}
}
