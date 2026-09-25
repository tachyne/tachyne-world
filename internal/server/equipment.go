package server

import attachproto "github.com/tachyne/tachyne-common/attach"

// Equipment visuals: set_equipment (0x5f) shows a player's worn armor and
// held item on their entity, so OTHER players see the iron chestplate and
// the sword in hand (your own client renders yours from the inventory
// window). Sent on join (both directions), whenever armor/held items change,
// and re-broadcast with the 2s absolute resync — one-shot trySends can drop,
// and equipment isn't otherwise resent (same lesson as entity positions).

const (
	equipMainHand = 0
	equipOffhand  = 1
	equipFeet     = 2
	equipLegs     = 3
	equipChest    = 4
	equipHead     = 5
)

// evHeldChange: the connection's selected hotbar slot moved — what's "in
// hand" changed even though no inventory slot did.
type evHeldChange struct{ eid int32 }

func (evHeldChange) isHubEvent() {}

// equipEv snapshots an entity's full loadout as a domain event. Empty slots
// ride along too — that's what clears a piece the viewer previously saw.
func equipEv(eid int32, main, off invStack, armor [4]invStack) attachproto.Equipment {
	var e attachproto.Equipment
	e.EID = eid
	e.Slots[attachproto.EquipMainHand] = stackEv(main)
	e.Slots[attachproto.EquipOffhand] = stackEv(off)
	e.Slots[attachproto.EquipFeet] = stackEv(armor[3])
	e.Slots[attachproto.EquipLegs] = stackEv(armor[2])
	e.Slots[attachproto.EquipChest] = stackEv(armor[1])
	e.Slots[attachproto.EquipHead] = stackEv(armor[0])
	return e
}

// heldStack is what the player currently holds in hand (hub-side view).
func heldStack(t *tracked) invStack {
	if t.inv == nil {
		return invStack{}
	}
	return t.inv.slots[t.p.heldSlot()]
}

// useSlot is the slot of the hand the latest item use came from: the
// offhand when the client used OFF_HAND, else the selected hotbar slot.
func (t *tracked) useSlot() int {
	if t.useOffhand {
		return offhandSlot
	}
	return t.p.heldSlot()
}

// usedStack is LivingEntity.getItemInHand(usedHand): the stack in the hand
// the latest use came from.
func usedStack(t *tracked) invStack {
	if s := t.handStack(t.useSlot()); s != nil {
		return *s
	}
	return invStack{}
}

// holdsLike is Inventory.contains(stack): any slot, armour and offhand
// included, holding the same item with the same data.
func (t *tracked) holdsLike(st invStack) bool {
	if t.inv == nil {
		return false
	}
	for _, s := range t.inv.slots {
		if s.count > 0 && sameItemComponents(s, st) {
			return true
		}
	}
	for _, s := range t.armor {
		if s.count > 0 && sameItemComponents(s, st) {
			return true
		}
	}
	return t.offhand.count > 0 && sameItemComponents(t.offhand, st)
}

// consumeUsed takes one from the hand the latest use came from
// (ItemStack.consume on the used hand's stack).
func (h *hub) consumeUsed(t *tracked) {
	slot := t.useSlot()
	if s := t.handStack(slot); s != nil && s.count > 0 {
		if s.count--; s.count == 0 {
			*s = invStack{}
		}
		h.sendHandSlot(t, slot)
	}
}

// broadcastEquipment shows t's current loadout to every other player.
func (h *hub) broadcastEquipment(players map[int32]*tracked, t *tracked) {
	body := equipEv(t.p.eid, heldStack(t), t.offhand, t.armor)
	for _, o := range players {
		if o != t {
			o.p.trySendEv(body)
		}
	}
}

// evSwapHands is the F key: swap the held hotbar slot with the off-hand.
type evSwapHands struct{ eid int32 }

func (evSwapHands) isHubEvent() {}

// onSwapHands is ServerboundPlayerActionPacket's SWAP_ITEM_WITH_OFFHAND.
// Vanilla swaps the two stacks whole — enchantments, damage and all — and
// refuses while the player is using an item.
func (h *hub) onSwapHands(players map[int32]*tracked, e evSwapHands) {
	t := players[e.eid]
	if t == nil || t.inv == nil || t.dead || t.gamemode == gmSpectator {
		return
	}
	slot := t.p.heldSlot()
	t.inv.slots[slot], t.offhand = t.offhand, t.inv.slots[slot]
	h.sendSlot(t, slot)
	h.sendOffhand(t)
	h.broadcastEquipment(players, t)
}
