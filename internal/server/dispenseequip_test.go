package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// equipRig is breezeRig with an east-facing dispenser at (0,180,0): the cell
// ahead is (1,180,0). The rig's player is moved out of the way.
func equipRig(t *testing.T) (*hub, map[int32]*tracked, *tracked, func(dim int, st invStack) *invStack) {
	t.Helper()
	h, players, pl := breezeRig(t)
	pl.x, pl.z = -6.5, -6.5
	state := eastDispenser(t)
	pos := blockPos{0, 180, 0}
	fire := func(dim int, st invStack) *invStack {
		h.world.SetBlock(pos.x, pos.y, pos.z, state)
		b := &bin{slots: make([]invStack, 9)}
		b.slots[0] = st
		sp := simPos{dim: dim, blockPos: pos}
		h.bins[sp] = b
		h.inDim(dim, func() { h.ejectFromBin(players, sp, state) })
		return &b.slots[0]
	}
	return h, players, pl, fire
}

// groundItem finds the one item entity lying about, if any.
func groundItem(h *hub) *itemEntity {
	for _, it := range h.items {
		return it
	}
	return nil
}

// The armour-stand search runs in the dispenser's own dimension: a Nether
// dispenser dresses a Nether stand.
func TestDispenserDressesStandInNether(t *testing.T) {
	h, players, _, fire := equipRig(t)
	sd := &armorStand{eid: h.allocEID(), dim: dimNether, x: 1.5, y: 180, z: 0.5}
	h.armorStands[sd.eid] = sd
	helmet := int32(itemByName["iron_helmet"])
	s := fire(dimNether, invStack{item: helmet, count: 2})
	if sd.equip[wearSlotToStand[0]].item != helmet {
		t.Fatal("a Nether dispenser did not put the helmet on the Nether stand")
	}
	if s.count != 1 || len(h.items) != 0 {
		t.Errorf("one helmet should be worn and none tossed: %d left, %d on the ground", s.count, len(h.items))
	}
	_ = players
}

// EquipmentDispenseItemBehavior equips any LivingEntity: a player standing
// in the cell gets the chestplate in an empty slot.
func TestDispenserEquipsPlayer(t *testing.T) {
	h, _, pl, fire := equipRig(t)
	pl.x, pl.z = 1.5, 0.5
	chest := int32(itemByName["diamond_chestplate"])
	fire(dimOverworld, invStack{item: chest, count: 1})
	if pl.armor[1].item != chest {
		t.Fatalf("the player in front was not given the chestplate: %+v", pl.armor)
	}
	// A full slot is passed over: the next one is tossed.
	fire(dimOverworld, invStack{item: chest, count: 1})
	if it := groundItem(h); it == nil || it.item != chest {
		t.Error("a chestplate for an occupied slot was not tossed")
	}
}

// A saddle goes on an adult pig; a piglet cannot wear one, so it is tossed.
func TestDispenserSaddlesPig(t *testing.T) {
	h, players, _, fire := equipRig(t)
	pig := h.spawnMob(players, entityPig, 1.5, 180, 0.5)
	fire(dimOverworld, invStack{item: itemSaddle, count: 1})
	if !pig.saddled {
		t.Fatal("the adult pig in front was not saddled")
	}
	pig.saddled, pig.baby = false, true
	fire(dimOverworld, invStack{item: itemSaddle, count: 1})
	if pig.saddled {
		t.Error("a piglet was saddled")
	}
	if it := groundItem(h); it == nil || it.item != itemSaddle {
		t.Error("the saddle nothing could wear was not tossed")
	}
}

// A zombie that may pick up loot is dressed; one that may not is left bare
// (Mob.canDispenserEquipIntoSlot is canPickUpLoot).
func TestDispenserDressesLootingZombieOnly(t *testing.T) {
	h, players, _, fire := equipRig(t)
	z := h.spawnMob(players, entityZombie, 1.5, 180, 0.5)
	z.canPickup = false
	z.gear = [4]invStack{}
	boots := int32(itemByName["iron_boots"])
	fire(dimOverworld, invStack{item: boots, count: 1})
	if z.gear[3].item != 0 {
		t.Fatal("a zombie that cannot pick up loot was given boots")
	}
	for id := range h.items {
		delete(h.items, id)
	}
	z.canPickup = true
	fire(dimOverworld, invStack{item: boots, count: 1})
	if z.gear[3].item != boots || !z.gearSure[3] || !z.persistent {
		t.Fatalf("a looting zombie was not dressed as vanilla does: gear %+v sure %v persistent %v", z.gear, z.gearSure, z.persistent)
	}
}

// A fallback toss carries the whole stack's data: a water bottle that
// misses mud-able ground lands as a water bottle, dyed and named armour
// keeps its colour and name.
func TestDispenserTossKeepsData(t *testing.T) {
	h, _, _, fire := equipRig(t)
	fire(dimOverworld, potionStack(potWater))
	if it := groundItem(h); it == nil || it.item != itemPotion || it.potion != potWater {
		t.Fatalf("a tossed water bottle lost its potion: %+v", it)
	}
	for id := range h.items {
		delete(h.items, id)
	}
	cap := int32(itemByName["leather_helmet"])
	fire(dimOverworld, invStack{item: cap, count: 1, color: 0x123456, name: "Lid"})
	if it := groundItem(h); it == nil || it.color != 0x123456 || it.name != "Lid" {
		t.Fatalf("tossed dyed, named armour came out plain: %+v", it)
	}
}

// ItemStack.hurtAndBreak: Unbreaking spares a dispensed flint and steel's
// wear as it spares a held one.
func TestDispensedFlintHonoursUnbreaking(t *testing.T) {
	_, _, _, fire := equipRig(t)
	const shots = 40
	plain, charmed := 0, 0
	for i := 0; i < shots; i++ {
		plain += fire(dimOverworld, invStack{item: itemFlintSteel, count: 1}).dmg
		charmed += fire(dimOverworld, invStack{item: itemFlintSteel, count: 1,
			ench: enchList{{id: enchUnbreaking, lvl: 3}}}).dmg
	}
	if plain != shots {
		t.Fatalf("a plain flint and steel wore %d over %d shots, want one each", plain, shots)
	}
	if charmed >= shots*3/4 {
		t.Errorf("Unbreaking III flint and steel wore %d over %d shots; expected about a quarter", charmed, shots)
	}
}

// Flint and steel primes TNT in front of the dispenser.
func TestDispensedFlintPrimesTNT(t *testing.T) {
	h, _, _, fire := equipRig(t)
	h.rules.TNTExplodes = true
	h.world.SetBlock(1, 180, 0, worldgen.BlockBase("tnt"))
	before := len(h.tnt)
	fire(dimOverworld, invStack{item: itemFlintSteel, count: 1})
	if len(h.tnt) != before+1 || h.world.Block(1, 180, 0) != worldgen.Air {
		t.Fatal("a dispensed flint and steel did not prime the TNT in front")
	}
}
