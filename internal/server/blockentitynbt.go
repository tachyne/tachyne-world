package server

import (
	"fmt"
	"math"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/protocol"
)

// Block entity data as NBT, for the commands that take it: the {…} after a
// block state in /setblock and /fill (BlockInput.place loads it into the
// fresh block entity), and the {…} of a block predicate (/fill … replace,
// /clone … filtered, an item's can_break / can_place_on), which vanilla
// matches against the block entity's saved tag with NbtUtils.compareNbt.
//
// The engine keeps each kind of block entity in its own hub map, so there
// is no saved tag to read: blockEntityNBT composes the part of one the
// engine models — a container's Items, a custom name, a sign's two sides,
// a banner's patterns, a spawner's entity and delay — in vanilla's field
// names, and carriedFromNBT reads the same fields back.

// stackNBT is an ItemStack's saved form: id, count and the components the
// engine models (the {Slot} a container adds is the caller's).
func stackNBT(st invStack) map[string]any {
	m := map[string]any{"id": "minecraft:" + itemNameOf[st.item], "count": int64(st.count)}
	comps := map[string]any{}
	if st.dmg > 0 {
		comps["minecraft:damage"] = int64(st.dmg)
	}
	if st.name != "" {
		comps["minecraft:custom_name"] = st.name
	}
	if st.potion != potNone && st.item != itemOminousBottle {
		for n, id := range potionByVanillaName {
			if id == st.potion {
				comps["minecraft:potion_contents"] = map[string]any{"potion": "minecraft:" + n}
				break
			}
		}
	}
	if len(st.ench) > 0 {
		lv := map[string]any{}
		for _, e := range st.ench {
			if e.lvl > 0 {
				lv["minecraft:"+enchName(e.id)] = int64(e.lvl)
			}
		}
		if len(lv) > 0 {
			key := "minecraft:enchantments"
			if st.item == itemEnchantedBook {
				key = "minecraft:stored_enchantments"
			}
			comps[key] = lv
		}
	}
	if st.noteSound != "" {
		comps["minecraft:note_block_sound"] = st.noteSound
	}
	if len(comps) > 0 {
		m["components"] = comps
	}
	return m
}

// stackFromNBT is stackNBT's inverse (ItemStack.CODEC): an unknown item or a
// component the engine does not model refuses the whole stack.
func stackFromNBT(m map[string]any) (invStack, bool) {
	id, _ := m["id"].(string)
	item, ok := itemByName[strings.TrimPrefix(id, "minecraft:")]
	if !ok || item == 0 {
		return invStack{}, false
	}
	st := invStack{item: item, count: 1}
	if n, ok := snbtInt(m["count"]); ok {
		if n < 1 || n > 99 {
			return invStack{}, false
		}
		st.count = int(n)
	}
	if comps, ok := m["components"].(map[string]any); ok {
		for k, v := range comps {
			if msg := applyItemComponent(&st, nsID(k), v); msg != "" {
				return invStack{}, false
			}
		}
	}
	return st, true
}

// itemsNBT is ContainerHelper.saveAllItems: the non-empty slots, each with
// its Slot byte.
func itemsNBT(slots []invStack) []any {
	out := []any{}
	for i, st := range slots {
		if st.item == 0 || st.count <= 0 {
			continue
		}
		m := stackNBT(st)
		m["Slot"] = int64(i)
		out = append(out, m)
	}
	return out
}

// itemsFromNBT is ContainerHelper.loadAllItems into n slots.
func itemsFromNBT(v any, n int) ([]invStack, bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	slots := make([]invStack, n)
	for _, raw := range list {
		it, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		slot, ok := snbtInt(it["Slot"])
		st, known := stackFromNBT(it)
		if !ok || !known || slot < 0 || int(slot) >= n {
			continue
		}
		slots[slot] = st
	}
	return slots, true
}

