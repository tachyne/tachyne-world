package server

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"strings"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/world"
)

// The stack a creative client puts in a slot (ServerboundSetCreativeModeSlot
// → ServerGamePacketListenerImpl.handleSetCreativeModeSlot: the whole stack,
// components and all, goes into the slot). The gateway's chain hands the
// component patch over in canonical form; this reads back every component
// the engine's stacks model — the exact inverse of stackComponents for each
// field it writes — so a creative-picked potion, enchanted book, firework,
// banner, stew or named item keeps what it is. Components the engine does
// not model are skipped.

// Canonical component ids not declared with the rest (item.go).
const (
	componentUnbreakable = 4  // unbreakable: a Unit
	componentEntityData  = 49 // entity_data: the entity's tag (an armor stand's pose and flags)
)

// maxCreativeNesting caps how deep stacks inside stacks (a bundle in a box
// in a bundle…) are read.
const maxCreativeNesting = 4

// potionRegistryOrder is the potion registry in id order (the same from
// 1.21.5 through 26.3): a potion_contents holder is an index into it.
var potionRegistryOrder = []string{"water", "mundane", "thick", "awkward",
	"night_vision", "long_night_vision", "invisibility", "long_invisibility",
	"leaping", "long_leaping", "strong_leaping", "fire_resistance", "long_fire_resistance",
	"swiftness", "long_swiftness", "strong_swiftness", "slowness", "long_slowness",
	"strong_slowness", "turtle_master", "long_turtle_master", "strong_turtle_master",
	"water_breathing", "long_water_breathing", "healing", "strong_healing", "harming",
	"strong_harming", "poison", "long_poison", "strong_poison", "regeneration",
	"long_regeneration", "strong_regeneration", "strength", "long_strength",
	"strong_strength", "weakness", "long_weakness", "luck", "slow_falling",
	"long_slow_falling", "wind_charged", "weaving", "oozing", "infested"}

// stackDecode is one stack being read back from its components, with what
// needs the whole patch before it can be settled (a potion's kind, a
// bucketed fish's three variant parts, the ominous banner's name).
type stackDecode struct {
	h     *hub
	st    invStack
	depth int

	potSeen bool
	potHeld int32 // the potion holder's registry id + 1 (0 = none)
	potEffs []potEffect

	variant     int32 // salmon/size or axolotl/variant + 1 (0 = none)
	fish        [3]int32
	fishSeen    bool
	ominousName bool

	// be is the block entity tag block_entity_data, sign_text_front,
	// sign_text_back and waxed build (beitemcomp.go), the stack's beData.
	be map[string]any
}

// setBE is a copy of the block entity tag with key set (copied, so a
// component that fails later leaves the tag as it was).
func (d *stackDecode) setBE(kv map[string]any) {
	m := make(map[string]any, len(d.be)+len(kv))
	for k, v := range d.be {
		m[k] = v
	}
	for k, v := range kv {
		m[k] = v
	}
	d.be = m
}

// creativeStack is the stack a creative client set: item, count and the
// canonical component patch (attach.ItemStack.Components).
func (h *hub) creativeStack(item int32, count int, comps []byte) invStack {
	d := &stackDecode{h: h, st: invStack{item: item, count: count}}
	if len(comps) > 0 {
		protocol.WalkCanonicalComponents(comps, func(id int32, payload []byte) {
			// One component at a time: a payload that does not read cleanly
			// leaves the stack as it was before it.
			try := *d
			try.potEffs = append([]potEffect(nil), d.potEffs...)
			r := bytes.NewReader(payload)
			if try.component(id, r) && r.Len() == 0 {
				*d = try
			}
		})
	}
	return d.finish()
}

