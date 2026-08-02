package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The real hive cycle (#138 M2): a bee pollinates a flower, carries the
// nectar home, waits out its stay inside, and the hive gains a honey level
// as it leaves. Robbing an occupied hive without smoke throws the bees out
// angry, and a sting is the last thing a bee does.

func beeWorld(t *testing.T) (*hub, map[int32]*tracked, blockPos) {
	t.Helper()
	h := newHub(world.New(1))
	h.hivestore = newHiveStore("") // in-memory
	h.hivesLoad()
	players := map[int32]*tracked{}
	nest := blockPos{2000, 200, 2000}
	for dx := -6; dx <= 6; dx++ {
		for dz := -6; dz <= 6; dz++ {
			h.world.SetBlock(nest.x+dx, nest.y-2, nest.z+dz, worldgen.Dirt)
		}
	}
	h.world.SetBlock(nest.x, nest.y, nest.z, worldgen.BlockBase("bee_nest")+6)
	h.registerHive(nest)
	return h, players, nest
}

func TestBeeCycleFillsTheHive(t *testing.T) {
	h, players, nest := beeWorld(t)
	flower := blockPos{nest.x + 4, nest.y - 1, nest.z}
	h.world.SetBlock(flower.x, flower.y, flower.z, worldgen.BlockBase("poppy"))
	m := h.spawnAnimal(players, entityBee, flower.x, flower.z)
	if m == nil {
		t.Fatal("no bee spawned")
	}
	m.x, m.y, m.z = float64(flower.x)+0.5, float64(flower.y)+0.5, float64(flower.z)+0.5
	// Forage: within a few seconds the bee picks the flower and hovers it out.
	for i := 0; i < 60 && !m.beeNectar; i++ {
		h.updateBees(players)
	}
	if !m.beeNectar {
		t.Fatal("the bee never gathered nectar beside a poppy")
	}
	// Home: put it beside the nest and let it enter.
	m.x, m.y, m.z = float64(nest.x)+0.5, float64(nest.y)+0.5, float64(nest.z)+1.5
	m.beeNoEnter = 0
	for i := 0; i < 30 && h.mobs[m.eid] != nil; i++ {
		h.updateBees(players)
	}
	if h.mobs[m.eid] != nil {
		t.Fatal("the bee never entered its hive")
	}
	if len(h.hives[nest]) != 1 || !h.hives[nest][0].Nectar {
		t.Fatalf("hive occupants %v, want one carrying nectar", h.hives[nest])
	}
	// The stay: it leaves with the honey delivered.
	for i := 0; i < beeOccupySecs+5 && len(h.hives[nest]) > 0; i++ {
		h.updateBees(players)
	}
	if len(h.hives[nest]) != 0 {
		t.Fatal("the occupant never finished its stay")
	}
	if lvl := honeyLevel(h.world.At(nest.x, nest.y, nest.z)); lvl != 1 {
		t.Fatalf("hive honey level %d after a nectar delivery, want 1", lvl)
	}
	bees := 0
	for _, mm := range h.mobs {
		if mm.etype == entityBee {
			bees++
		}
	}
	if bees != 1 {
		t.Fatalf("%d bees in the world after the release, want the one", bees)
	}
}

func TestRobbedHiveThrowsAngryBeesOut(t *testing.T) {
	h, players, nest := beeWorld(t)
	h.world.SetBlock(nest.x, nest.y, nest.z, withHoney(worldgen.BlockBase("bee_nest")+6, beeMaxHoney))
	h.hives[nest] = []hiveOccupant{{SecsLeft: 100}, {SecsLeft: 100, Nectar: true}}
	t2 := survPlayer(h)
	players[t2.p.eid] = t2
	t2.x, t2.y, t2.z = float64(nest.x), float64(nest.y), float64(nest.z)+2
	t2.inv.slots[t2.p.heldSlot()] = invStack{item: int32(itemByName["shears"]), count: 1}
	if !h.harvestBeeHome(players, t2, nest) {
		t.Fatal("harvest did nothing on a full hive")
	}
	if len(h.hives[nest]) != 0 {
		t.Fatalf("robbed hive still holds %v", h.hives[nest])
	}
	angry := 0
	for _, m := range h.mobs {
		if m.etype == entityBee && m.anger > 0 {
			angry++
		}
	}
	if angry != 2 {
		t.Fatalf("%d angry bees out of a robbed hive, want 2", angry)
	}
}

func TestStingIsABeesLastAct(t *testing.T) {
	h, players, _ := beeWorld(t)
	m := h.spawnAnimal(players, entityBee, 2010, 2010)
	if m == nil {
		t.Fatal("no bee")
	}
	m.beeStingDie = 2
	h.updateBees(players)
	h.updateBees(players)
	if mm := h.mobs[m.eid]; mm != nil && mm.dying == 0 && mm.health > 0 {
		t.Fatalf("a stung-out bee should be dead or dying (health %d)", mm.health)
	}
}

// Bees court over any flower — the #bee_food tag — and nothing else.
func TestBeesCourtOverFlowers(t *testing.T) {
	h, players, _ := beeWorld(t)
	m := h.spawnAnimal(players, entityBee, 2020, 2020)
	t2 := survPlayer(h)
	players[t2.p.eid] = t2
	t2.inv.slots[t2.p.heldSlot()] = invStack{item: int32(itemByName["wheat"]), count: 1}
	if h.feedAnimal(players, t2, m) {
		t.Fatal("a bee courted over wheat")
	}
	t2.inv.slots[t2.p.heldSlot()] = invStack{item: int32(itemByName["cornflower"]), count: 1}
	if !h.feedAnimal(players, t2, m) {
		t.Fatal("a bee refused a cornflower")
	}
	if m.loveTicks == 0 {
		t.Fatal("the courted bee is not in love")
	}
}
