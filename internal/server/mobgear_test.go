package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

func TestMobEquipment(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	z := h.spawnHostile(players, entityZombie, 0, 0)
	z.x, z.y, z.z = 0.5, 64, 0.5
	baseArmor := z.armorValue()
	sword := int32(itemByName["iron_sword"])
	helm := int32(itemByName["iron_helmet"])
	leather := int32(itemByName["leather_helmet"])
	one := func(item int32) invStack { return invStack{item: item, count: 1} }

	// Weapon → held, boosting melee.
	if h.mobEquipItem(players, z, one(sword)) != 1 || z.held != sword {
		t.Fatal("mob did not wield the sword")
	}
	if mobHeldBonus(z) <= 0 {
		t.Error("held weapon gave no melee bonus")
	}

	// Armour → the right slot, feeding the ARMOR attribute.
	if h.mobEquipItem(players, z, one(helm)) != 1 || z.gear[0].item != helm {
		t.Fatal("mob did not wear the helmet")
	}
	if z.armorValue() <= baseArmor {
		t.Errorf("armour attribute %.1f not raised from %.1f", z.armorValue(), baseArmor)
	}

	// A worse helmet is refused (the hand is full, so it has nowhere to go).
	if h.mobEquipItem(players, z, one(leather)) != 0 {
		t.Error("mob downgraded to a leather helmet")
	}

	// On death it drops the held weapon + worn armour: picked up, so guaranteed.
	if h.gearDropChance(z, 0) != 1 || h.gearDropChance(z, gearSlotHand) != 1 {
		t.Error("picked-up gear should be a guaranteed drop")
	}
	z.health, z.hitByPlayer = 0, true
	itemsBefore := len(h.items)
	h.despawnMob(players, z)
	got := map[int32]bool{}
	for _, it := range h.items {
		got[it.item] = true
	}
	if len(h.items) <= itemsBefore || !got[sword] || !got[helm] {
		t.Errorf("dead mob did not drop its gear (sword=%v helm=%v)", got[sword], got[helm])
	}
}

func TestMobPickupScan(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	z := h.spawnHostile(players, entityZombie, 0, 0)
	z.x, z.y, z.z = 0.5, 64, 0.5
	z.canPickup = true

	// A sword lying where the zombie stands is grabbed by the scan (the drop
	// settles to the floor, so put the zombie there too).
	sword := int32(itemByName["iron_sword"])
	it := h.spawnItem(players, sword, 1, 0.5, 64, 0.5)
	if it == nil {
		t.Fatal("could not drop a test sword")
	}
	it.noPickupUntil = 0
	z.x, z.y, z.z = it.x, it.y, it.z
	h.rules.MobGriefing = false
	h.mobPickupScan(players, z)
	if z.held != 0 {
		t.Error("mob griefing off, yet the zombie picked the sword up")
	}
	h.rules.MobGriefing = true
	h.mobPickupScan(players, z)
	if z.held != sword {
		t.Error("pickup scan did not equip the nearby sword")
	}
	if _, alive := h.items[it.eid]; alive {
		t.Error("picked-up item entity still present")
	}
}

// Vanilla's replacement rules: a skeleton keeps its bow over any sword, a
// drowned its trident; armour that is no upgrade goes into an empty hand;
// an empty hand takes a whole stack of anything and drops it all on death;
// equal weapons are settled by enchantments, then wear.
func TestMobGearReplacementRules(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	one := func(name string) invStack { return invStack{item: int32(itemByName[name]), count: 1} }

	s := h.spawnHostile(players, entitySkeleton, 0, 0)
	s.held, s.heldEnch = int32(itemByName["bow"]), enchList{}
	if h.mobEquipItem(players, s, one("diamond_sword")) != 0 || s.held != int32(itemByName["bow"]) {
		t.Error("a skeleton traded its bow for a sword")
	}
	s.held = int32(itemByName["diamond_sword"])
	if h.mobEquipItem(players, s, one("bow")) != 1 || s.held != int32(itemByName["bow"]) {
		t.Error("a skeleton holding a sword did not take the bow it prefers")
	}

	z := h.spawnHostile(players, entityZombie, 0, 0)
	z.gear[0] = one("iron_helmet")
	if h.mobEquipItem(players, z, one("leather_helmet")) != 1 || z.held != int32(itemByName["leather_helmet"]) || z.gear[0].item != int32(itemByName["iron_helmet"]) {
		t.Errorf("a refused helmet should go into the empty hand: held %d head %d", z.held, z.gear[0].item)
	}
	// Same points, more toughness wins; same everything, more enchantments; then less wear.
	z.gear[0] = one("diamond_helmet")
	if h.mobEquipItem(players, z, one("netherite_helmet")) != 1 || z.gear[0].item != int32(itemByName["netherite_helmet"]) {
		t.Error("netherite over diamond (toughness) refused")
	}
	worn := one("netherite_helmet")
	worn.dmg = 50
	z.gear[0] = worn
	if h.mobEquipItem(players, z, one("netherite_helmet")) != 1 || z.gear[0].dmg != 0 {
		t.Error("a fresh helmet over a worn one refused")
	}
	ench := one("netherite_helmet")
	ench.ench[0] = enchApply{id: enchProtection, lvl: 1}
	if h.mobEquipItem(players, z, ench) != 1 || z.gear[0].enchLvl(enchProtection) != 1 {
		t.Error("an enchanted helmet over a plain one refused")
	}
	bound := one("netherite_helmet")
	bound.ench[0] = enchApply{id: enchBindingCurse, lvl: 1}
	z.gear[0] = bound
	better := one("netherite_helmet")
	better.ench[0] = enchApply{id: enchProtection, lvl: 4}
	better.ench[1] = enchApply{id: enchUnbreaking, lvl: 3}
	if h.mobEquipItem(players, z, better) != 0 || z.gear[0].enchLvl(enchBindingCurse) != 1 {
		t.Error("a bound helmet came off")
	}

	// An empty hand takes the whole stack, and the stack drops on death.
	z2 := h.spawnHostile(players, entityZombie, 0, 0)
	z2.x, z2.y, z2.z = 0.5, 64, 0.5
	sticks := invStack{item: int32(itemByName["stick"]), count: 5}
	if h.mobEquipItem(players, z2, sticks) != 5 || z2.heldCount != 5 || z2.heldStack().count != 5 {
		t.Fatalf("the zombie took %d sticks", z2.heldCount)
	}
	z2.health, z2.hitByPlayer = 0, true
	h.despawnMob(players, z2)
	n := 0
	for _, it := range h.items {
		if it.item == sticks.item {
			n += it.count
		}
	}
	if n != 5 {
		t.Errorf("%d sticks dropped, want 5", n)
	}
}
