package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// Wolf armour (vanilla Wolf.mobInteract / Wolf.actuallyHurt, 1.20.5+): the
// owner straps armadillo-scute armour onto a grown tamed wolf, shears take
// it off again (dropping it), a scute repairs an eighth of it while the
// wolf sits, and while it is worn every blow that does not bypass wolf
// armour goes into the armour's durability instead of the wolf — cracking
// audibly as it wears, breaking when it is spent.

const (
	wolfArmorPoints   = 11 // ArmorMaterials.ARMADILLO_SCUTE body defence
	wolfArmorMaxDmg   = 64 // 4 × ArmorType.BODY's 16
	wolfArmorRepairPt = 0.125
)

var (
	itemWolfArmor      = int32(itemByName["wolf_armor"])
	itemArmadilloScute = int32(itemByName["armadillo_scute"])
)

func wolfArmorMax() int {
	if max := itemMaxDurability[itemWolfArmor]; max > 0 {
		return max
	}
	return wolfArmorMaxDmg
}

// crackLevel is Crackiness.WOLF_ARMOR.byDamage: 0 none, 1 low, 2 medium, 3 high.
func crackLevel(dmg, max int) int {
	f := 1 - float64(dmg)/float64(max)
	switch {
	case f < 0.32:
		return 3
	case f < 0.69:
		return 2
	case f < 0.95:
		return 1
	}
	return 0
}

// wolfArmorAbsorb is the armour taking a blow: ceil(damage) durability,
// breaking at the end. Runs on the mob (no hub); the note is played later.
func (m *mob) wolfArmorAbsorb(dmg float64) {
	max := wolfArmorMax()
	before := crackLevel(m.armorSt.dmg, max)
	m.armorSt.dmg += int(math.Ceil(dmg))
	if m.armorSt.dmg >= max {
		m.armorSt = invStack{}
		m.armorNote = 2
		m.refreshGearArmor()
		return
	}
	if crackLevel(m.armorSt.dmg, max) != before {
		m.armorNote = 1
	} else {
		m.armorNote = 3 // plain damage sound, no new crack
	}
}

// wolfArmorNote plays what the last absorbed blow did and re-shows the armour.
func (h *hub) wolfArmorNote(players map[int32]*tracked, m *mob) {
	note := m.armorNote
	m.armorNote = 0
	snd := "minecraft:item.wolf_armor.damage"
	switch note {
	case 1:
		snd = "minecraft:item.wolf_armor.crack"
	case 2:
		snd = "minecraft:item.wolf_armor.break"
	}
	h.playSoundDim(players, m.dim, snd, sndNeutral, m.x, m.y, m.z, 1, 1)
	h.petEquipSync(players, m)
}

// petEquipSync broadcasts a pet's body slot (wolf armour) to its viewers.
func (h *hub) petEquipSync(players map[int32]*tracked, m *mob) {
	var eq attachproto.Equipment
	eq.EID = m.eid
	eq.Slots[attachproto.EquipBody] = stackEv(m.armorSt)
	h.toNearbyEv(players, m.dim, m.x, m.z, eq)
}

// tryWolfArmor is the wolf-armour part of Wolf.mobInteract, for the owner
// of a tamed wolf. Returns true when the click was spent here.
func (h *hub) tryWolfArmor(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityWolf || !m.tamed || m.owner != t.p.eid || m.dying > 0 {
		return false
	}
	held := heldStack(t)
	switch {
	case held.item == itemWolfArmor && m.armorSt.item == 0 && !m.baby:
		piece := held
		piece.count = 1
		m.armorSt = piece
		if t.gamemode == gmSurvival {
			h.consumeHeld(t)
		}
		h.playSoundDim(players, m.dim, "minecraft:item.armor.equip_wolf", sndNeutral, m.x, m.y, m.z, 1, 1)
		m.refreshGearArmor()
		h.petEquipSync(players, m)
		return true
	case held.item == itemShears && m.armorSt.item != 0:
		if t.gamemode == gmSurvival {
			h.applyToolWear(t, t.p.heldSlot(), 1)
		}
		h.playSoundDim(players, m.dim, "minecraft:item.armor.unequip_wolf", sndNeutral, m.x, m.y, m.z, 1, 1)
		piece := m.armorSt
		m.armorSt = invStack{}
		m.refreshGearArmor()
		h.petEquipSync(players, m)
		if it := h.spawnItemIn(players, m.dim, piece.item, 1, m.x, m.y+0.5, m.z); it != nil {
			it.dmg, it.ench, it.color, it.name, it.repairCost = piece.dmg, piece.ench, piece.color, piece.name, piece.repairCost
			h.refreshItemMeta(players, it)
		}
		return true
	case held.item == itemArmadilloScute && m.sitting && m.armorSt.item == itemWolfArmor && m.armorSt.dmg > 0:
		if t.gamemode == gmSurvival {
			h.consumeHeld(t)
		}
		h.playSoundDim(players, m.dim, "minecraft:item.wolf_armor.repair", sndNeutral, m.x, m.y, m.z, 1, 1)
		m.armorSt.dmg = max(0, m.armorSt.dmg-int(float64(wolfArmorMax())*wolfArmorRepairPt))
		h.petEquipSync(players, m)
		return true
	}
	return false
}
