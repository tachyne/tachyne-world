package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The item components nothing in survival play produces, only a command or a
// map-maker's loot table: lore, unbreakable, and adventure mode's can_break
// and can_place_on (AdventureModePredicate). They ride invStack.tags.

// advPred is one BlockPredicate of an AdventureModePredicate: the blocks it
// names (a list of ids, or one #tag), and the state properties a block must
// have (an exact value, or a min/max range).
type advPred struct {
	Blocks []string            `json:"blocks,omitempty"`
	Tag    string              `json:"tag,omitempty"`
	State  map[string]advRange `json:"state,omitempty"`
}

// advRange is StatePropertiesPredicate's matcher for one property: Exact,
// or Min and/or Max ("" = open).
type advRange struct {
	Exact string `json:"exact,omitempty"`
	Min   string `json:"min,omitempty"`
	Max   string `json:"max,omitempty"`
}

// parseAdvPredicate reads an AdventureModePredicate component value: one
// BlockPredicate compound, or a non-empty list of them (compactListCodec).
// The block entity half of a predicate (nbt, components) has nothing to
// match against on the session side, where digs and clicks are decided, so
// it is refused by name. Returns the JSON the stack stores.
func parseAdvPredicate(key string, v any) (string, string) {
	var raw []any
	switch x := v.(type) {
	case map[string]any:
		raw = []any{x}
	case []any:
		if len(x) == 0 {
			return "", fmt.Sprintf("Malformed '%s' component: '%s'", key, "list must not be empty")
		}
		raw = x
	default:
		return "", fmt.Sprintf("Malformed '%s' component", key)
	}
	preds := make([]advPred, 0, len(raw))
	for _, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			return "", fmt.Sprintf("Malformed '%s' component", key)
		}
		var p advPred
		for field, val := range m {
			switch field {
			case "blocks":
				switch b := val.(type) {
				case string:
					if tag, ok := strings.CutPrefix(b, "#"); ok {
						tag = strings.TrimPrefix(tag, "minecraft:")
						if _, ok := blockTagMembers(tag); !ok {
							return "", "Unknown block tag '" + nsID(tag) + "'"
						}
						p.Tag = tag
					} else {
						p.Blocks = []string{b}
					}
				case []any:
					for _, e := range b {
						s, ok := e.(string)
						if !ok {
							return "", fmt.Sprintf("Malformed '%s' component", key)
						}
						p.Blocks = append(p.Blocks, s)
					}
				default:
					return "", fmt.Sprintf("Malformed '%s' component", key)
				}
				for i, b := range p.Blocks {
					b = strings.TrimPrefix(b, "minecraft:")
					if _, _, ok := worldgen.BlockRangeOK(b); !ok {
						return "", fmt.Sprintf("Unknown block type '%s'", nsID(b))
					}
					p.Blocks[i] = b
				}
			case "state":
				sm, ok := val.(map[string]any)
				if !ok {
					return "", fmt.Sprintf("Malformed '%s' component", key)
				}
				p.State = map[string]advRange{}
				for prop, want := range sm {
					switch w := want.(type) {
					case map[string]any:
						var r advRange
						r.Min = propText(w["min"])
						r.Max = propText(w["max"])
						p.State[prop] = r
					default:
						s := propText(w)
						if s == "" {
							return "", fmt.Sprintf("Malformed '%s' component", key)
						}
						p.State[prop] = advRange{Exact: s}
					}
				}
			case "nbt", "components", "predicates":
				return "", fmt.Sprintf("The '%s' field of a '%s' block predicate is not supported here", field, key)
			default:
				return "", fmt.Sprintf("Malformed '%s' component", key)
			}
		}
		preds = append(preds, p)
	}
	b, _ := json.Marshal(preds)
	return string(b), ""
}

