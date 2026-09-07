package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Totem of undying (vanilla LivingEntity.checkTotemDeathProtection +
// DeathProtection.TOTEM_OF_UNDYING): a killing blow that does not bypass
// invulnerability is answered by a totem in either hand — the totem is
// spent, health is set to one, every effect is cleared, and Regeneration
// II for 45 s, Absorption II for 5 s and Fire Resistance for 40 s take
// over, with the totem animation for everyone watching.

const entityStatusTotem = 35

var itemTotem = itemByName["totem_of_undying"]

// totemSaves reports whether a totem answered a lethal hit, applying it.
func (h *hub) totemSaves(players map[int32]*tracked, t *tracked, dt dmgType) bool {
	if dmgTypeTags[dt]&tagBypassesInvulnerability != 0 || t.inv == nil {
		return false
	}
	held := &t.inv.slots[t.p.heldSlot()]
	var used *invStack
	switch {
	case held.item == itemTotem && held.count > 0:
		used = held
	case t.offhand.item == itemTotem && t.offhand.count > 0:
		used = &t.offhand
	default:
		return false
	}
	if used.count--; used.count <= 0 {
		*used = invStack{}
	}
	if used == held {
		h.sendSlot(t, t.p.heldSlot())
	} else {
		h.sendOffhand(t)
	}
	h.incStat(t, attachproto.StatUsed, itemTotem, 1)
	h.advance(players, t, "used_totem", advMatch{item: itemTotem})
	t.health = 1
	h.clearEffects(t)
	h.applyEffect(players, t, effRegen, 1, 45)
	h.applyEffect(players, t, effAbsorption, 1, 5)
	h.applyEffect(players, t, effFireRes, 0, 40)
	h.sendHealth(t)
	h.toNearbyEv(players, t.dim, t.x, t.z, attachproto.EntityStatus{EID: t.p.eid, Status: entityStatusTotem})
	t.p.trySendEv(attachproto.EntityStatus{EID: t.p.eid, Status: entityStatusTotem})
	return true
}

// sensorWithinEarshot reports whether any sculk sensor could hear a
// vibration from (x,y,z) — the condition for the "avoid vibration" award.
func (h *hub) sensorWithinEarshot(x, y, z int) bool {
	for pos := range h.sculkList {
		s := h.world.At(pos.x, pos.y, pos.z)
		if !isAnySensor(s) {
			continue
		}
		r := float64(sensorRadius(s))
		dx, dy, dz := float64(x-pos.x), float64(y-pos.y), float64(z-pos.z)
		if dx*dx+dy*dy+dz*dz <= r*r {
			return true
		}
	}
	return false
}

// sendOffhand refreshes the off-hand slot on the client and, since the hand
// contents show on the model, re-broadcasts the loadout.
func (h *hub) sendOffhand(t *tracked) {
	t.inv.stateId++
	t.p.trySendEv(attachproto.WindowSlot{ID: 0, StateID: t.inv.stateId, Slot: offhandWindowSlot, Item: stackEv(t.offhand)})
	h.broadcastEquipment(h.playersRef, t)
}
