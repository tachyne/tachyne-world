package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A charged creeper's blast leaves whatever it killed's head behind — the
// only way to a mob head in survival.
func TestChargedCreeperDropsHeads(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.rules.DoMobLoot = true
	victim := h.spawnMob(players, entitySkeleton, 1.5, 70, 0.5)
	victim.health = 1
	c := h.spawnMob(players, entityCreeper, 0.5, 70, 0.5)
	c.charged = true

	h.explodeCreeper(players, c)
	found := false
	for _, it := range h.items {
		if it.item == itemByName["skeleton_skull"] {
			found = true
		}
	}
	if !found {
		t.Fatal("a charged creeper's blast should leave a skeleton skull")
	}
	if h.blastChargedCreeper {
		t.Error("the flag must not outlive the blast")
	}
	// An ordinary creeper leaves nothing of the sort.
	for eid := range h.items {
		delete(h.items, eid)
	}
	v2 := h.spawnMob(players, entityZombie, 1.5, 70, 0.5)
	v2.health = 1
	plain := h.spawnMob(players, entityCreeper, 0.5, 70, 0.5)
	h.explodeCreeper(players, plain)
	for _, it := range h.items {
		if it.item == itemByName["zombie_head"] {
			t.Fatal("an ordinary creeper drops no heads")
		}
	}
	// A head only comes from the species that have one.
	if chargedHeadFor(entityCow) != 0 {
		t.Error("a cow has no head to drop")
	}
}

// A turtle that grows up leaves a scute — the only source in the game.
func TestGrownTurtleDropsScute(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityTurtle, 0.5, 70, 0.5)
	m.baby, m.growLeft = true, survivalTickN
	h.updateBreeding(players)
	if m.baby {
		t.Fatal("the hatchling should have grown up")
	}
	found := false
	for _, it := range h.items {
		if it.item == itemByName["turtle_scute"] {
			found = true
		}
	}
	if !found {
		t.Error("growing up leaves a scute behind")
	}
}