// signSideTag is SignText's saved form.
func signSideTag(s signSide) map[string]any {
	msgs := make([]any, 4)
	for i, l := range s.Lines {
		msgs[i] = l
	}
	color := s.Color
	if color == "" {
		color = "black"
	}
	return map[string]any{"messages": msgs, "color": color, "has_glowing_text": s.Glow}
}

// signSideFromNBT reads a SignText: four messages (plain strings, or text
// components whose text is taken), a dye colour and the glow flag.
func signSideFromNBT(v any) (signSide, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return signSide{}, false
	}
	var s signSide
	if msgs, ok := m["messages"].([]any); ok {
		if len(msgs) != 4 {
			return signSide{}, false
		}
		for i, raw := range msgs {
			switch x := raw.(type) {
			case string:
				s.Lines[i] = x
			case map[string]any:
				s.Lines[i], _ = x["text"].(string)
			default:
				return signSide{}, false
			}
		}
	}
	if c, ok := m["color"].(string); ok {
		if dyeIndex(c) < 0 {
			return signSide{}, false
		}
		if c != "black" {
			s.Color = c
		}
	}
	if g, ok := snbtInt(m["has_glowing_text"]); ok {
		s.Glow = g != 0
	}
	return s, true
}

// dyeIndex is a DyeColor's id by name, -1 for none.
func dyeIndex(name string) int {
	for i, n := range dyeName {
		if n == name {
			return i
		}
	}
	return -1
}

// bannerLayersNBT is BannerPatternLayers' saved form.
func bannerLayersNBT(layers []attachproto.BannerLayer) []any {
	out := make([]any, 0, len(layers))
	for _, l := range layers {
		out = append(out, map[string]any{"pattern": bannerPatternQualified(l.Pattern), "color": l.Color})
	}
	return out
}

// bannerLayersFromNBT reads a patterns list: each {pattern, color} names a
// banner_pattern and a dye.
func bannerLayersFromNBT(v any) ([]attachproto.BannerLayer, bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]attachproto.BannerLayer, 0, len(list))
	for _, raw := range list {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, false
		}
		pat, _ := m["pattern"].(string)
		color, _ := m["color"].(string)
		if _, ok := bannerPatternIDs[strings.TrimPrefix(pat, "minecraft:")]; !ok || dyeIndex(color) < 0 {
			return nil, false
		}
		out = append(out, attachproto.BannerLayer{Pattern: strings.TrimPrefix(pat, "minecraft:"), Color: color})
	}
	return out, true
}

// blockEntityNBT is the modelled part of a cell's block entity tag, nil when
// the cell has none.
func (h *hub) blockEntityNBT(pos simPos, state uint32) map[string]any {
	c := h.peekBlockEntity(pos, false)
	m := map[string]any{}
	switch {
	case c.chest != nil:
		m["Items"] = itemsNBT(c.chest.slots[:])
	case c.bin != nil:
		m["Items"] = itemsNBT(c.bin.slots)
	case c.furnace != nil:
		m["Items"] = itemsNBT(c.furnace.slots[:])
	}
	if c.sign != nil {
		m["front_text"] = signSideTag(c.sign.Front)
		m["back_text"] = signSideTag(c.sign.Back)
		m["is_waxed"] = c.sign.Waxed
	}
	if c.banner != nil {
		m["patterns"] = bannerLayersNBT(c.banner)
	}
	if c.spawner != "" {
		m["SpawnData"] = map[string]any{"entity": map[string]any{"id": nsID(c.spawner)}}
		if c.spawnerDl != nil {
			m["Delay"] = int64(*c.spawnerDl)
		}
	}
	if h.blockNames != nil {
		if n := h.blockNames.get(pos); n != "" {
			m["CustomName"] = n
		}
	}
	if len(m) == 0 && !isChestLikeContainer(state) && !isBinBlock(state) {
		return nil
	}
	return m
}

