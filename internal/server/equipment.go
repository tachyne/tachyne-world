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

// broadcastEquipment shows t's current loadout to every other player.
func (h *hub) broadcastEquipment(players map[int32]*tracked, t *tracked) {
	body := equipEv(t.p.eid, heldStack(t), t.offhand, t.armor)
	for _, o := range players {
		if o != t {
			o.p.trySendEv(body)
		}
	}
}
