package server

import (
	"github.com/tachyne/tachyne-common/protocol"
)

// Name tags. Renaming a mob is the whole feature, and it carries a second
// meaning that matters more: a named mob NEVER despawns, which is how a pet
// wolf or a hard-won villager stays where you left it.

const (
	metaIndexCustomName        = 2 // Optional<Component>
	metaIndexCustomNameVisible = 3 // boolean
	metaTypeOptionalComponent  = 6
)

// nameMeta builds the two metadata fields that show a name over a mob.
func nameMeta(eid int32, name string) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexCustomName)
	b = protocol.AppendVarInt(b, metaTypeOptionalComponent)
	if name == "" {
		b = protocol.AppendBool(b, false) // no name
	} else {
		b = protocol.AppendBool(b, true)
		b = append(b, chatNBT(name)...)
	}
	b = protocol.AppendU8(b, metaIndexCustomNameVisible)
	b = protocol.AppendVarInt(b, metaTypeBool)
	b = protocol.AppendBool(b, name != "")
	return protocol.AppendU8(b, itemMetaEnd)
}

// tryNameTag is the right-click branch: a NAMED name tag renames the mob it is
// used on. An unnamed one does nothing, exactly as in vanilla — you have to
// put it through an anvil first, which is what makes the anvil step meaningful.
func (h *hub) tryNameTag(players map[int32]*tracked, t *tracked, m *mob) bool {
	held := heldStack(t)
	if held.item != int32(itemByName["name_tag"]) || held.name == "" {
		return false
	}
	if m.etype == entityEnderDragon || m.etype == entityWither {
		return false // vanilla refuses the bosses
	}
	m.customName = held.name
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(nameMeta(m.eid, m.customName)))
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

// named reports whether a mob has been name-tagged — the despawn clock skips
// those, which is the other half of what a name tag is for.
func (m *mob) named() bool { return m.customName != "" }
