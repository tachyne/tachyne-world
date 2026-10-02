package server

import (
	"encoding/base64"
	"sort"
	"strconv"
	"strings"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
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
// its boxID (the container component the box item already has), a player
// head's owner and note sound its profile and noteSound, a decorated pot's
// faces its sherds (pot_decorations), a hive's bees and honey its hiveID
// (bees + block_state), and the rest — a chest's, barrel's, hopper's,
// furnace's, campfire's or chiseled bookshelf's Items, a campfire's cooking
// times, a lectern's Book and Page, a jukebox's RecordItem and how far its
// song has played, a pot's item, a spawner's entity and delay — beData.
//
// A sign's two sides and its wax are item components in 26.3
// (sign_text_front, sign_text_back, waxed — SignBlockEntity.
// collectImplicitComponents), so they are not op-gated as the sign's
// block_entity_data is; the engine keeps them in beData under the block
// entity's own field names and loads them whoever places the sign. A copied
// sign comes down with its text, and the editor that opens on it
// (SignBlock.setPlacedBy) opens on that text — not at all on a waxed sign.
//
// A stack inside a copied block entity keeps every component the engine
// models: its tag carries, beside the vanilla id/count/components fields,
// the stack's whole canonical component patch under pickComponentsKey,
// which placement reads back with the creative-slot decoder.

// pickComponentsKey is the engine's own field on a copied stack's tag: the
// stack's canonical component patch (stackComponents), base64.
const pickComponentsKey = "tachyne:components"

// opOnlyBlockEntity reports whether a block's block entity data is one only
// an operator may set from an item (BlockEntityTypes.OP_ONLY_CUSTOM_DATA:
// command block, lectern, sign, hanging sign, spawner, trial spawner). Of
// these the engine carries a spawner's data, a lectern's book and a sign's
// text — the last an item component in 26.3, exempt (signComponentKeys).
func opOnlyBlockEntity(state uint32) bool {
	if state == spawnerBlock || isLectern(state) {
		return true
	}
	_, isSign := signKind(state)
	return isSign
}

// signComponentKeys are the beData fields that stand for a sign's item
// components (sign_text_front, sign_text_back, waxed): applied however the
// sign is placed (BlockItem.updateBlockEntityComponents), not op-gated.
var signComponentKeys = []string{"front_text", "back_text", "is_waxed"}

// pickStackNBT is stackNBT for a stack inside a copied block entity: the
// vanilla fields, and the whole component patch under pickComponentsKey.
func pickStackNBT(st invStack) map[string]any {
	m := stackNBT(st)
	if p := stackComponents(st); len(p) > 2 { // more than the two empty counts
		m[pickComponentsKey] = base64.StdEncoding.EncodeToString(p)
	}
	return m
}

// pickItemsNBT is itemsNBT with pickStackNBT's stacks.
func pickItemsNBT(slots []invStack) []any {
	out := []any{}
	for i, st := range slots {
		if st.item == 0 || st.count <= 0 {
			continue
		}
		m := pickStackNBT(st)
		m["Slot"] = int64(i)
		out = append(out, m)
	}
	return out
}

// fullStackFromNBT is a stack read from its tag (st, as stackFromNBT gave
// it) completed from the component patch pickStackNBT put beside it: the
// creative-slot decoder reads every component the engine models, minting
// fresh records for a box's, bundle's or book's contents.
func (h *hub) fullStackFromNBT(m map[string]any, st invStack) invStack {
	s, ok := m[pickComponentsKey].(string)
	if !ok || st.item == 0 {
		return st
	}
	patch, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return st
	}
	return h.creativeStack(st.item, st.count, patch)
}

