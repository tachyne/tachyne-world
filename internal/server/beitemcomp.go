package server

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/tachyne/tachyne-common/protocol"
)

// A stack's beData (pickdata.go) as the client sees it: the item
// components vanilla puts on a ctrl-picked block's item.
//
//	block_entity_data   the block entity's tag, its type named under "id"
//	                    (TypedEntityData<BlockEntityType>), minus what is
//	                    an item component of its own
//	sign_text_front     a sign's front and back (SignText.STREAM_CODEC),
//	sign_text_back      26.3 components the tooltip lists
//	waxed               a waxed sign (a Unit)
//
// The creative-slot decoder reads all four back into beData, so a stack a
// creative client moves keeps them.

// Canonical ids of the block-entity components (tachyne-common's
// protocol.ComponentBlockEntityData770 and the 26.3-only three, which have
// no 770 id and keep their 26.3 ones).
const (
	componentBlockEntityData = 51
	componentSignTextFront   = 118
	componentSignTextBack    = 119
	componentWaxed           = 120
)

// blockEntityTypeNames is the canonical block_entity_type registry in id
// order (the ids protocol.BlockEntityType gives).
var blockEntityTypeNames = []string{
	"furnace", "chest", "trapped_chest", "ender_chest", "jukebox", "dispenser",
	"dropper", "sign", "hanging_sign", "mob_spawner", "creaking_heart", "piston",
	"brewing_stand", "enchanting_table", "end_portal", "beacon", "skull",
	"daylight_detector", "hopper", "comparator", "banner", "structure_block",
	"end_gateway", "command_block", "shulker_box", "bed", "conduit", "barrel",
	"smoker", "blast_furnace", "lectern", "bell", "jigsaw", "campfire", "beehive",
	"sculk_sensor", "calibrated_sculk_sensor", "sculk_catalyst", "sculk_shrieker",
	"chiseled_bookshelf", "shelf", "brushable_block", "decorated_pot", "crafter",
	"trial_spawner", "vault", "test_block", "test_instance_block",
	"copper_golem_statue",
}

// blockEntityTypeByName is the canonical id of a block entity type name
// ("minecraft:" optional).
func blockEntityTypeByName(name string) (int32, bool) {
	name = strings.TrimPrefix(name, "minecraft:")
	for i, n := range blockEntityTypeNames {
		if n == name {
			return int32(i), true
		}
	}
	return 0, false
}

// engineTagKey reports whether a tag key is the engine's own (a nested
// stack's component patch, pickComponentsKey): kept off the client's copy.
func engineTagKey(k string) bool { return strings.HasPrefix(k, "tachyne:") }

// clientTag is a copy of a tag without the engine's own keys.
func clientTag(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			if !engineTagKey(k) {
				out[k] = clientTag(e)
			}
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = clientTag(e)
		}
		return out
	}
	return v
}

// beDataComponents is the components a stack's beData stands for.
func beDataComponents(st invStack) (int32, []byte) {
	if st.beData == "" {
		return 0, nil
	}
	v, err := parseSNBT(st.beData)
	m, ok := v.(map[string]any)
	if err != nil || !ok {
		return 0, nil
	}
	id, ok := snbtInt(m["id"])
	if !ok || id < 0 || int(id) >= len(blockEntityTypeNames) {
		return 0, nil
	}
	var n int32
	var b []byte
	rest := map[string]any{}
	for k, e := range m {
		switch k {
		case "id", "front_text", "back_text", "is_waxed":
			continue
		}
		rest[k] = e
	}
	if len(rest) > 0 {
		tag := clientTag(rest).(map[string]any)
		tag["id"] = "minecraft:" + blockEntityTypeNames[id]
		raw, err := json.Marshal(tag)
		if err == nil {
			if nbt, ok := protocol.JSONToNBT(raw); ok {
				b = protocol.AppendVarInt(b, componentBlockEntityData)
				b = append(b, nbt...)
				n++
			}
		}
	}
	for _, side := range []struct {
		key  string
		comp int32
	}{{"front_text", componentSignTextFront}, {"back_text", componentSignTextBack}} {
		if raw, ok := m[side.key]; ok {
			if s, ok := signSideFromNBT(raw); ok {
				b = protocol.AppendVarInt(b, side.comp)
				b = appendSignText(b, s)
				n++
			}
		}
	}
	if w, ok := snbtInt(m["is_waxed"]); ok && w != 0 {
		b = protocol.AppendVarInt(b, componentWaxed)
		n++
	}
	return n, b
}

// appendSignText is SignText.STREAM_CODEC: four messages, no filtered ones
// (they equal the messages), the dye and the glow.
func appendSignText(b []byte, s signSide) []byte {
	for _, l := range s.Lines {
		b = append(b, chatNBT(l)...)
	}
	b = append(b, 0) // filtered_messages: absent
	color := dyeIndex(s.Color)
	if color < 0 {
		color = dyeIndex("black")
	}
	b = protocol.AppendVarInt(b, int32(color))
	return append(b, b2u8(s.Glow))
}

func b2u8(v bool) byte {
	if v {
		return 1
	}
	return 0
}

// readSignText reads SignText.STREAM_CODEC back as a side's tag.
func readSignText(r *bytes.Reader) (map[string]any, bool) {
	var s signSide
	for i := range s.Lines {
		l, ok := readTextNBT(r)
		if !ok {
			return nil, false
		}
		s.Lines[i] = l
	}
	has, err := r.ReadByte()
	if err != nil {
		return nil, false
	}
	if has != 0 { // the filtered lines: read and dropped, as the engine keeps none
		for i := 0; i < 4; i++ {
			if _, ok := readTextNBT(r); !ok {
				return nil, false
			}
		}
	}
	color, err := protocol.ReadVarInt(r)
	if err != nil || color < 0 || int(color) >= len(dyeName) {
		return nil, false
	}
	glow, err := r.ReadByte()
	if err != nil {
		return nil, false
	}
	s.Color, s.Glow = dyeName[color], glow != 0
	return signSideTag(s), true
}

// signItemBlockEntity is the block entity a sign item places (sign or
// hanging_sign), false for any other item.
func signItemBlockEntity(item int32) (int32, bool) {
	name := itemNameOf[item]
	switch {
	case strings.HasSuffix(name, "_hanging_sign"):
		return blockEntityTypeByName("hanging_sign")
	case strings.HasSuffix(name, "_sign"):
		return blockEntityTypeByName("sign")
	}
	return 0, false
}
