package server

import (
	"github.com/tachyne/tachyne-common/protocol"
)

// Sheep colour, and the dye that changes it. Two bugs in one place: sheep were
// ALWAYS white because the fleece byte's colour bits were hard-zero, and dye
// did nothing to them because there was no colour to set. Vanilla distributes
// colours at spawn and lets a dye recolour a live sheep, and the wool it drops
// follows the fleece.

// The colour names live in sign.go as dyeColors — registry order, which is also
// the order the fleece byte and every dyed-block family use.

// dyeItemColor maps a dye item to its colour index (-1 if it is not a dye).
var dyeItemColor = func() map[int32]int8 {
	m := map[int32]int8{}
	for i, c := range dyeColors {
		if id, ok := itemByName[c+"_dye"]; ok {
			m[int32(id)] = int8(i)
		}
	}
	return m
}()

// woolForColor is the wool item of each colour, so a sheep drops its own fleece.
var woolForColor = func() []int32 {
	out := make([]int32, len(dyeColors))
	for i, c := range dyeColors {
		if id, ok := itemByName[c+"_wool"]; ok {
			out[i] = int32(id)
		}
	}
	return out
}()

// rollSheepColor is Sheep.getRandomSheepColour: mostly white, a spread of
// greys, some brown, and pink about one in six hundred.
func (h *hub) rollSheepColor() int8 {
	switch r := h.rng.Intn(1000); {
	case r < 818:
		return 0 // white
	case r < 868:
		return 7 // gray
	case r < 918:
		return 8 // light gray
	case r < 968:
		return 15 // black
	case r < 998:
		return 12 // brown
	default:
		return 6 // pink
	}
}

// sheepFleeceMeta builds the fleece byte: low four bits the colour, 0x10 the
// sheared flag. The colour bits were the part that was always zero.
func sheepFleeceMeta(eid int32, color int8, sheared bool) []byte {
	v := byte(color) & 0x0f
	if sheared {
		v |= 0x10
	}
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexSheep)
	b = protocol.AppendVarInt(b, 0) // type: byte
	b = protocol.AppendU8(b, v)
	return protocol.AppendU8(b, itemMetaEnd)
}

// dyeSheep recolours a sheep from a held dye. Reports whether it took, so the
// caller knows to consume the dye.
func (h *hub) dyeSheep(players map[int32]*tracked, m *mob, item int32) bool {
	color, ok := dyeItemColor[item]
	if !ok || m.etype != entitySheep || m.color == color {
		return false
	}
	m.color = color
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(sheepFleeceMeta(m.eid, m.color, m.sheared)))
	h.playSound(players, "minecraft:item.dye.use", sndNeutral, m.x, m.y, m.z, 1, 1)
	return true
}

// sheepWool is the wool a sheep drops or is sheared for — its own colour.
func sheepWool(m *mob) int32 {
	i := int(m.color)
	if i < 0 || i >= len(woolForColor) || woolForColor[i] == 0 {
		return itemWhiteWool
	}
	return woolForColor[i]
}

// tryDyeSheep is the right-click branch: a dye on a sheep recolours it and
// costs one dye in survival.
func (h *hub) tryDyeSheep(players map[int32]*tracked, t *tracked, m *mob) bool {
	held := heldStack(t)
	if !h.dyeSheep(players, m, held.item) {
		return false
	}
	if t.gamemode == gmSurvival {
		slot := t.p.heldSlot()
		if held.count--; held.count <= 0 {
			held = invStack{}
		}
		t.inv.slots[slot] = held
		h.sendSlot(t, slot)
	}
	return true
}