// propText is a property value as the text a state carries ("" for none).
func propText(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// advCache holds parsed predicate lists by their JSON: the session side
// tests a held stack's list on every dig and click.
var advCache sync.Map // string → []advPred

func advPredsOf(js string) []advPred {
	if js == "" {
		return nil
	}
	if v, ok := advCache.Load(js); ok {
		return v.([]advPred)
	}
	var preds []advPred
	if json.Unmarshal([]byte(js), &preds) != nil {
		preds = nil
	}
	advCache.Store(js, preds)
	return preds
}

// advAllows is AdventureModePredicate.test on a block state: any of the
// predicates matches it. An empty list allows nothing.
func advAllows(js string, state uint32) bool {
	for _, p := range advPredsOf(js) {
		if p.matches(state) {
			return true
		}
	}
	return false
}

func (p advPred) matches(state uint32) bool {
	name, ok := worldgen.StateName(state)
	if !ok {
		return false
	}
	if p.Tag != "" {
		members, ok := blockTagMembers(p.Tag)
		if !ok {
			return false
		}
		in := false
		for _, m := range members {
			in = in || m == name
		}
		if !in {
			return false
		}
	} else if len(p.Blocks) > 0 {
		in := false
		for _, b := range p.Blocks {
			in = in || b == name
		}
		if !in {
			return false
		}
	}
	if len(p.State) == 0 {
		return true
	}
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return false
	}
	for prop, r := range p.State {
		if !info.HasProperty(prop) {
			return false // vanilla: a property the block does not have fails
		}
		have := worldgen.GetProperty(info, state, prop)
		switch {
		case r.Exact != "":
			if have != r.Exact {
				return false
			}
		default:
			if r.Min != "" && propLess(have, r.Min) {
				return false
			}
			if r.Max != "" && propLess(r.Max, have) {
				return false
			}
		}
	}
	return true
}

// propLess orders two property values: integers numerically, anything else
// as text.
func propLess(a, b string) bool {
	x, err1 := strconv.Atoi(a)
	y, err2 := strconv.Atoi(b)
	if err1 == nil && err2 == nil {
		return x < y
	}
	return a < b
}

// wearMax is the stack's durability when it wears at all
// (ItemStack.isDamageableItem): a max_damage, and not unbreakable.
func wearMax(st invStack) (int, bool) {
	max, ok := itemMaxDurability[st.item]
	return max, ok && !st.tags.unbreakable
}

// loreLines are the stack's lore lines.
func (t itemTags) loreLines() []string {
	if t.lore == "" {
		return nil
	}
	return strings.Split(t.lore, "\n")
}

// Canonical (1.21.5) ids of adventure mode's two predicates.
const (
	componentCanPlaceOn = 11 // can_place_on: AdventureModePredicate
	componentCanBreak   = 12 // can_break: AdventureModePredicate
)

// tagComponents are the item tags a client is sent: the lore, unbreakable
// (a Unit, which puts "Unbreakable" in the tooltip), and can_break /
// can_place_on (the tooltip's "Can break:" / "Can be placed on:" lists).
func tagComponents(st invStack) (int32, []byte) {
	var n int32
	var b []byte
	if lines := st.tags.loreLines(); len(lines) > 0 {
		b = protocol.AppendVarInt(b, componentLore)
		b = protocol.AppendVarInt(b, int32(len(lines)))
		for _, l := range lines {
			b = append(b, chatNBT(l)...)
		}
		n++
	}
	if st.tags.unbreakable {
		b = protocol.AppendVarInt(b, componentUnbreakable) // no payload
		n++
	}
	for _, c := range []struct {
		id int32
		js string
	}{{componentCanBreak, st.tags.canBreak}, {componentCanPlaceOn, st.tags.canPlace}} {
		if pb, ok := advPredicateBytes(c.js); ok {
			b = protocol.AppendVarInt(b, c.id)
			b = append(b, pb...)
			n++
		}
	}
	return n, b
}

// advPredicateBytes is a stored predicate list as AdventureModePredicate's
// STREAM_CODEC writes it: a list of BlockPredicates, each an optional
// HolderSet<Block> (0 then the tag's name, or the count + 1 then block
// registry ids), optional StatePropertiesPredicate (a list of property
// name + either an exact value or an optional min and max), no NbtPredicate
// and empty DataComponentMatchers (an exact list and a partial list, both
// empty). Properties go out sorted by name. False when there is nothing to
// send.
func advPredicateBytes(js string) ([]byte, bool) {
	preds := advPredsOf(js)
	if len(preds) == 0 {
		return nil, false
	}
	b := protocol.AppendVarInt(nil, int32(len(preds)))
	for _, p := range preds {
		switch {
		case p.Tag != "":
			b = append(b, 1)
			b = protocol.AppendVarInt(b, 0)
			b = protocol.AppendString(b, nsID(p.Tag))
		case len(p.Blocks) > 0:
			var ids []int32
			for _, name := range p.Blocks {
				if id, ok := worldgen.BlockRegistryID(name); ok {
					ids = append(ids, int32(id))
				}
			}
			b = append(b, 1)
			b = protocol.AppendVarInt(b, int32(len(ids))+1)
			for _, id := range ids {
				b = protocol.AppendVarInt(b, id)
			}
		default:
			b = append(b, 0)
		}
		if len(p.State) == 0 {
			b = append(b, 0)
		} else {
			props := make([]string, 0, len(p.State))
			for k := range p.State {
				props = append(props, k)
			}
			sort.Strings(props)
			b = append(b, 1)
			b = protocol.AppendVarInt(b, int32(len(props)))
			for _, k := range props {
				r := p.State[k]
				b = protocol.AppendString(b, k)
				if r.Exact != "" {
					b = append(b, 1)
					b = protocol.AppendString(b, r.Exact)
					continue
				}
				b = append(b, 0)
				for _, v := range []string{r.Min, r.Max} {
					b = protocol.AppendBool(b, v != "")
					if v != "" {
						b = protocol.AppendString(b, v)
					}
				}
			}
		}
		b = append(b, 0)    // no nbt
		b = append(b, 0, 0) // no component matchers: exact, partial
	}
	return b, true
}

