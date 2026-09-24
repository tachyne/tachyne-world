package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func shearFixture(t *testing.T, etype int) (*hub, map[int32]*tracked, *tracked, *mob) {
	t.Helper()
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemShears, count: 1}
	m := h.spawnMob(players, etype, 1.5, 180, 0.5)
	if m == nil {
		t.Fatal("no mob")
	}
	return h, players, pl, m
}

func droppedItem(h *hub, item int32) *itemEntity {
	for _, it := range h.items {
		if it.item == item {
			return it
		}
	}
	return nil
}

// Shears take a pig's saddle off and drop it.
func TestShearsTakeASaddleOff(t *testing.T) {
	h, players, pl, pig := shearFixture(t, entityPig)
	pig.saddled, pig.saddleSt = true, invStack{item: itemSaddle, count: 1}
	if !h.tryShearEquipment(players, pl, pig, false) {
		t.Fatal("the shears did nothing")
	}
	if pig.saddled || droppedItem(h, itemSaddle) == nil {
		t.Fatalf("saddled=%v, dropped saddle=%v", pig.saddled, droppedItem(h, itemSaddle) != nil)
	}
	if h.tryShearEquipment(players, pl, pig, false) {
		t.Error("a bare pig was sheared of something")
	}
}

// Sneaking, or a rider aboard, keeps the gear on.
func TestShearingRefusedWhileSneakingOrRidden(t *testing.T) {
	h, players, pl, pig := shearFixture(t, entityPig)
	pig.saddled, pig.saddleSt = true, invStack{item: itemSaddle, count: 1}
	if h.tryShearEquipment(players, pl, pig, true) {
		t.Error("sneaking sheared the saddle")
	}
	pig.rider = 99
	if h.tryShearEquipment(players, pl, pig, false) {
		t.Error("a ridden pig was sheared")
	}
}

// A horse loses its body armour before its saddle, whole stack and all.
func TestHorseArmourComesOffBeforeTheSaddle(t *testing.T) {
	h, players, pl, horse := shearFixture(t, entityHorse)
	armour := invStack{item: itemByName["diamond_horse_armor"], count: 1, name: "Sparkle"}
	horse.armorSt = armour
	horse.saddled, horse.saddleSt = true, invStack{item: itemSaddle, count: 1}
	h.tryShearEquipment(players, pl, horse, false)
	if horse.armorSt.item != 0 || !horse.saddled {
		t.Fatalf("after one cut: armour %d saddled %v, want armour off and saddle on", horse.armorSt.item, horse.saddled)
	}
	if it := droppedItem(h, armour.item); it == nil || it.name != "Sparkle" {
		t.Fatal("the armour came off without its name")
	}
}

// A tamed wolf's armour comes off for its owner only, and earns the
// advancement's criterion.
func TestWolfArmourShearsForItsOwner(t *testing.T) {
	h, players, pl, wolf := shearFixture(t, entityWolf)
	wolf.tamed, wolf.owner = true, pl.p.eid+1
	wolf.armorSt = invStack{item: itemWolfArmor, count: 1}
	if h.tryShearEquipment(players, pl, wolf, false) {
		t.Fatal("a stranger sheared the wolf's armour")
	}
	wolf.owner = pl.p.eid
	if !h.tryShearEquipment(players, pl, wolf, false) || wolf.armorSt.item != 0 {
		t.Fatal("the owner could not shear the armour off")
	}
	if !(advMatch{entity: "wolf", item: itemWolfArmor}).criterion(critOf(t, "minecraft:husbandry/remove_wolf_armor", "remove_wolf_armor")) {
		t.Error("remove_wolf_armor does not match the shearing")
	}
}