// readSlot reads one whole Slot — count, item, and a canonical patch — the
// way appendStack writes it. Nested stacks carry only components this
// reader can measure; anything else fails the whole value.
func (d *stackDecode) readSlot(r *bytes.Reader) (invStack, bool) {
	if d.depth >= maxCreativeNesting {
		return invStack{}, false
	}
	n, err := protocol.ReadVarInt(r)
	if err != nil || n < 0 {
		return invStack{}, false
	}
	if n == 0 {
		return invStack{}, true
	}
	item, err := protocol.ReadVarInt(r)
	if err != nil {
		return invStack{}, false
	}
	sub := &stackDecode{h: d.h, st: invStack{item: item, count: int(n)}, depth: d.depth + 1}
	addC, e1 := protocol.ReadVarInt(r)
	remC, e2 := protocol.ReadVarInt(r)
	if e1 != nil || e2 != nil || addC < 0 || remC < 0 || addC > 64 || remC > 64 {
		return invStack{}, false
	}
	for i := int32(0); i < addC; i++ {
		id, err := protocol.ReadVarInt(r)
		if err != nil || !sub.component(id, r) {
			return invStack{}, false
		}
	}
	for i := int32(0); i < remC; i++ { // removed components: ids only
		if _, err := protocol.ReadVarInt(r); err != nil {
			return invStack{}, false
		}
	}
	return sub.finish(), true
}