// fullItemsFromNBT is itemsFromNBT with each slot completed by
// fullStackFromNBT.
func (h *hub) fullItemsFromNBT(v any, n int) ([]invStack, bool) {
	slots, ok := itemsFromNBT(v, n)
	if !ok {
		return nil, false
	}
	list, _ := v.([]any)
	for _, raw := range list {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		slot, ok := snbtInt(m["Slot"])
		if !ok || slot < 0 || int(slot) >= n || slots[slot].item == 0 {
			continue
		}
		slots[slot] = h.fullStackFromNBT(m, slots[slot])
	}
	return slots, true
}

// fullOneStackFromNBT reads a single stack field (a lectern's Book, a
// jukebox's RecordItem, a pot's item); false when it does not read.
func (h *hub) fullOneStackFromNBT(v any) (invStack, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return invStack{}, false
	}
	st, ok := stackFromNBT(m)
	if !ok {
		return invStack{}, false
	}
	return h.fullStackFromNBT(m, st), true
}

// snbtInts reads an int array (or a list of whole numbers).
func snbtInts(v any) ([]int64, bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]int64, 0, len(list))
	for _, e := range list {
		n, ok := snbtInt(e)
		if !ok {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

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
	if nbt == nil {
		nbt = map[string]any{}
	}
	delete(nbt, "CustomName")
	c := h.peekBlockEntity(pos, false)
	switch { // the stacks inside, with every component they carry
	case c.chest != nil:
		nbt["Items"] = pickItemsNBT(c.chest.slots[:])
	case c.bin != nil:
		nbt["Items"] = pickItemsNBT(c.bin.slots)
	case c.furnace != nil:
		nbt["Items"] = pickItemsNBT(c.furnace.slots[:])
	}
	// sign_text_front, sign_text_back, waxed: kept beside the rest (see
	// signComponentKeys) unless the sign is blank and unwaxed.
	if c.sign == nil || (c.sign.Front == signSide{} && c.sign.Back == signSide{} && !c.sign.Waxed) {
		for _, k := range signComponentKeys {
			delete(nbt, k)
		}
	}
	if isPlayerHeadState(state) && h.skulls != nil { // profile, note_block_sound
		st.profile = h.skulls.get(pos)
		st.noteSound = h.skulls.note(pos)
	}
	if c.lectern != nil && c.lectern.book.item != 0 && c.lectern.book.count > 0 {
		nbt["Book"] = pickStackNBT(c.lectern.book) // LecternBlockEntity.saveAdditional
		nbt["Page"] = int64(c.lectern.page)
	}
	if c.jukebox != nil && c.jukebox.disc.item != 0 && c.jukebox.disc.count > 0 {
		nbt["RecordItem"] = pickStackNBT(c.jukebox.disc) // JukeboxBlockEntity.saveAdditional
		if now := h.tick.Load(); c.jukebox.playing(now) {
			// JukeboxSongPlayer: how far into the song, while one plays.
			nbt["ticks_since_song_started"] = int64(now - c.jukebox.started)
		}
	}
	if c.campfire != nil && isCampfireBlock(state) { // CampfireBlockEntity.saveAdditional
		var slots [4]invStack
		for i, it := range c.campfire.items {
			if it != 0 {
				slots[i] = invStack{item: it, count: 1}
			}
		}
		nbt["Items"] = pickItemsNBT(slots[:])
		times, totals := make([]any, 4), make([]any, 4)
		for i := range times {
			times[i], totals[i] = int64(c.campfire.prog[i]), int64(c.campfire.total[i])
		}
		nbt["CookingTimes"], nbt["CookingTotalTimes"] = times, totals
	}
	if c.shelf != nil && isBookshelf(state) { // ChiseledBookShelfBlockEntity.saveAdditional
		nbt["Items"] = pickItemsNBT(c.shelf[:])
		last := int64(-1)
		if c.shelfLast != nil {
			last = int64(*c.shelfLast)
		}
		nbt["last_interacted_slot"] = last
	}
	if isDecoratedPot(state) {
		if c.sherds != nil { // pot_decorations
			st.sherds = *c.sherds
		}
		if c.pot != nil && c.pot.item != 0 && c.pot.count > 0 {
			nbt["item"] = pickStackNBT(*c.pot) // the container component's one stack
		}
	}
	if isBeeHome(state) {
		// bees (BeehiveBlockEntity.collectImplicitComponents) and the
		// honey level (BeehiveBlock.getCloneItemStack's block_state), on
		// the hiveID a Silk-Touched hive's item carries.
		st.hiveID = h.stowHiveCopy(pos, honeyLevel(state))
	}
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
	if items, ok := nbt["Items"].([]any); ok && len(items) == 0 && !isCampfireBlock(state) {
		delete(nbt, "Items") // an empty container carries nothing
	}
	if isCampfireBlock(state) && campfireTagIdle(nbt) {
		// An unlit-and-empty campfire's tag is all zeros: nothing to carry.
		delete(nbt, "Items")
		delete(nbt, "CookingTimes")
		delete(nbt, "CookingTotalTimes")
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
	delete(nbt, "id")
	if opOnlyBlockEntity(state) && (t == nil || t.gamemode != gmCreative || h.isOp == nil || !h.isOp(t.p.name)) {
		// Player.canUseGameMasterBlocks: the block entity data stays out.
		// A sign's text and wax are item components, applied regardless.
		comps := map[string]any{}
		if _, isSign := signKind(state); isSign {
			for _, k := range signComponentKeys {
				if v, ok := nbt[k]; ok {
					comps[k] = v
				}
			}
		}
		if len(comps) == 0 {
			return
		}
		nbt = comps
	}
	h.loadBlockEntityNBT(players, pos, state, nbt)
	w := h.worldFor(pos.dim)
	if w == nil {
		return
	}
	cur := w.At(pos.x, pos.y, pos.z)
	info, ok := worldgen.InfoForState(cur)
	if !ok {
		return
	}
	switch {
	case isLectern(cur):
		// LecternBlock.getStateForPlacement: HAS_BOOK when the (operator's)
		// data carries a Book.
		if l := h.lecterns[pos]; l != nil && l.book.item != 0 && worldgen.GetProperty(info, cur, "has_book") != "true" {
			h.setBlockLive(players, pos.dim, pos.x, pos.y, pos.z, worldgen.SetProperty(info, cur, "has_book", "true"))
		}
	case isJukebox(cur):
		// JukeboxBlock.setPlacedBy: HAS_RECORD when the data carries a
		// RecordItem.
		if _, has := nbt["RecordItem"]; has && h.jukeboxes[pos] != nil && cur != jukeboxState(true) {
			h.setBlockLive(players, pos.dim, pos.x, pos.y, pos.z, jukeboxState(true))
		}
	}
}

// campfireTagIdle reports whether a copied campfire tag holds nothing: no
// items and no cooking time anywhere.
func campfireTagIdle(nbt map[string]any) bool {
	if items, ok := nbt["Items"].([]any); ok && len(items) > 0 {
		return false
	}
	for _, k := range []string{"CookingTimes", "CookingTotalTimes"} {
		vals, _ := snbtInts(nbt[k])
		for _, v := range vals {
			if v != 0 {
				return false
			}
		}
	}
	return true
}

// stowHiveCopy is stowHiveItem without emptying the hive: a fresh hiveID
// holding a copy of the hive's occupants and its honey level (0 = nothing
// worth carrying).
func (h *hub) stowHiveCopy(pos simPos, honey int) int32 {
	occ := h.hives[pos]
	if len(occ) == 0 && honey == 0 {
		return 0
	}
	cp := make([]hiveOccupant, len(occ))
	for i, o := range occ {
		if o.Flower != nil {
			f := *o.Flower
			o.Flower = &f
		}
		cp[i] = o
	}
	if h.hiveItems == nil {
		h.hiveItems = map[int32]hiveStow{}
	}
	h.nextHiveID++
	h.hiveItems[h.nextHiveID] = hiveStow{Honey: honey, Occ: cp}
	return h.nextHiveID
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