// readAdvPredicate reads what advPredicateBytes writes back into the JSON a
// stack stores. A predicate the engine cannot keep — an nbt or component
// matcher, a block or tag it does not know — fails the whole component, as
// /give refuses one.
func readAdvPredicate(r *bytes.Reader) (string, bool) {
	n, err := protocol.ReadVarInt(r)
	if err != nil || n <= 0 || n > 64 {
		return "", false
	}
	flag := func() (bool, bool) {
		v, err := r.ReadByte()
		return v == 1, err == nil && v <= 1
	}
	preds := make([]advPred, 0, n)
	for i := int32(0); i < n; i++ {
		var p advPred
		has, ok := flag()
		if !ok {
			return "", false
		}
		if has {
			k, err := protocol.ReadVarInt(r)
			if err != nil || k < 0 || k > 4097 {
				return "", false
			}
			if k == 0 {
				tag, err := protocol.ReadString(r)
				if err != nil {
					return "", false
				}
				tag = strings.TrimPrefix(tag, "minecraft:")
				if _, ok := blockTagMembers(tag); !ok {
					return "", false
				}
				p.Tag = tag
			}
			for j := int32(0); j < k-1; j++ {
				id, err := protocol.ReadVarInt(r)
				if err != nil || id < 0 {
					return "", false
				}
				name, ok := worldgen.BlockRegistryName(uint32(id))
				if !ok {
					return "", false
				}
				p.Blocks = append(p.Blocks, name)
			}
		}
		if has, ok = flag(); !ok {
			return "", false
		}
		if has {
			m, err := protocol.ReadVarInt(r)
			if err != nil || m < 0 || m > 64 {
				return "", false
			}
			p.State = map[string]advRange{}
			for j := int32(0); j < m; j++ {
				prop, err := protocol.ReadString(r)
				if err != nil {
					return "", false
				}
				exact, ok := flag()
				if !ok {
					return "", false
				}
				var rg advRange
				if exact {
					if rg.Exact, err = protocol.ReadString(r); err != nil {
						return "", false
					}
				} else {
					for _, dst := range []*string{&rg.Min, &rg.Max} {
						present, ok := flag()
						if !ok {
							return "", false
						}
						if present {
							if *dst, err = protocol.ReadString(r); err != nil {
								return "", false
							}
						}
					}
				}
				p.State[prop] = rg
			}
		}
		if has, ok = flag(); !ok || has { // an nbt predicate is not kept
			return "", false
		}
		for j := 0; j < 2; j++ { // component matchers: exact, partial
			if c, err := protocol.ReadVarInt(r); err != nil || c != 0 {
				return "", false
			}
		}
		preds = append(preds, p)
	}
	js, err := json.Marshal(preds)
	if err != nil {
		return "", false
	}
	return string(js), true
}

// textOf reads a text component value as plain text: a string, or a
// compound's text (and its extra's, in order).
func textOf(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case map[string]any:
		s, ok := x["text"].(string)
		if !ok {
			if tr, ok2 := x["translate"].(string); ok2 {
				s, ok = tr, true
			}
		}
		if !ok {
			return "", false
		}
		if extra, ok := x["extra"].([]any); ok {
			for _, e := range extra {
				t, ok := textOf(e)
				if !ok {
					return "", false
				}
				s += t
			}
		}
		return s, true
	case []any:
		var s string
		for _, e := range x {
			t, ok := textOf(e)
			if !ok {
				return "", false
			}
			s += t
		}
		return s, true
	}
	return "", false
}