// component reads one component's value onto the stack. False when it is
// one the engine does not know (its length cannot be measured here) or its
// value does not read.
func (d *stackDecode) component(id int32, r *bytes.Reader) bool {
	st := &d.st
	switch id {
	case componentDamage:
		v, err := protocol.ReadVarInt(r)
		if err != nil || v < 0 {
			return false
		}
		st.dmg = int(v)
	case componentEnchantments, componentStoredEnch:
		n, err := protocol.ReadVarInt(r)
		if err != nil || n < 0 || n > 256 {
			return false
		}
		for i := int32(0); i < n; i++ {
			eid, e1 := protocol.ReadVarInt(r)
			lvl, e2 := protocol.ReadVarInt(r)
			if e1 != nil || e2 != nil {
				return false
			}
			if eid >= 0 && eid <= math.MaxInt8 && lvl > 0 {
				st.ench = enchSetLevel(st.ench, int8(eid), int8(min(lvl, math.MaxInt8)))
			}
		}
	case componentCustomName:
		s, ok := readTextNBT(r)
		if !ok {
			return false
		}
		st.name = s
	case componentItemName:
		v, ok := readNetNBT(r)
		if !ok {
			return false
		}
		// The engine writes item_name only on the ominous banner.
		if m, isMap := v.(map[string]any); isMap && m["translate"] == "block.minecraft.ominous_banner" {
			d.ominousName = true
		}
	case componentLore:
		n, err := protocol.ReadVarInt(r)
		if err != nil || n < 0 || n > 256 { // ItemLore.MAX_LINES
			return false
		}
		lines := make([]string, 0, n)
		for i := int32(0); i < n; i++ {
			s, ok := readTextNBT(r)
			if !ok {
				return false
			}
			lines = append(lines, strings.ReplaceAll(s, "\n", " "))
		}
		st.tags.lore = strings.Join(lines, "\n")
	case componentUnbreakable:
		st.tags.unbreakable = true // a Unit: no payload
	case componentCanBreak, componentCanPlaceOn:
		js, ok := readAdvPredicate(r)
		if !ok {
			return false
		}
		if id == componentCanBreak {
			st.tags.canBreak = js
		} else {
			st.tags.canPlace = js
		}
	case componentMapID:
		v, err := protocol.ReadVarInt(r)
		if err != nil {
			return false
		}
		st.mapID = v
	case componentDyedColor:
		var rgb [4]byte // DyedItemColor: ByteBufCodecs.INT, four bytes
		if _, err := io.ReadFull(r, rgb[:]); err != nil {
			return false
		}
		st.color = int32(binary.BigEndian.Uint32(rgb[:]))
	case componentTrim:
		mat, e1 := protocol.ReadVarInt(r)
		pat, e2 := protocol.ReadVarInt(r)
		// Holders: registry id + 1. An inline (0) material or pattern is
		// nothing the engine can name.
		if e1 != nil || e2 != nil || mat <= 0 || pat <= 0 || mat > math.MaxInt8 || pat > math.MaxInt8 {
			return false
		}
		st.trimMat, st.trimPat = int8(mat), int8(pat)
	case componentBannerPats:
		n, err := protocol.ReadVarInt(r)
		if err != nil || n < 0 || n > 256 {
			return false
		}
		var pats [6]bannerLayer
		for i := int32(0); i < n; i++ {
			pat, e1 := protocol.ReadVarInt(r)
			color, e2 := protocol.ReadVarInt(r)
			if e1 != nil || e2 != nil || pat <= 0 || pat > math.MaxInt16 || color < 0 || color > 15 {
				return false // an inline pattern (0) cannot be measured, let alone kept
			}
			if int(i) < len(pats) {
				pats[i] = bannerLayer{patPlus1: int16(pat), color: int8(color)}
			}
		}
		st.pats = pats
	case componentBaseColor:
		v, err := protocol.ReadVarInt(r)
		if err != nil || v < 0 || v > 15 {
			return false
		}
		st.shieldBase = int8(v + 1)
	case componentPotionContents:
		return d.potionContents(r)
	case componentStewEffects:
		n, err := protocol.ReadVarInt(r)
		if err != nil || n < 0 || n > 256 {
			return false
		}
		st.stew = 0
		for i := int32(0); i < n; i++ {
			holder, e1 := protocol.ReadVarInt(r)
			ticks, e2 := protocol.ReadVarInt(r)
			if e1 != nil || e2 != nil {
				return false
			}
			if st.stew == 0 {
				st.stew = stewRowFor(holder, ticks) // plain registry id
			}
		}
	case componentRepairCost:
		v, err := protocol.ReadVarInt(r)
		if err != nil || v < 0 {
			return false
		}
		st.repairCost = int(v)
	case componentOminousBottle:
		v, err := protocol.ReadVarInt(r)
		if err != nil {
			return false
		}
		if st.item == itemOminousBottle {
			st.potion = int8(max(1, min(5, v+1))) // the level rides the potion field
		}
	case componentFireworks:
		flight, err := protocol.ReadVarInt(r)
		if err != nil {
			return false
		}
		bursts, ok := readBursts(r, 256)
		if !ok {
			return false
		}
		if st.item == itemFireworkRocket {
			st.flight = int8(max(0, min(flight, math.MaxInt8)))
			if len(bursts) > maxRocketBursts {
				bursts = bursts[:maxRocketBursts]
			}
			st.starID = d.h.internStars(bursts)
		}
	case componentFireworkStar:
		b, ok := readBurst(r)
		if !ok {
			return false
		}
		if st.item == itemFireworkStar {
			st.starID = d.h.internStars([]fireworkBurst{b})
		}
	case componentPotDecorations:
		n, err := protocol.ReadVarInt(r)
		if err != nil || n < 0 || n > 4 {
			return false
		}
		var sh potSherds
		for i := int32(0); i < n; i++ {
			v, err := protocol.ReadVarInt(r)
			if err != nil {
				return false
			}
			if v != itemBrick { // a brick is a plain side
				sh[i] = v
			}
		}
		st.sherds = sh
	case componentInstrument:
		flag, err := r.ReadByte()
		if err != nil || flag != 1 { // EitherHolder: a holder; a bare key is never ours
			return false
		}
		holder, err := protocol.ReadVarInt(r)
		if err != nil {
			return false
		}
		for i, idx := range instrumentRegistryOrder {
			if idx == holder-1 {
				st.instrument = int8(i)
			}
		}
	case componentLodestone:
		has, err := r.ReadByte()
		if err != nil {
			return false
		}
		l := lodeTracker{has: true}
		if has != 0 {
			name, err := protocol.ReadString(r)
			if err != nil {
				return false
			}
			var pos [8]byte
			if _, err := io.ReadFull(r, pos[:]); err != nil {
				return false
			}
			v := int64(binary.BigEndian.Uint64(pos[:]))
			l.target = true
			l.x, l.y, l.z = int32(v>>38), int32(v<<52>>52), int32(v<<26>>38)
			l.dim = int8(dimByRegistryName(name))
		}
		if _, err := r.ReadByte(); err != nil { // tracked
			return false
		}
		st.lode = l
	case componentWritableBook:
		n, err := protocol.ReadVarInt(r)
		if err != nil || n < 0 || n > bookMaxPages {
			return false
		}
		var b savedBook
		for i := int32(0); i < n; i++ {
			page, err := protocol.ReadString(r)
			if err != nil || !skipOptString(r) { // the filtered page
				return false
			}
			b.Pages = append(b.Pages, page)
		}
		if st.item == itemWritableBook && d.h.books != nil {
			st.bookID = d.h.books.create(b)
		}
	case componentWrittenBook:
		title, err := protocol.ReadString(r)
		if err != nil || !skipOptString(r) { // the filtered title
			return false
		}
		author, err := protocol.ReadString(r)
		if err != nil {
			return false
		}
		gen, err := protocol.ReadVarInt(r)
		if err != nil {
			return false
		}
		n, err := protocol.ReadVarInt(r)
		if err != nil || n < 0 || n > bookMaxPages {
			return false
		}
		b := savedBook{Title: title, Author: author, Gen: int(max(0, min(gen, 3)))}
		for i := int32(0); i < n; i++ {
			page, ok := readTextNBT(r)
			if !ok {
				return false
			}
			filtered, err := r.ReadByte()
			if err != nil {
				return false
			}
			if filtered != 0 {
				if _, ok := readNetNBT(r); !ok {
					return false
				}
			}
			b.Pages = append(b.Pages, page)
		}
		if _, err := r.ReadByte(); err != nil { // resolved
			return false
		}
		if st.item == itemWrittenBook && d.h.books != nil {
			st.bookID = d.h.books.create(b)
		}
	case componentBundleContents:
		n, err := protocol.ReadVarInt(r)
		if err != nil || n < 0 || n > 64 {
			return false
		}
		var items []invStack
		for i := int32(0); i < n; i++ {
			s, ok := d.readSlot(r)
			if !ok {
				return false
			}
			if s.item != 0 && s.count > 0 {
				items = append(items, s)
			}
		}
		if isBundle(st.item) && len(items) > 0 && d.h.bundles != nil {
			id := d.h.bundles.mint()
			d.h.bundles.set(id, items)
			st.bundleID = id
		}
	case componentContainer:
		n, err := protocol.ReadVarInt(r)
		if err != nil || n < 0 || n > 256 {
			return false
		}
		var c chest
		filled := false
		for i := int32(0); i < n; i++ {
			s, ok := d.readSlot(r)
			if !ok {
				return false
			}
			if int(i) < len(c.slots) && s.item != 0 && s.count > 0 {
				c.slots[i] = s
				filled = true
			}
		}
		if isShulkerBoxItem(st.item) && filled && d.h.boxes != nil {
			id := d.h.boxes.mint()
			d.h.boxes.set(id, c)
			st.boxID = id
		}
	case componentChargedProj:
		n, err := protocol.ReadVarInt(r)
		if err != nil || n < 0 || n > 64 {
			return false
		}
		var load xbowLoad
		for i := int32(0); i < n; i++ {
			s, ok := d.readSlot(r)
			if !ok {
				return false
			}
			if s.item == 0 || s.count <= 0 {
				continue
			}
			if load.n == 0 {
				load = loadOf(s, 0)
			}
			if load.n < 3 {
				load.n++
			}
		}
		st.load = load
	case componentBucketEntityData:
		v, ok := readNetNBT(r)
		if !ok {
			return false
		}
		m, _ := v.(map[string]any)
		if _, isBucket := speciesByMobBucket[st.item]; isBucket && m != nil {
			if hp, ok := snbtFloat(m["Health"]); ok && hp >= 0 {
				st.cube.health = int32(hp) + 1
			}
			if age, ok := snbtInt(m["Age"]); ok {
				st.cube.age = int32(age)
			}
		}
	case componentSalmonSize, componentAxolotlVariant:
		v, err := protocol.ReadVarInt(r)
		if err != nil || v < 0 {
			return false
		}
		d.variant = v + 1
	case componentFishPattern, componentFishBaseColor, componentFishPatternColor:
		v, err := protocol.ReadVarInt(r)
		if err != nil || v < 0 {
			return false
		}
		d.fishSeen = true
		switch id {
		case componentFishPattern:
			d.fish[0] = v
		case componentFishBaseColor:
			d.fish[1] = v
		default:
			d.fish[2] = v
		}
	case componentBlockState:
		n, err := protocol.ReadVarInt(r)
		if err != nil || n < 0 || n > 64 {
			return false
		}
		for i := int32(0); i < n; i++ {
			k, e1 := protocol.ReadString(r)
			v, e2 := protocol.ReadString(r)
			if e1 != nil || e2 != nil {
				return false
			}
			if k == "copper_golem_pose" {
				for j, p := range statuePoses {
					if p == v {
						st.golemPose = int8(j + 1)
					}
				}
			}
		}
	case componentEntityData:
		v, ok := readNetNBT(r)
		if !ok {
			return false
		}
		if m, isMap := v.(map[string]any); isMap && st.item == itemArmorStand {
			if tags, ok := standTagsFromEntityData(m); ok {
				st.standTags = tags
			}
		}
	case componentTooltipDisplay:
		if _, err := r.ReadByte(); err != nil { // hide the whole tooltip
			return false
		}
		n, err := protocol.ReadVarInt(r)
		if err != nil || n < 0 || n > 256 {
			return false
		}
		for i := int32(0); i < n; i++ {
			if _, err := protocol.ReadVarInt(r); err != nil {
				return false
			}
		}
	case componentRarity:
		if _, err := protocol.ReadVarInt(r); err != nil {
			return false
		}
	case componentProfile:
		p, ok := readProfile(r)
		if !ok {
			return false
		}
		if st.item == itemPlayerHead {
			st.profile = profileString(p)
		}
	case componentBlockEntityData:
		v, ok := readNetNBT(r)
		m, isMap := v.(map[string]any)
		if !ok || !isMap {
			return false
		}
		name, _ := m["id"].(string)
		id, ok := blockEntityTypeByName(name)
		if !ok {
			return false
		}
		kv := map[string]any{}
		for k, e := range m {
			kv[k] = e
		}
		kv["id"] = int64(id)
		d.setBE(kv)
	case componentSignTextFront, componentSignTextBack:
		side, ok := readSignText(r)
		if !ok {
			return false
		}
		key := "front_text"
		if id == componentSignTextBack {
			key = "back_text"
		}
		d.setBE(map[string]any{key: side})
	case componentWaxed:
		d.setBE(map[string]any{"is_waxed": true})
	case componentNoteBlockSound:
		s, err := protocol.ReadString(r) // Identifier.STREAM_CODEC
		if err != nil {
			return false
		}
		id, ok := parseResID(s)
		if !ok {
			return false
		}
		st.noteSound = id
	default:
		return false
	}
	return true
}

