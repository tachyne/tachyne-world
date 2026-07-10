package server

// play.go — what remains of the former connection state machine after the
// domain-events refactor deleted the TCP path (stage 6c): the dig phase
// constants, the interest radius, and the canonical block-entity chunk
// section the attach layer serves to gateways.

import (
	"github.com/tachyne/tachyne-common/protocol"
	"tachyne/internal/world"
)

const (
	digStartBreak  = 0 // creative: break immediately
	digFinishBreak = 2 // survival: finished breaking
	digDropStack   = 3 // ctrl+Q: drop the whole held stack
	digDropOne     = 4 // Q: drop one of the held item
	digReleaseUse  = 5 // released right-click (ends an eat-hold / bow draw)

	gameEventChangeGameMode = 3 // change game mode (value = mode)

	viewRadius = 6 // chunk interest radius (broadcast culling + session Want cap)
)

// appendBlockEntities lists a chunk's block entities (beds, chests, signs, …) so
// the client's block-entity renderer draws them; without this they show only their
// wireframe outline after the chunk (re)loads. Each entry is packed-XZ + Y + type +
// NBT; empty data (a single TAG_End 0x00) is enough for rendering.
func appendBlockEntities(b []byte, w *world.World, cx, cz int32) []byte {
	edits := w.EditedBlocks(cx, cz)
	var buf []byte
	n := int32(0)
	for _, e := range edits {
		typ, ok := protocol.BlockEntityType(e.State)
		if !ok {
			continue
		}
		buf = append(buf, byte((e.LX&15)<<4|(e.LZ&15))) // packed XZ
		buf = protocol.AppendI16(buf, int16(e.Y))
		buf = protocol.AppendVarInt(buf, typ)
		buf = append(buf, 0x00) // NBT: TAG_End — no data (renderer needs only type+pos)
		n++
	}
	b = protocol.AppendVarInt(b, n)
	return append(b, buf...)
}