// nbtMatches is NbtUtils.compareNbt: every key of a compound in want is in
// have with a matching value, and with partial lists every element of a list
// in want matches some element of have's. The engine's SNBT does not keep
// the numeric tag types apart, so numbers compare by value.
func nbtMatches(want, have any, partial bool) bool {
	if want == nil {
		return true
	}
	if have == nil {
		return false
	}
	switch w := want.(type) {
	case map[string]any:
		hm, ok := have.(map[string]any)
		if !ok || len(hm) < len(w) {
			return false
		}
		for k, v := range w {
			if !nbtMatches(v, hm[k], partial) {
				return false
			}
		}
		return true
	case []any:
		hl, ok := have.([]any)
		if !ok {
			return false
		}
		if !partial {
			if len(hl) != len(w) {
				return false
			}
			for i := range w {
				if !nbtMatches(w[i], hl[i], partial) {
					return false
				}
			}
			return true
		}
		if len(w) == 0 {
			return len(hl) == 0
		}
		if len(hl) < len(w) {
			return false
		}
		for _, e := range w {
			found := false
			for _, g := range hl {
				if nbtMatches(e, g, partial) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	case string:
		s, ok := have.(string)
		return ok && s == w
	}
	a, ok1 := nbtNumber(want)
	b, ok2 := nbtNumber(have)
	return ok1 && ok2 && a == b
}

func nbtNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case int64:
		return float64(n), true
	case float64:
		return n, true
	case bool:
		if n {
			return 1, true
		}
		return 0, true
	}
	return math.NaN(), false
}