// adventureMayBreak is Player.blockActionRestricted's adventure branch: the
// main-hand stack is not empty and its can_break matches the block.
func adventureMayBreak(p *player, mode int, state uint32) bool {
	return mode == gmAdventure && p.heldItem() != 0 && advAllows(p.handTagsOf(false).canBreak, state)
}

// adventureMayPlaceOn is ItemStack.useOn's adventure test: the hand's stack
// may be used against the clicked block when its can_place_on matches it.
func adventureMayPlaceOn(p *player, mode int, off bool, clicked uint32) bool {
	return mode == gmAdventure && p.handItem(off) != 0 && advAllows(p.handTagsOf(off).canPlace, clicked)
}

// applyItemTagComponent sets the components past the everyday ones
// (applyItemComponent's switch): lore, unbreakable, can_break,
// can_place_on, trim, banner_patterns, fireworks, firework_explosion, the
// two book contents, charged_projectiles and bucket_entity_data. Any other
// component is refused by name.
func applyItemTagComponent(st *invStack, key string, v any) string {
	bad := func() string { return fmt.Sprintf("Malformed '%s' component", key) }
	switch key {
	case "minecraft:lore":
		list, ok := v.([]any)
		if !ok || len(list) > 256 { // ItemLore.MAX_LINES
			return bad()
		}
		lines := make([]string, 0, len(list))
		for _, l := range list {
			s, ok := textOf(l)
			if !ok || strings.Contains(s, "\n") {
				return bad()
			}
			lines = append(lines, s)
		}
		st.tags.lore = strings.Join(lines, "\n")
	case "minecraft:unbreakable":
		if _, ok := v.(map[string]any); !ok { // a Unit: {}
			return bad()
		}
		st.tags.unbreakable = true
	case "minecraft:can_break", "minecraft:can_place_on":
		js, msg := parseAdvPredicate(key, v)
		if msg != "" {
			return msg
		}
		if key == "minecraft:can_break" {
			st.tags.canBreak = js
		} else {
			st.tags.canPlace = js
		}
	case "minecraft:trim":
		m, ok := v.(map[string]any)
		if !ok {
			return bad()
		}
		mat, _ := m["material"].(string)
		pat, _ := m["pattern"].(string)
		mi, ok1 := registryIDs("minecraft:trim_material")[nsID(mat)]
		pi, ok2 := registryIDs("minecraft:trim_pattern")[nsID(pat)]
		if !ok1 {
			return fmt.Sprintf("Can't find element '%s' of type 'minecraft:trim_material'", nsID(mat))
		}
		if !ok2 {
			return fmt.Sprintf("Can't find element '%s' of type 'minecraft:trim_pattern'", nsID(pat))
		}
		st.trimMat, st.trimPat = int8(mi+1), int8(pi+1)
	case "minecraft:banner_patterns":
		layers, ok := bannerLayersFromNBT(v)
		if !ok {
			return bad()
		}
		if len(layers) > len(st.pats) {
			return fmt.Sprintf("The '%s' component holds at most %d layers here", key, len(st.pats))
		}
		st.pats = [6]bannerLayer{}
		for i, l := range layers {
			id, _ := bannerPatternIDFor(l.Pattern)
			st.pats[i] = bannerLayer{patPlus1: id + 1, color: int8(dyeIndex(l.Color))}
		}
	case "minecraft:firework_explosion":
		b, ok := burstFromNBT(v)
		if !ok {
			return bad()
		}
		st.starID = internBursts([]fireworkBurst{b})
	case "minecraft:fireworks":
		m, ok := v.(map[string]any)
		if !ok {
			return bad()
		}
		if f, ok := snbtInt(m["flight_duration"]); ok {
			if f < 0 || f > 255 {
				return bad()
			}
			st.flight = int8(min(f, 127))
		}
		var bursts []fireworkBurst
		if list, ok := m["explosions"].([]any); ok {
			if len(list) > 256 { // Fireworks.MAX_EXPLOSIONS
				return bad()
			}
			for _, e := range list {
				b, ok := burstFromNBT(e)
				if !ok {
					return bad()
				}
				bursts = append(bursts, b)
			}
		}
		st.starID = internBursts(bursts)
	case "minecraft:written_book_content", "minecraft:writable_book_content":
		m, ok := v.(map[string]any)
		if !ok {
			return bad()
		}
		// The book's contents render from the item it is on: a written
		// book's reader, a book and quill's editor.
		if want := map[string]int32{"minecraft:written_book_content": itemWrittenBook,
			"minecraft:writable_book_content": itemWritableBook}[key]; st.item != want {
			return fmt.Sprintf("The '%s' component is not supported here", key)
		}
		var b savedBook
		pages, _ := m["pages"].([]any)
		if len(pages) > bookMaxPages {
			return bad()
		}
		for _, p := range pages {
			if f, ok := p.(map[string]any); ok && f["raw"] != nil { // Filterable: {raw, filtered}
				p = f["raw"]
			}
			s, ok := textOf(p)
			if !ok {
				return bad()
			}
			b.Pages = append(b.Pages, s)
		}
		if key == "minecraft:written_book_content" {
			t := m["title"]
			if f, ok := t.(map[string]any); ok {
				t = f["raw"]
			}
			title, ok1 := t.(string)
			author, ok2 := m["author"].(string)
			if !ok1 || !ok2 || title == "" || len(title) > bookMaxTitle {
				return bad()
			}
			b.Title, b.Author = title, author
			if g, ok := snbtInt(m["generation"]); ok {
				if g < 0 || g > 3 {
					return bad()
				}
				b.Gen = int(g)
			}
		}
		bs := globalBooks.Load()
		if bs == nil {
			return fmt.Sprintf("The '%s' component is not supported here", key)
		}
		st.bookID = bs.create(b)
	case "minecraft:charged_projectiles":
		list, ok := v.([]any)
		if !ok {
			return bad()
		}
		st.load = xbowLoad{}
		for _, raw := range list {
			m, ok := raw.(map[string]any)
			if !ok {
				return bad()
			}
			ammo, ok := stackFromNBT(m)
			if !ok {
				return bad()
			}
			l := loadOf(ammo, ammo.count)
			switch {
			case st.load.n == 0:
				st.load = l
			case l.item == st.load.item && l.potion == st.load.potion && l.flight == st.load.flight && l.starID == st.load.starID:
				st.load.n += l.n
			default:
				return fmt.Sprintf("The '%s' component holds one kind of projectile here", key)
			}
		}
	case "minecraft:bucket_entity_data":
		m, ok := v.(map[string]any)
		if !ok {
			return bad()
		}
		if _, isBucket := speciesByMobBucket[st.item]; !isBucket {
			return fmt.Sprintf("The '%s' component is not supported here", key)
		}
		if hp, ok := snbtFloat(m["Health"]); ok && hp >= 0 {
			st.cube.health = int32(hp) + 1
		}
		if vr, ok := snbtInt(m["BucketVariantTag"]); ok {
			st.cube.variant = int32(vr) + 1
		} else if vr, ok := snbtInt(m["Variant"]); ok {
			st.cube.variant = int32(vr) + 1
		}
		if age, ok := snbtInt(m["Age"]); ok {
			st.cube.age = int32(age)
		}
	default:
		return fmt.Sprintf("The '%s' component is not supported here", key)
	}
	return ""
}

