package server

import (
	"github.com/tachyne/tachyne-common/protocol"
)

// The shared entity-flags byte (metadata index 0) for a PLAYER. Vanilla packs
// several independent states into that one field, so it can only be written
// whole — the old fire-only builder was safe purely because nothing else set a
// bit. Invisibility and Glowing set two of the others, so composing them in one
// place is now load-bearing rather than tidy.
const (
	entFlagOnFire    = 0x01
	entFlagInvisible = 0x20
	entFlagGlowing   = 0x40
)

// playerEntityFlags packs every flag bit a player's current state implies.
func playerEntityFlags(t *tracked) byte {
	var f byte
	if t.fireSecs > 0 {
		f |= entFlagOnFire
	}
	if t.hasEffect(effInvisibility) > 0 {
		f |= entFlagInvisible
	}
	if t.hasEffect(effGlowing) > 0 {
		f |= entFlagGlowing
	}
	return f
}

// playerFlagsMeta builds the set_entity_data body for that byte.
func playerFlagsMeta(t *tracked) []byte {
	b := protocol.AppendVarInt(nil, t.p.eid)
	b = protocol.AppendU8(b, 0)     // index 0: shared entity flags
	b = protocol.AppendVarInt(b, 0) // type 0: byte
	b = protocol.AppendU8(b, playerEntityFlags(t))
	return protocol.AppendU8(b, itemMetaEnd)
}

// broadcastPlayerFlags pushes the flags to everyone who can see the player,
// and to the player's own client (which renders its own flame overlay and
// the translucency of its own invisibility).
func (h *hub) broadcastPlayerFlags(players map[int32]*tracked, t *tracked) {
	ev := metaEv(playerFlagsMeta(t))
	h.toNearbyEv(players, t.dim, t.x, t.z, ev)
	t.p.trySendEv(ev)
}