// carriedFromNBT reads block entity data for a state into the carried form
// placeBlockEntity sets down: Items for a chest-like container, a
// dispenser, dropper, hopper, crafter or brewing stand, a furnace, a
// campfire (with its CookingTimes / CookingTotalTimes) or a chiseled
// bookshelf (with last_interacted_slot); a sign's front_text / back_text /
// is_waxed; a banner's patterns; a lectern's Book and Page; a jukebox's
// RecordItem and ticks_since_song_started; a decorated pot's item. A
// stack's tag may carry its whole component patch (pickStackNBT). A field
// the tag leaves out keeps what the cell had (the fresh block entity's
// default). A malformed field is refused by name.
func (h *hub) carriedFromNBT(pos simPos, state uint32, nbt map[string]any) (carriedBE, string) {
	c := h.peekBlockEntity(pos, false)
	bad := func(field string) (carriedBE, string) {
		return carriedBE{}, fmt.Sprintf("Invalid block entity data: '%s'", field)
	}
	if v, ok := nbt["Items"]; ok {
		switch {
		case isChestLikeContainer(state):
			slots, ok := h.fullItemsFromNBT(v, 27)
			if !ok {
				return bad("Items")
			}
			ch := &chest{}
			copy(ch.slots[:], slots)
			c.chest = ch
		case isBinBlock(state):
			slots, ok := h.fullItemsFromNBT(v, binSizeFor(state))
			if !ok {
				return bad("Items")
			}
			b := &bin{slots: slots}
			if old := h.bins[pos]; old != nil {
				b.disabled = old.disabled
			}
			c.bin = b
		case isCampfireBlock(state):
			slots, ok := h.fullItemsFromNBT(v, 4)
			if !ok {
				return bad("Items")
			}
			cf := &campfire{}
			if c.campfire != nil {
				cp := *c.campfire
				cf = &cp
			}
			for i, s := range slots {
				cf.items[i] = 0
				if s.item != 0 && s.count > 0 {
					cf.items[i] = s.item
				}
			}
			c.campfire = cf
		case isBookshelf(state):
			slots, ok := h.fullItemsFromNBT(v, 6)
			if !ok {
				return bad("Items")
			}
			var shelf [6]invStack
			copy(shelf[:], slots)
			c.shelf = &shelf
		default:
			if kind, ok := furnaceKindOf(state); ok {
				slots, ok := h.fullItemsFromNBT(v, 3)
				if !ok {
					return bad("Items")
				}
				f := &furnace{kind: kind}
				if old := h.furnaces[pos]; old != nil {
					cp := *old
					cp.viewers = nil
					f = &cp
				}
				copy(f.slots[:], slots)
				c.furnace = f
			}
		}
	}
	if kind, isSign := signKind(state); isSign {
		sd := signData{Hanging: kind == signHangingCeiling || kind == signHangingWall}
		if c.sign != nil {
			sd = *c.sign
		}
		for key, side := range map[string]*signSide{"front_text": &sd.Front, "back_text": &sd.Back} {
			if v, ok := nbt[key]; ok {
				s, ok := signSideFromNBT(v)
				if !ok {
					return bad(key)
				}
				*side = s
			}
		}
		if w, ok := snbtInt(nbt["is_waxed"]); ok {
			sd.Waxed = w != 0
		}
		c.sign = &sd
	}
	if isCampfireBlock(state) {
		// CampfireBlockEntity.loadAdditional: the cooking progress and
		// totals, slot by slot.
		for _, key := range []string{"CookingTimes", "CookingTotalTimes"} {
			v, ok := nbt[key]
			if !ok {
				continue
			}
			vals, ok := snbtInts(v)
			if !ok {
				return bad(key)
			}
			cf := &campfire{}
			if c.campfire != nil {
				cp := *c.campfire
				cf = &cp
			}
			dst := &cf.prog
			if key == "CookingTotalTimes" {
				dst = &cf.total
			}
			for i := range dst {
				dst[i] = 0
				if i < len(vals) {
					dst[i] = int(vals[i])
				}
			}
			c.campfire = cf
		}
	}
	if isBookshelf(state) {
		if v, ok := nbt["last_interacted_slot"]; ok {
			n, ok := snbtInt(v)
			if !ok {
				return bad("last_interacted_slot")
			}
			last := int(n)
			c.shelfLast = &last
		}
	}
	if v, ok := nbt["Book"]; ok && isLectern(state) {
		// LecternBlockEntity.loadAdditional: the book, and the page clamped
		// to it.
		book, ok := h.fullOneStackFromNBT(v)
		if !ok {
			return bad("Book")
		}
		l := &lectern{book: book}
		if n, ok := snbtInt(nbt["Page"]); ok {
			pages := 1
			if h.books != nil {
				pages = h.lecternPages(l)
			}
			l.page = int(max(0, min(n, int64(pages-1))))
		}
		c.lectern = l
	}
	if v, ok := nbt["RecordItem"]; ok && isJukebox(state) {
		// JukeboxBlockEntity.loadAdditional: the disc, and — with
		// ticks_since_song_started — its song carried on where it was,
		// without starting it afresh (setSongWithoutPlaying).
		disc, ok := h.fullOneStackFromNBT(v)
		if !ok {
			return bad("RecordItem")
		}
		disc.count = 1
		j := &jukebox{disc: disc}
		if ticks, ok := snbtInt(nbt["ticks_since_song_started"]); ok && ticks >= 0 {
			if _, length, ok := jukeboxSongFor(disc.item); ok && uint64(ticks) < length {
				if now := h.tick.Load(); now > uint64(ticks) {
					j.started, j.length = now-uint64(ticks), length
				}
			}
		}
		c.jukebox = j
	}
	if v, ok := nbt["item"]; ok && isDecoratedPot(state) {
		// DecoratedPotBlockEntity's one stack.
		st, ok := h.fullOneStackFromNBT(v)
		if !ok {
			return bad("item")
		}
		c.pot = &st
	}
	if v, ok := nbt["patterns"]; ok && isBannerState(state) {
		layers, ok := bannerLayersFromNBT(v)
		if !ok {
			return bad("patterns")
		}
		c.banner = layers
	}
	return c, ""
}

// bannerPatternIDFor is a banner_pattern's registry id by name, for a stack's
// layers (patPlus1 = id + 1).
func bannerPatternIDFor(name string) (int16, bool) {
	id, ok := bannerPatternIDs[strings.TrimPrefix(name, "minecraft:")]
	if !ok || protocol.BannerPatternName(int32(id)) == "" {
		return 0, false
	}
	return id, true
}