// burstShapes are FireworkExplosion.Shape's names by id.
var burstShapes = []string{"small_ball", "large_ball", "star", "creeper", "burst"}

// burstFromNBT reads one FireworkExplosion: its shape, the colours it opens
// with and fades to (int arrays of rgb), and the trail and twinkle flags.
func burstFromNBT(v any) (fireworkBurst, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return fireworkBurst{}, false
	}
	var b fireworkBurst
	shape, _ := m["shape"].(string)
	b.Shape = -1
	for i, n := range burstShapes {
		if n == strings.TrimPrefix(shape, "minecraft:") {
			b.Shape = int8(i)
		}
	}
	if b.Shape < 0 {
		return fireworkBurst{}, false
	}
	for key, dst := range map[string]*[]int32{"colors": &b.Colors, "fade_colors": &b.Fade} {
		if raw, ok := m[key]; ok {
			list, ok := raw.([]any)
			if !ok {
				return fireworkBurst{}, false
			}
			for _, c := range list {
				n, ok := snbtInt(c)
				if !ok {
					return fireworkBurst{}, false
				}
				*dst = append(*dst, int32(n))
			}
		}
	}
	if t, ok := snbtInt(m["has_trail"]); ok {
		b.Trail = t != 0
	}
	if t, ok := snbtInt(m["has_twinkle"]); ok {
		b.Twinkle = t != 0
	}
	return b, true
}

// internBursts is the star store's id for a burst list (0 for none).
func internBursts(bursts []fireworkBurst) int32 {
	ss := globalStars.Load()
	if ss == nil || len(bursts) == 0 {
		return 0
	}
	return ss.intern(bursts)
}