// potionContents reads potion_contents: the optional potion holder, the
// optional custom colour (a fixed-width int), the effects and the optional
// custom name. The kind is settled in finish, once the stack's name is
// known too.
func (d *stackDecode) potionContents(r *bytes.Reader) bool {
	has, err := r.ReadByte()
	if err != nil {
		return false
	}
	held := int32(0)
	if has != 0 {
		v, err := protocol.ReadVarInt(r) // Potion.STREAM_CODEC: the registry id
		if err != nil || v < 0 {
			return false
		}
		held = v + 1
	}
	if has, err = r.ReadByte(); err != nil {
		return false
	}
	if has != 0 {
		var rgb [4]byte
		if _, err := io.ReadFull(r, rgb[:]); err != nil {
			return false
		}
	}
	n, err := protocol.ReadVarInt(r)
	if err != nil || n < 0 || n > 256 {
		return false
	}
	var effs []potEffect
	for i := int32(0); i < n; i++ {
		holder, err := protocol.ReadVarInt(r)
		if err != nil {
			return false
		}
		amp, ticks, ok := readEffectDetails(r, 0)
		if !ok {
			return false
		}
		// MobEffect.STREAM_CODEC: holderRegistry, the plain registry id.
		effs = append(effs, potEffect{id: holder, amp: int(amp), ticks: int(ticks)})
	}
	if has, err = r.ReadByte(); err != nil { // custom name
		return false
	}
	if has != 0 {
		if _, err := protocol.ReadString(r); err != nil {
			return false
		}
	}
	d.potSeen, d.potHeld, d.potEffs = true, held, effs
	return true
}

