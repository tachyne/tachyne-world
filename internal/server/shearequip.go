package server

import attachproto "github.com/tachyne/tachyne-common/attach"

// Shears take a mob's gear off (26.x): Entity.interact tries it before the
// species' own interaction, and Mob.attemptToShearEquipment walks the slots
// in EquipmentSlot order, so the body slot (armour, carpet, harness) comes
// off before the saddle. Only equipment marked can_be_sheared qualifies:
// wolf, horse and nautilus armour, llama carpets, happy-ghast harnesses
// and saddles. A mob carrying anyone refuses (canShearEquipment), except a
// wolf, which answers only to its owner. Curse of Binding holds a piece on
// against anyone not in creative. Until 2026-09-24 only a wolf's armour and
// a ghast's harness could come off, and neither raised the advancement
// trigger (husbandry/remove_wolf_armor).

// shearSoundFor is the piece's Equippable shearing sound.
func shearSoundFor(item int32) string {
	switch {
	case item == itemWolfArmor:
		return "minecraft:item.armor.unequip_wolf"
	case horseArmorItems[item]:
		return "minecraft:item.horse_armor.unequip"
	case nautilusArmorItems[item]:
		return "minecraft:item.armor.unequip_nautilus"
	case carpetItems[item]:
		return "minecraft:item.llama_carpet.unequip"
	case isHarness(item):
		return "minecraft:entity.happy_ghast.unequip"
	}
	return "minecraft:item.saddle.unequip"
}

// tryShearEquipment is Entity.interact's shears step. Returns true when the
// click was spent here.
func (h *hub) tryShearEquipment(players map[int32]*tracked, t *tracked, m *mob, sneak bool) bool {
	if heldStack(t).item != itemShears || sneak || m.dying > 0 {
		return false
	}
	if m.etype == entityWolf {
		if !m.tamed || m.owner != t.p.eid { // Wolf.canShearEquipment: its owner only
			return false
		}
	} else if m.rider != 0 || len(m.riders) > 0 || m.mobRider != 0 { // Mob.canShearEquipment: !isVehicle
		return false
	}
	creative := t.gamemode == gmCreative
	var piece invStack
	switch {
	case m.harness != 0:
		piece = invStack{item: m.harness, count: 1}
		m.harness = 0
		h.toTracking(players, m.eid, m.dim, m.x, m.z, ghastHarnessEquip(m.eid, 0))
	case m.armorSt.item != 0 && (creative || m.armorSt.enchLvl(enchBindingCurse) == 0):
		piece = m.armorSt
		m.armorSt = invStack{}
		if m.etype == entityWolf {
			m.refreshGearArmor()
			h.petEquipSync(players, m)
		} else {
			h.horseEquipSync(players, m)
		}
	case m.saddled:
		piece = m.saddleSt
		if piece.item == 0 {
			piece = invStack{item: itemSaddle, count: 1}
		}
		m.saddled, m.saddleSt = false, invStack{}
		if horseFamily(m.etype) {
			h.horseEquipSync(players, m)
		} else {
			var eq attachproto.Equipment
			eq.EID = m.eid
			eq.SendSaddle = true // an empty saddle slot
			h.toTracking(players, m.eid, m.dim, m.x, m.z, eq)
		}
	default:
		return false
	}
	if isSurvival(t.gamemode) {
		h.applyToolWear(t, t.p.heldSlot(), 1)
	}
	h.vibAt(m.dim, freqShear, m.x, m.y, m.z, t.p.eid)
	if it := h.spawnItemIn(players, m.dim, piece.item, piece.count, m.x, m.y+0.5, m.z); it != nil {
		it.setFrom(piece)
		h.refreshItemMeta(players, it)
	}
	h.playSoundDim(players, m.dim, shearSoundFor(piece.item), sndNeutral, m.x, m.y, m.z, 1, 1)
	h.advance(players, t, "player_sheared_equipment", advMatch{entity: advEntityName[m.etype], item: piece.item})
	return true
}
