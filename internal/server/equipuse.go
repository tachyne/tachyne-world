package server

import (
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Right-clicking with armour in hand puts it on (Equippable.
// swapWithEquipmentSlot, reached from Item.use for every swappable
// equippable — the armour sets and the elytra; the carved pumpkin and the
// heads are not swappable and stay in the hand). What was worn comes back
// to the hand when the held stack was a single item, else into the
// inventory (or onto the ground); a worn piece under the binding curse
// stays put unless the player is in creative, and the very same item
// changes nothing.

type evEquipHeld struct{ eid int32 }

func (evEquipHeld) isHubEvent() {}

// equipSlotOnUse is the armour slot a held item goes to on use, -1 if none.
func equipSlotOnUse(item int32) int {
	if item == 0 {
		return -1
	}
	if ap, ok := armorInfo[item]; ok {
		return ap.Slot
	}
	if item == int32(itemElytra) {
		return 1 // chest
	}
	return -1
}

// equipSound is the piece's material sound (item.armor.equip_<material>).
func equipSound(item int32) string {
	name := itemNameOf[item]
	switch {
	case name == "elytra":
		return "minecraft:item.armor.equip_elytra"
	case name == "turtle_helmet":
		return "minecraft:item.armor.equip_turtle"
	case strings.HasPrefix(name, "leather_"):
		return "minecraft:item.armor.equip_leather"
	case strings.HasPrefix(name, "chainmail_"):
		return "minecraft:item.armor.equip_chain"
	case strings.HasPrefix(name, "iron_"):
		return "minecraft:item.armor.equip_iron"
	case strings.HasPrefix(name, "golden_"):
		return "minecraft:item.armor.equip_gold"
	case strings.HasPrefix(name, "diamond_"):
		return "minecraft:item.armor.equip_diamond"
	case strings.HasPrefix(name, "netherite_"):
		return "minecraft:item.armor.equip_netherite"
	}
	return "minecraft:item.armor.equip_generic"
}

func (h *hub) onEquipHeld(players map[int32]*tracked, e evEquipHeld) {
	t := players[e.eid]
	if t == nil || t.dead || t.inv == nil {
		return
	}
	logical := t.p.heldSlot()
	held := t.inv.slots[logical]
	slot := equipSlotOnUse(held.item)
	if slot < 0 || held.count <= 0 {
		return
	}
	worn := t.armor[slot]
	if worn.item != 0 && worn.enchLvl(enchBindingCurse) > 0 && t.gamemode != gmCreative {
		return
	}
	if worn == held {
		return // ItemStack.isSameItemSameComponents
	}
	h.incStat(t, attachproto.StatUsed, held.item, 1)
	one := held
	one.count = 1
	if held.count <= 1 {
		t.armor[slot] = one
		if t.gamemode == gmCreative {
			t.inv.slots[logical] = held // creative keeps the hand's copy
		} else {
			t.inv.slots[logical] = worn
		}
	} else {
		t.armor[slot] = one
		if t.gamemode != gmCreative {
			held.count--
			t.inv.slots[logical] = held
		}
		if worn.item != 0 {
			changed, leftover := t.inv.addStack(worn)
			for _, s := range changed {
				h.sendSlot(t, s)
			}
			if leftover > 0 {
				h.spawnItem(players, worn.item, leftover, t.x, t.y, t.z)
			}
		}
	}
	h.sendSlot(t, logical)
	t.inv.stateId++
	t.p.trySendEv(attachproto.WindowSlot{ID: 0, StateID: t.inv.stateId, Slot: int32(5 + slot), Item: stackEv(t.armor[slot])})
	t.refreshArmorAttrs()
	h.broadcastEquipment(players, t)
	h.playSound(players, equipSound(one.item), sndPlayer, t.x, t.y, t.z, 1, 1)
}