// readEffectDetails reads MobEffectInstance.Details: amplifier, duration,
// three flags and an optional hidden Details under it.
func readEffectDetails(r *bytes.Reader, depth int) (amp, ticks int32, ok bool) {
	if depth > 4 {
		return 0, 0, false
	}
	amp, e1 := protocol.ReadVarInt(r)
	ticks, e2 := protocol.ReadVarInt(r)
	if e1 != nil || e2 != nil {
		return 0, 0, false
	}
	var flags [3]byte
	if _, err := io.ReadFull(r, flags[:]); err != nil {
		return 0, 0, false
	}
	hidden, err := r.ReadByte()
	if err != nil {
		return 0, 0, false
	}
	if hidden != 0 {
		if _, _, ok := readEffectDetails(r, depth+1); !ok {
			return 0, 0, false
		}
	}
	return amp, ticks, true
}

// finish settles what needed the whole patch.
func (d *stackDecode) finish() invStack {
	st := d.st
	if len(d.be) > 0 {
		if _, has := d.be["id"]; !has { // sign components alone: the sign's own type
			if id, ok := signItemBlockEntity(st.item); ok {
				d.setBE(map[string]any{"id": int64(id)})
			}
		}
		if _, has := d.be["id"]; has {
			st.beData = writeSNBT(d.be)
		}
	}
	if d.potSeen && st.item != itemOminousBottle {
		st.potion = d.potionKind(st)
		if d.potHeld > 0 && st.name == "" && st.potion != potNone &&
			(st.item == itemPotion || st.item == itemSplashPotion || st.item == itemLingerPotion) {
			// A brewed bottle carries its label as its name (brewing.go);
			// a picked one gets the same.
			st.name = potionName(st.potion, st.item)
		}
	}
	if d.ominousName && st.item == itemWhiteBanner {
		st.ominous = true // its eight layers are the flag's, not the stack's
		st.pats = [6]bannerLayer{}
	}
	if etype, ok := speciesByMobBucket[st.item]; ok {
		switch {
		case etype == entityTropicalFish && d.fishSeen:
			st.cube.variant = (d.fish[0]&0xffff | (d.fish[1]&0xff)<<16 | (d.fish[2]&0xff)<<24) + 1
		case (etype == entitySalmon || etype == entityAxolotl) && d.variant > 0:
			st.cube.variant = d.variant
		}
	}
	return st
}

