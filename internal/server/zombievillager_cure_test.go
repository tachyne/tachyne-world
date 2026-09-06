package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// On Hard a zombie's killing bite turns a villager into a zombie villager
// that keeps its profession, tier and trades; a golden apple under Weakness
// cures it back, the villager returns with everything and owes its curer a
// discount.
func TestZombieVillagerInfectionAndCure(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.rules.Difficulty = diffHard
	v := h.spawnMob(players, entityVillager, 10.5, 200, 10.5)
	h.initVillagerTrades(v, 4) // librarian
	v.tradeLevel, v.tradeXP = 3, 80
	offers := len(v.offers)
	z := h.spawnMob(players, entityZombie, 11.5, 200, 10.5)
	if z == nil || v == nil {
		t.Fatal("spawns")
	}
	z.villagerTarget = v.eid
	v.health = 1
	if !h.zombieBitesVillager(players, z) {
		t.Fatal("the zombie should bite the villager in reach")
	}
	var zv *mob
	for _, m := range h.mobs {
		if m.etype == entityZombieVillager {
			zv = m
		}
	}
	if zv == nil || h.mobs[v.eid] != nil {
		t.Fatal("on Hard the villager should have turned")
	}
	if zv.profession != 4 || zv.tradeLevel != 3 || len(zv.offers) != offers || zv.tradeXP != 80 {
		t.Errorf("the zombie villager must keep the identity: prof=%d tier=%d offers=%d", zv.profession, zv.tradeLevel, len(zv.offers))
	}
	// The cure.
	pl := testTracked()
	players[pl.p.eid] = pl
	pl.x, pl.y, pl.z = zv.x, zv.y, zv.z
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["golden_apple"], count: 2}
	if !h.cureZombieVillager(players, pl, zv) || zv.converting != 0 {
		t.Fatal("without Weakness the apple is refused (interaction consumed, apple kept)")
	}
	h.applyMobEffect(players, zv, effWeakness, 0, 60)
	if !h.cureZombieVillager(players, pl, zv) || zv.converting < cureTimeMin || zv.curer != pl.p.name {
		t.Fatalf("the cure should have started: converting=%d curer=%q", zv.converting, zv.curer)
	}
	if pl.inv.slots[pl.p.heldSlot()].count != 1 {
		t.Error("one golden apple is spent")
	}
	if zv.hasEffect(effWeakness) != 0 || zv.hasEffect(effStrength) == 0 {
		t.Error("Weakness gives way to Strength for the cure")
	}
	for i := 0; i < 400 && h.mobs[zv.eid] != nil; i++ {
		h.tickCure(players, zv)
	}
	if h.mobs[zv.eid] != nil {
		t.Fatal("the cure should have finished")
	}
	var cured *mob
	for _, m := range h.mobs {
		if m.etype == entityVillager {
			cured = m
		}
	}
	if cured == nil {
		t.Fatal("no villager came back")
	}
	if cured.profession != 4 || cured.tradeLevel != 3 || len(cured.offers) != offers {
		t.Errorf("the cured villager keeps the identity: prof=%d tier=%d offers=%d", cured.profession, cured.tradeLevel, len(cured.offers))
	}
	if rep := cured.gossip.reputation(pl.p.name); rep != 20*5+25 {
		t.Errorf("gratitude gossip: reputation %d, want 125", rep)
	}
	if cured.hasEffect(effNausea) == 0 {
		t.Error("a cured villager is queasy")
	}
}

// On Easy the bite just kills; villagers run from a nearby zombie.
func TestZombieBiteOnEasyAndVillagerFlee(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.rules.Difficulty = diffEasy
	v := h.spawnMob(players, entityVillager, 10.5, 200, 10.5)
	z := h.spawnMob(players, entityZombie, 14.5, 200, 10.5)
	h.gridDirty()
	vx, _, fleeing := h.villagerFlee(v)
	if !fleeing || vx >= 0 {
		t.Errorf("the villager should run west, away from the zombie: fleeing=%v vx=%.2f", fleeing, vx)
	}
	z.x = 11.5
	z.villagerTarget = v.eid
	v.health = 1
	h.zombieBitesVillager(players, z)
	if v.dying == 0 {
		t.Error("on Easy the villager dies")
	}
	for _, m := range h.mobs {
		if m.etype == entityZombieVillager {
			t.Error("no infection on Easy")
		}
	}
}
