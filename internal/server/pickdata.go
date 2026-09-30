package server

import (
	"sort"
	"strconv"
	"strings"

	"github.com/tachyne/tachyne-common/protocol"
)

// Ctrl + middle click in creative (ServerGamePacketListenerImpl.
// addBlockDataToItem): the picked block's item carries its block entity.
// Vanilla saves the block entity (saveCustomOnly), moves the parts that are
// item components onto the stack (collectComponents: a container's
// contents, a custom name, a banner's patterns, a sign's text) and keeps
// the rest as block_entity_data, tagged with the block entity type.
// Placing the item loads it back (BlockItem.updateCustomBlockEntityTag —
// refused on a block entity of another type, and for the op-only types,
// a spawner among them, unless the placer is an operator in creative).
//
// The engine models a block entity as the fields blockEntityNBT composes.
// A stack carries them in beData, an SNBT compound with the type under
// "id" (the block_entity_type network id): the custom name rides the
// stack's name, a banner's patterns its layers, a shulker box's contents
// its boxID (the container component the box item already has), and the
// rest — a chest's, barrel's, hopper's or furnace's Items, a spawner's
// entity and delay — beData. A sign's text (sign_text_front / back, an item
// component in 26.3) is not carried yet: the engine's sign placement opens
// a blank editor of its own.

// opOnlyBlockEntity reports whether a block's block entity data is one only
// an operator may set from an item (BlockEntityTypes.OP_ONLY_CUSTOM_DATA:
// command block, lectern, sign, hanging sign, spawner, trial spawner). Of
// these the engine carries a spawner's data; a sign's text is an item
// component in 26.3 (sign_text_front / back), which is not gated.
func opOnlyBlockEntity(state uint32) bool { return state == spawnerBlock }

// addBlockDataToItem is addBlockDataToItem for a block at pos.
func (h *hub) addBlockDataToItem(pos simPos, state uint32, st invStack) invStack {
	beType, ok := protocol.BlockEntityType(state)
	if !ok {
		return st
	}
	if h.blockNames != nil && nameableBlock(state) {
		if n := h.blockNames.get(pos); n != "" {
			st.name = n // custom_name
		}
	}
	nbt := h.blockEntityNBT(pos, state)
	delete(nbt, "CustomName")
	delete(nbt, "front_text")
	delete(nbt, "back_text")
	delete(nbt, "is_waxed")
	if isBannerState(state) && h.banners != nil { // banner_patterns
		n := 0
		for _, l := range h.banners.get(pos.dim, pos.x, pos.y, pos.z) {
			id, ok := bannerPatternIDs[bannerPatternQualified(l.Pattern)]
			c := dyeIndex(l.Color)
			if !ok || c < 0 || n >= len(st.pats) {
				continue
			}
			st.pats[n] = bannerLayer{patPlus1: id + 1, color: int8(c)}
			n++
		}
		delete(nbt, "patterns")
	}
	if isShulkerBox(state) && h.boxes != nil { // container: the box item's boxID
		if c := h.chests[pos]; c != nil {
			cp := *c
			empty := true
			for i := range cp.slots {
				if cp.slots[i].item != 0 && cp.slots[i].count > 0 {
					empty = false
					cp.slots[i] = h.forkStack(cp.slots[i])
				}
			}
			if !empty {
				id := h.boxes.mint()
				h.boxes.set(id, cp)
				st.boxID = id
			}
		}
		delete(nbt, "Items")
	}
	if items, ok := nbt["Items"].([]any); ok && len(items) == 0 {
		delete(nbt, "Items") // an empty container carries nothing
	}
	if len(nbt) == 0 {
		return st
	}
	nbt["id"] = int64(beType)
	st.beData = writeSNBT(nbt)
	return st
}

// applyItemBlockEntityData is BlockItem.updateCustomBlockEntityTag (and,
// for what the engine carries in the same field, updateBlockEntityComponents)
// for a block just placed from st.
func (h *hub) applyItemBlockEntityData(players map[int32]*tracked, t *tracked, pos simPos, state uint32, st invStack) {
	if st.beData == "" {
		return
	}
	v, err := parseSNBT(st.beData)
	nbt, ok := v.(map[string]any)
	if err != nil || !ok {
		return
	}
	beType, ok := protocol.BlockEntityType(state)
	if want, ok2 := snbtInt(nbt["id"]); !ok || !ok2 || int32(want) != beType {
		return // no block entity here, or one of another type
	}
	if opOnlyBlockEntity(state) && (t == nil || t.gamemode != gmCreative || h.isOp == nil || !h.isOp(t.p.name)) {
		return // Player.canUseGameMasterBlocks
	}
	delete(nbt, "id")
	h.loadBlockEntityNBT(players, pos, state, nbt)
}

// writeSNBT is the SNBT text of a value parseSNBT gives back as the same
// value: compounds with quoted keys in sorted order, lists, quoted strings,
// booleans, whole numbers, and decimals with a d suffix.
func writeSNBT(v any) string {
	var b strings.Builder
	snbtWrite(&b, v)
	return b.String()
}

func snbtWrite(b *strings.Builder, v any) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			snbtQuoteTo(b, k)
			b.WriteByte(':')
			snbtWrite(b, x[k])
		}
		b.WriteByte('}')
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			snbtWrite(b, e)
		}
		b.WriteByte(']')
	case []string:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			snbtQuoteTo(b, e)
		}
		b.WriteByte(']')
	case string:
		snbtQuoteTo(b, x)
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case int:
		b.WriteString(strconv.Itoa(x))
	case int32:
		b.WriteString(strconv.FormatInt(int64(x), 10))
	case int8:
		b.WriteString(strconv.FormatInt(int64(x), 10))
	case float64:
		b.WriteString(strconv.FormatFloat(x, 'g', -1, 64) + "d")
	case float32:
		b.WriteString(strconv.FormatFloat(float64(x), 'g', -1, 32) + "d")
	default:
		b.WriteString(`""`)
	}
}

// snbtQuoteTo writes s as a double-quoted SNBT string.
func snbtQuoteTo(b *strings.Builder, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteByte(c)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
}
