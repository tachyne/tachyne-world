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
	baseArmor := z.armor
	sword := int32(itemByName["iron_sword"])
	helm := int32(itemByName["iron_helmet"])
	leather := int32(itemByName["leather_helmet"])

	// Weapon → held, boosting melee.
	if !h.mobEquipItem(players, z, sword) || z.held != sword {
		t.Fatal("mob did not wield the sword")
	}
	if mobHeldBonus(z) <= 0 {
		t.Error("held weapon gave no melee bonus")
	}

	// Armour → the right slot, feeding the ARMOR attribute.
	if !h.mobEquipItem(players, z, helm) || z.gear[0].item != helm {
		t.Fatal("mob did not wear the helmet")
	}
	if z.armor <= baseArmor {
		t.Errorf("armour attribute %.1f not raised from %.1f", z.armor, baseArmor)
	}

	// A worse helmet is refused.
	if h.mobEquipItem(players, z, leather) {
		t.Error("mob downgraded to a leather helmet")
	}

	// On death it drops the held weapon + worn armour.
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
	z.x, z.y, z.z = it.x, it.y, it.z
	h.mobPickupScan(players, z)
	if z.held != sword {
		t.Error("pickup scan did not equip the nearby sword")
	}
	if _, alive := h.items[it.eid]; alive {
		t.Error("picked-up item entity still present")
	}
}