// potionKind is the engine's potion for what potion_contents said: the
// holder's potion when it names one, else the kind whose effects these are
// (the engine writes effects only). Kinds that share their effects — the
// four plain bottles — are told apart by the stack's name.
func (d *stackDecode) potionKind(st invStack) int8 {
	if d.potHeld > 0 && int(d.potHeld) <= len(potionRegistryOrder) {
		if k, ok := potionByVanillaName[potionRegistryOrder[d.potHeld-1]]; ok {
			return k
		}
	}
	var match []int8
	for k := int8(potNone + 1); k < potCount; k++ {
		if effectsMatch(potionEffects(k), d.potEffs) {
			match = append(match, k)
		}
	}
	if len(match) == 0 {
		return potNone
	}
	for _, k := range match {
		if st.name != "" && potionName(k, st.item) == st.name {
			return k
		}
	}
	return match[0]
}

// effectsMatch compares a kind's effects with those read off the wire, as
// potionComponentBytes writes them (a zero duration goes out as one tick).
func effectsMatch(def, got []potEffect) bool {
	if len(def) != len(got) {
		return false
	}
	for i, e := range def {
		ticks := e.ticks
		if ticks == 0 {
			ticks = 1
		}
		if e.id != got[i].id || e.amp != got[i].amp || ticks != got[i].ticks {
			return false
		}
	}
	return true
}

// stewRowFor is the stew row for an effect and duration (in ticks); the
// first row with the effect when none has that duration.
func stewRowFor(effect, ticks int32) int8 {
	code := int8(0)
	for i, e := range stewEffects {
		if e.effect != effect {
			continue
		}
		if int32(e.secs*20) == ticks {
			return int8(i + 1)
		}
		if code == 0 {
			code = int8(i + 1)
		}
	}
	return code
}

// internStars is the star store's id for a burst list (0 for none).
func (h *hub) internStars(bursts []fireworkBurst) int32 {
	if len(bursts) == 0 || h.stars == nil {
		return 0
	}
	return h.stars.intern(bursts)
}

// readBurst reads one FireworkExplosion as appendBurst writes it.
func readBurst(r *bytes.Reader) (fireworkBurst, bool) {
	shape, err := protocol.ReadVarInt(r)
	if err != nil || shape < 0 || shape > burstBurst {
		return fireworkBurst{}, false
	}
	b := fireworkBurst{Shape: int8(shape)}
	for i := 0; i < 2; i++ {
		n, err := protocol.ReadVarInt(r)
		if err != nil || n < 0 || n > 256 {
			return fireworkBurst{}, false
		}
		var list []int32
		for j := int32(0); j < n; j++ {
			var c [4]byte
			if _, err := io.ReadFull(r, c[:]); err != nil {
				return fireworkBurst{}, false
			}
			list = append(list, int32(binary.BigEndian.Uint32(c[:])))
		}
		if i == 0 {
			b.Colors = list
		} else {
			b.Fade = list
		}
	}
	var flags [2]byte
	if _, err := io.ReadFull(r, flags[:]); err != nil {
		return fireworkBurst{}, false
	}
	b.Trail, b.Twinkle = flags[0] != 0, flags[1] != 0
	return b, true
}

// readBursts reads a counted list of bursts.
func readBursts(r *bytes.Reader, limit int32) ([]fireworkBurst, bool) {
	n, err := protocol.ReadVarInt(r)
	if err != nil || n < 0 || n > limit {
		return nil, false
	}
	var out []fireworkBurst
	for i := int32(0); i < n; i++ {
		b, ok := readBurst(r)
		if !ok {
			return nil, false
		}
		out = append(out, b)
	}
	return out, true
}

// skipOptString skips an Optional<String> (Filterable's filtered text).
func skipOptString(r *bytes.Reader) bool {
	has, err := r.ReadByte()
	if err != nil {
		return false
	}
	if has != 0 {
		if _, err := protocol.ReadString(r); err != nil {
			return false
		}
	}
	return true
}

// dimByRegistryName is the dimension whose registry key a GlobalPos names
// (the overworld for one the table does not have).
func dimByRegistryName(name string) int {
	for id, d := range world.Dimensions {
		if d.Key == name {
			return id
		}
	}
	return dimOverworld
}

// readTextNBT reads a network-NBT text component as plain text.
func readTextNBT(r *bytes.Reader) (string, bool) {
	v, ok := readNetNBT(r)
	if !ok {
		return "", false
	}
	return textOf(v)
}

// NBT tag types.
const (
	nbtTagEnd = iota
	nbtTagByte
	nbtTagShort
	nbtTagInt
	nbtTagLong
	nbtTagFloat
	nbtTagDouble
	nbtTagByteArray
	nbtTagString
	nbtTagList
	nbtTagCompound
	nbtTagIntArray
	nbtTagLongArray
)

// readNetNBT reads one network NBT value (a nameless root: the type, then
// the payload) into the shapes the SNBT parser produces — whole numbers as
// int64, fractions as float64, strings, []any lists and arrays, and
// map[string]any compounds — so the readers written for command arguments
// read it too.
func readNetNBT(r *bytes.Reader) (any, bool) {
	t, err := r.ReadByte()
	if err != nil || t == nbtTagEnd {
		return nil, false
	}
	return readNBTPayload(r, t, 0)
}

func readNBTPayload(r *bytes.Reader, t byte, depth int) (any, bool) {
	if depth > 32 {
		return nil, false
	}
	fixed := func(n int) ([]byte, bool) {
		if n < 0 || n > r.Len() {
			return nil, false
		}
		b := make([]byte, n)
		_, err := io.ReadFull(r, b)
		return b, err == nil
	}
	count := func() (int, bool) {
		b, ok := fixed(4)
		if !ok {
			return 0, false
		}
		n := int(int32(binary.BigEndian.Uint32(b)))
		return n, n >= 0 && n <= r.Len()
	}
	switch t {
	case nbtTagByte:
		b, ok := fixed(1)
		if !ok {
			return nil, false
		}
		return int64(int8(b[0])), true
	case nbtTagShort:
		b, ok := fixed(2)
		if !ok {
			return nil, false
		}
		return int64(int16(binary.BigEndian.Uint16(b))), true
	case nbtTagInt:
		b, ok := fixed(4)
		if !ok {
			return nil, false
		}
		return int64(int32(binary.BigEndian.Uint32(b))), true
	case nbtTagLong:
		b, ok := fixed(8)
		if !ok {
			return nil, false
		}
		return int64(binary.BigEndian.Uint64(b)), true
	case nbtTagFloat:
		b, ok := fixed(4)
		if !ok {
			return nil, false
		}
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b))), true
	case nbtTagDouble:
		b, ok := fixed(8)
		if !ok {
			return nil, false
		}
		return math.Float64frombits(binary.BigEndian.Uint64(b)), true
	case nbtTagString:
		l, ok := fixed(2)
		if !ok {
			return nil, false
		}
		s, ok := fixed(int(binary.BigEndian.Uint16(l)))
		if !ok {
			return nil, false
		}
		return string(s), true
	case nbtTagByteArray, nbtTagIntArray, nbtTagLongArray:
		n, ok := count()
		if !ok {
			return nil, false
		}
		elem := map[byte]byte{nbtTagByteArray: nbtTagByte, nbtTagIntArray: nbtTagInt, nbtTagLongArray: nbtTagLong}[t]
		out := make([]any, 0, n)
		for i := 0; i < n; i++ {
			v, ok := readNBTPayload(r, elem, depth+1)
			if !ok {
				return nil, false
			}
			out = append(out, v)
		}
		return out, true
	case nbtTagList:
		et, err := r.ReadByte()
		if err != nil {
			return nil, false
		}
		n, ok := count()
		if !ok {
			return nil, false
		}
		out := make([]any, 0, n)
		for i := 0; i < n; i++ {
			if et == nbtTagEnd {
				return nil, false
			}
			v, ok := readNBTPayload(r, et, depth+1)
			if !ok {
				return nil, false
			}
			out = append(out, v)
		}
		return out, true
	case nbtTagCompound:
		m := map[string]any{}
		for {
			ct, err := r.ReadByte()
			if err != nil {
				return nil, false
			}
			if ct == nbtTagEnd {
				return m, true
			}
			l, ok := fixed(2)
			if !ok {
				return nil, false
			}
			name, ok := fixed(int(binary.BigEndian.Uint16(l)))
			if !ok {
				return nil, false
			}
			v, ok := readNBTPayload(r, ct, depth+1)
			if !ok {
				return nil, false
			}
			m[string(name)] = v
		}
	}
	return nil, false
}
