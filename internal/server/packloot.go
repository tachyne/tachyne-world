package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Data pack loot tables, predicates and item modifiers, read at load time
// into the loot IR the generated tables use (blockloot.go), so a pack's
// table rolls through the same evaluators: a chest, barrel, dispenser or
// /loot table through the chest evaluator, a block's drops and a mob's
// death drops through the block/entity one. A pack table overrides the
// vanilla one of its id — blocks/<block> for a block (and its wall form),
// entities/<type> for a mob, any other id for /loot and the containers
// that name it. A file that does not parse stands as an empty table, as an
// element missing from the loot table registry rolls LootTable.EMPTY.
//
// Read: the 26.x shape (a pool's or entry's "condition", "modifier", a
// condition's or function's "type", a predicate or item modifier named by
// id) and the older 1.21 shape alike. Conditions: inverted, any_of, all_of,
// random_chance (a constant), random_chance_with_enchanted_bonus,
// survives_explosion, table_bonus, match_block, match_tool (items, tags,
// silk touch), killed_by_player, entity_properties (on fire, type),
// damage_source_properties (tags, direct and source type), location_check
// (biomes, tags too) and weather_check. Functions: set_count, limit_count,
// explosion_decay, apply_bonus, enchanted_count_increase, furnace_smelt,
// set_potion, enchant_randomly, set_enchantments, enchant_with_levels,
// set_damage, set_name, set_stew_effect, set_ominous_bottle_amplifier,
// set_instrument. Entries: item, empty, alternatives, group, sequence,
// loot_table (an id or an inline table) and tag. A condition this server
// cannot evaluate never holds; a function it cannot apply is left out (the
// item comes plainer); both are logged with the table.

// vanillaPredicates are the vanilla pack's predicate files, which a pack's
// tables name by id (minecraft:tool/can_silk_touch …).
var vanillaPredicates = map[string]string{
	"minecraft:block/fast_cooking":  `{"type":"minecraft:match_block","blocks":["minecraft:smoker","minecraft:blast_furnace"]}`,
	"minecraft:tool/can_shear":      `{"type":"minecraft:match_tool","predicate":{"items":"minecraft:shears"}}`,
	"minecraft:tool/can_silk_touch": `{"type":"minecraft:match_tool","predicate":{"predicates":{"minecraft:enchantments":[{"enchantments":"minecraft:silk_touch","levels":{"min":1}}]}}}`,
}

// lootKey is the key a loot table id is kept under: a vanilla table by its
// path ("chests/simple_dungeon"), another namespace's by its full id.
func lootKey(id string) string { return strings.TrimPrefix(nsID(id), "minecraft:") }

// lootConv converts decoded loot JSON (or SNBT) into the IR.
type lootConv struct {
	tags      *tagRegistry
	preds     map[string]any // predicate id → its decoded file
	mods      map[string]any // item modifier id → its decoded file
	resolving map[string]bool
	extra     map[string]*lootTable // inline tables, by their made-up key
	base      string                // the table being read (names its inline tables)
	notes     map[string]bool       // what was not supported, for the log
}

func newLootConv(tags *tagRegistry, preds, mods map[string]any) *lootConv {
	return &lootConv{tags: tags, preds: preds, mods: mods, resolving: map[string]bool{}, extra: map[string]*lootTable{}, notes: map[string]bool{}}
}

func (lc *lootConv) note(s string) { lc.notes[s] = true }

func (lc *lootConv) noteList() string {
	out := make([]string, 0, len(lc.notes))
	for n := range lc.notes {
		out = append(out, n)
	}
	sort.Strings(out)
	lc.notes = map[string]bool{}
	return strings.Join(out, "; ")
}

// lootField is a map value under its 26.x name or its older one.
func lootField(m map[string]any, names ...string) (any, bool) {
	for _, n := range names {
		if v, ok := m[n]; ok {
			return v, true
		}
	}
	return nil, false
}

// stripNS reads a predicate's keys without their namespace
// ("minecraft:flags" → "flags").
func stripNS(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[strings.TrimPrefix(k, "minecraft:")] = v
	}
	return out
}

func asBool(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	n, _ := snbtInt(v)
	return n != 0
}

func asString(v any) string { s, _ := v.(string); return s }

// conds reads a condition holder: a predicate id, an inline condition, or a
// list of them (all must hold).
func (lc *lootConv) conds(v any) ([]lootCond, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		id, ok := parseResID(x)
		if !ok {
			return nil, fmt.Errorf("not a valid predicate id: %s", x)
		}
		raw, ok := lc.preds[id]
		if !ok {
			return nil, fmt.Errorf("unknown predicate %s", id)
		}
		if lc.resolving[id] {
			return nil, fmt.Errorf("predicate %s refers to itself", id)
		}
		lc.resolving[id] = true
		defer delete(lc.resolving, id)
		return lc.conds(raw)
	case []any:
		var out []lootCond
		for _, e := range x {
			c, err := lc.conds(e)
			if err != nil {
				return nil, err
			}
			out = append(out, c...)
		}
		return out, nil
	case map[string]any:
		c, err := lc.cond(x)
		if err != nil {
			return nil, err
		}
		return []lootCond{c}, nil
	}
	return nil, errors.New("not a condition")
}

// one is a condition holder as a single condition.
func (lc *lootConv) one(v any) (lootCond, error) {
	cs, err := lc.conds(v)
	if err != nil {
		return lootCond{}, err
	}
	if len(cs) == 1 {
		return cs[0], nil
	}
	return lootCond{C: "all", Terms: cs}, nil
}

// unsupported is a condition this server cannot evaluate: it never holds.
func (lc *lootConv) unsupported(what string) lootCond {
	lc.note("condition " + what)
	return lootCond{C: "unsupported"}
}

func (lc *lootConv) cond(m map[string]any) (lootCond, error) {
	tv, _ := lootField(m, "type", "condition")
	typ := nsID(asString(tv))
	switch typ {
	case "minecraft:inverted":
		t, err := lc.one(m["term"])
		if err != nil {
			return lootCond{}, err
		}
		return lootCond{C: "not", Term: &t}, nil
	case "minecraft:any_of", "minecraft:all_of":
		terms, ok := m["terms"].([]any)
		if !ok {
			return lootCond{}, fmt.Errorf("%s needs terms", typ)
		}
		c := lootCond{C: "all"}
		if typ == "minecraft:any_of" {
			c.C = "any"
		}
		for _, e := range terms {
			t, err := lc.one(e)
			if err != nil {
				return lootCond{}, err
			}
			c.Terms = append(c.Terms, t)
		}
		return c, nil
	case "minecraft:random_chance":
		np, ok := numberProvider(m["chance"])
		if !ok || np.T != "const" {
			return lc.unsupported("random_chance with a varying chance"), nil
		}
		return lootCond{C: "chance", P: np.V}, nil
	case "minecraft:random_chance_with_enchanted_bonus":
		base, _ := snbtFloat(m["unenchanted_chance"])
		c := lootCond{C: "ench_chance", Ench: strings.TrimPrefix(nsID(asString(m["enchantment"])), "minecraft:"), BaseChance: base}
		switch ec := m["enchanted_chance"].(type) {
		case map[string]any:
			switch nsID(asString(ec["type"])) {
			case "minecraft:linear":
				c.LinearBase, _ = snbtFloat(ec["base"])
				c.PerLevel, _ = snbtFloat(ec["per_level_above_first"])
			case "minecraft:constant":
				c.LinearBase, _ = snbtFloat(ec["value"])
			default:
				return lc.unsupported("random_chance_with_enchanted_bonus level formula"), nil
			}
		default:
			f, ok := snbtFloat(ec)
			if !ok {
				return lootCond{}, errors.New("bad enchanted_chance")
			}
			c.LinearBase = f
		}
		return c, nil
	case "minecraft:survives_explosion":
		return lootCond{C: "survives"}, nil
	case "minecraft:killed_by_player":
		return lootCond{C: "killed_by_player"}, nil
	case "minecraft:table_bonus":
		list, _ := m["chances"].([]any)
		c := lootCond{C: "table_bonus", Ench: strings.TrimPrefix(nsID(asString(m["enchantment"])), "minecraft:")}
		for _, e := range list {
			f, ok := snbtFloat(e)
			if !ok {
				return lootCond{}, errors.New("bad table_bonus chances")
			}
			c.Chances = append(c.Chances, f)
		}
		if len(c.Chances) == 0 {
			return lootCond{}, errors.New("table_bonus needs chances")
		}
		return c, nil
	case "minecraft:match_block", "minecraft:block_state_property":
		st, _ := lootField(m, "state", "properties")
		props := map[string]string{}
		if sm, ok := st.(map[string]any); ok {
			for k, v := range sm {
				switch x := v.(type) {
				case string:
					props[k] = x
				case bool:
					props[k] = fmt.Sprint(x)
				case map[string]any:
					return lc.unsupported("block state ranges"), nil
				default:
					if n, ok := snbtInt(x); ok {
						props[k] = fmt.Sprint(n)
					} else {
						return lootCond{}, fmt.Errorf("bad state value for %s", k)
					}
				}
			}
		}
		return lootCond{C: "state", Props: props}, nil
	case "minecraft:match_tool":
		pm, _ := m["predicate"].(map[string]any)
		if pm == nil {
			return lootCond{C: "all"}, nil // no predicate: any tool
		}
		pm = stripNS(pm)
		c := lootCond{C: "tool"}
		for k, v := range pm {
			switch k {
			case "items":
				names, ok := lc.itemNames(v)
				if !ok {
					return lootCond{}, fmt.Errorf("bad match_tool items %v", v)
				}
				c.Items = names
			case "predicates":
				sub, _ := v.(map[string]any)
				for sk, sv := range stripNS(sub) {
					if sk != "enchantments" {
						return lc.unsupported("match_tool " + sk), nil
					}
					list, _ := sv.([]any)
					if len(list) != 1 {
						return lc.unsupported("match_tool enchantments"), nil
					}
					em, _ := list[0].(map[string]any)
					if nsID(asString(em["enchantments"])) != "minecraft:silk_touch" {
						return lc.unsupported("match_tool enchantments"), nil
					}
					c.Silk = true
				}
			default:
				return lc.unsupported("match_tool " + k), nil
			}
		}
		if c.Silk && c.Items != nil {
			return lc.unsupported("match_tool items with enchantments"), nil
		}
		if !c.Silk && c.Items == nil {
			return lootCond{C: "all"}, nil
		}
		if len(c.Items) == 0 && !c.Silk {
			return lootCond{C: "any"}, nil // an empty item set: no tool matches
		}
		return c, nil
	case "minecraft:entity_properties":
		c := lootCond{C: "entity", Who: asString(m["entity"])}
		if c.Who == "" {
			return lootCond{}, errors.New("entity_properties needs an entity")
		}
		pm, _ := m["predicate"].(map[string]any)
		for k, v := range stripNS(pm) {
			switch k {
			case "flags":
				fm, _ := v.(map[string]any)
				for fk, fv := range stripNS(fm) {
					if fk != "is_on_fire" || !asBool(fv) {
						return lc.unsupported("entity flag " + fk), nil
					}
					c.OnFire = true
				}
			case "type", "entity_type":
				s := asString(v)
				if s == "" || strings.HasPrefix(s, "#") {
					return lc.unsupported("entity type tags"), nil
				}
				c.EType = strings.TrimPrefix(nsID(s), "minecraft:")
			default:
				return lc.unsupported("entity predicate " + k), nil
			}
		}
		return c, nil
	case "minecraft:damage_source_properties":
		c := lootCond{C: "dmgsrc"}
		pm, _ := m["predicate"].(map[string]any)
		for k, v := range stripNS(pm) {
			switch k {
			case "tags":
				list, _ := v.([]any)
				for _, e := range list {
					tm, _ := e.(map[string]any)
					id := strings.TrimPrefix(strings.TrimPrefix(asString(tm["id"]), "#"), "minecraft:")
					if _, known := dmgTagByName[id]; !known {
						return lc.unsupported("damage tag " + id), nil
					}
					if c.DmgTags == nil {
						c.DmgTags = map[string]bool{}
					}
					c.DmgTags[id] = asBool(tm["expected"])
				}
			case "direct_entity", "source_entity":
				em, _ := v.(map[string]any)
				em = stripNS(em)
				tv, _ := lootField(em, "type", "entity_type")
				s := asString(tv)
				if len(em) != 1 || s == "" || strings.HasPrefix(s, "#") {
					return lc.unsupported("damage source " + k), nil
				}
				if k == "direct_entity" {
					c.Direct = strings.TrimPrefix(nsID(s), "minecraft:")
				} else {
					c.Source = strings.TrimPrefix(nsID(s), "minecraft:")
				}
			default:
				return lc.unsupported("damage source predicate " + k), nil
			}
		}
		return c, nil
	case "minecraft:location_check":
		for _, k := range []string{"offsetX", "offsetY", "offsetZ"} {
			if n, _ := snbtInt(m[k]); n != 0 {
				return lc.unsupported("location_check offsets"), nil
			}
		}
		pm, _ := m["predicate"].(map[string]any)
		pm = stripNS(pm)
		if len(pm) != 1 || pm["biomes"] == nil {
			return lc.unsupported("location_check beyond biomes"), nil
		}
		biomes, ok := lc.biomeIDs(pm["biomes"])
		if !ok {
			return lootCond{}, fmt.Errorf("bad biomes %v", pm["biomes"])
		}
		return lootCond{C: "biome", Biomes: biomes}, nil
	case "minecraft:weather_check":
		c := lootCond{C: "weather"}
		if v, ok := m["raining"]; ok {
			b := asBool(v)
			c.Raining = &b
		}
		if v, ok := m["thundering"]; ok {
			b := asBool(v)
			c.Thundering = &b
		}
		return c, nil
	case "minecraft:":
		return lootCond{}, errors.New("a condition needs a type")
	}
	return lc.unsupported(typ), nil
}

// itemNames reads an item holder set — an id, a #tag, a list of ids — as
// bare item names.
func (lc *lootConv) itemNames(v any) ([]string, bool) {
	var ids []string
	switch x := v.(type) {
	case string:
		if tag, ok := strings.CutPrefix(x, "#"); ok {
			id, ok := parseResID(tag)
			if !ok {
				return nil, false
			}
			members, ok := lc.tags.members("item", id)
			if !ok {
				return nil, false
			}
			ids = members
		} else {
			ids = []string{x}
		}
	case []any:
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			ids = append(ids, s)
		}
	default:
		return nil, false
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := itemIDOf(id); !ok {
			return nil, false
		}
		out = append(out, strings.TrimPrefix(nsID(id), "minecraft:"))
	}
	return out, true
}

// biomeIDs reads a biome holder set as biome ids.
func (lc *lootConv) biomeIDs(v any) ([]string, bool) {
	switch x := v.(type) {
	case string:
		if tag, ok := strings.CutPrefix(x, "#"); ok {
			id, ok := parseResID(tag)
			if !ok {
				return nil, false
			}
			return lc.tags.members("worldgen/biome", id)
		}
		return []string{nsID(x)}, true
	case []any:
		var out []string
		for _, e := range x {
			s, ok := e.(string)
			if !ok {
				return nil, false
			}
			out = append(out, nsID(s))
		}
		return out, true
	}
	return nil, false
}

// fns reads a function holder: an item modifier id, one function, or a
// list of them (a sequence).
func (lc *lootConv) fns(v any) ([]lootFn, error) {
	switch x := v.(type) {
	case nil:
		return nil, nil
	case string:
		id, ok := parseResID(x)
		if !ok {
			return nil, fmt.Errorf("not a valid item modifier id: %s", x)
		}
		raw, ok := lc.mods[id]
		if !ok {
			return nil, fmt.Errorf("unknown item modifier %s", id)
		}
		if lc.resolving["mod:"+id] {
			return nil, fmt.Errorf("item modifier %s refers to itself", id)
		}
		lc.resolving["mod:"+id] = true
		defer delete(lc.resolving, "mod:"+id)
		return lc.fns(raw)
	case []any:
		var out []lootFn
		for _, e := range x {
			f, err := lc.fns(e)
			if err != nil {
				return nil, err
			}
			out = append(out, f...)
		}
		return out, nil
	case map[string]any:
		tv, _ := lootField(x, "type", "function")
		typ := nsID(asString(tv))
		if typ == "minecraft:sequence" {
			return lc.fns(x["functions"])
		}
		f, ok, err := lc.fn(typ, x)
		if err != nil || !ok {
			return nil, err
		}
		return []lootFn{f}, nil
	}
	return nil, errors.New("not a loot function")
}

// fn reads one function; ok is false for one left out.
func (lc *lootConv) fn(typ string, m map[string]any) (lootFn, bool, error) {
	if _, cond := lootField(m, "condition", "conditions"); cond && typ != "minecraft:furnace_smelt" {
		// The IR's functions carry no conditions (a smelt already waits for
		// the burning mob).
		lc.note("conditional " + typ)
		return lootFn{}, false, nil
	}
	np := func(key string) (*lootNP, error) {
		n, ok := numberProvider(m[key])
		if !ok {
			return nil, fmt.Errorf("bad %s of %s", key, typ)
		}
		return n, nil
	}
	ench := func() string { return strings.TrimPrefix(nsID(asString(m["enchantment"])), "minecraft:") }
	switch typ {
	case "minecraft:set_count":
		n, err := np("count")
		return lootFn{F: "set_count", NP: n, Add: asBool(m["add"])}, err == nil, err
	case "minecraft:explosion_decay":
		return lootFn{F: "explosion_decay"}, true, nil
	case "minecraft:limit_count":
		f := lootFn{F: "limit"}
		switch l := m["limit"].(type) {
		case map[string]any:
			for _, k := range []string{"min", "max"} {
				if v, ok := l[k]; ok {
					n, ok := snbtInt(v)
					if !ok {
						lc.note("limit_count with a varying bound")
						return lootFn{}, false, nil
					}
					iv := int(n)
					if k == "min" {
						f.Min = &iv
					} else {
						f.Max = &iv
					}
				}
			}
		default:
			n, ok := snbtInt(l)
			if !ok {
				return lootFn{}, false, errors.New("bad limit")
			}
			iv := int(n)
			f.Min, f.Max = &iv, &iv
		}
		return f, true, nil
	case "minecraft:apply_bonus":
		f := lootFn{F: "bonus", Ench: ench(), Formula: strings.TrimPrefix(nsID(asString(m["formula"])), "minecraft:")}
		p, _ := m["parameters"].(map[string]any)
		switch f.Formula {
		case "ore_drops":
		case "uniform_bonus_count":
			f.Mult = 1
			if v, ok := snbtInt(p["bonusMultiplier"]); ok {
				f.Mult = int(v)
			}
		case "binomial_with_bonus_count":
			e, _ := snbtInt(p["extra"])
			f.Extra = int(e)
			f.Prob, _ = snbtFloat(p["probability"])
		default:
			return lootFn{}, false, fmt.Errorf("unknown bonus formula %s", f.Formula)
		}
		return f, true, nil
	case "minecraft:enchanted_count_increase":
		n, err := np("count")
		lim, _ := snbtInt(m["limit"])
		return lootFn{F: "looting", Ench: ench(), NP: n, Limit: int(lim)}, err == nil, err
	case "minecraft:furnace_smelt":
		return lootFn{F: "smelt"}, true, nil
	case "minecraft:set_potion":
		return lootFn{F: "set_potion", Potion: strings.TrimPrefix(nsID(asString(m["id"])), "minecraft:")}, true, nil
	case "minecraft:enchant_randomly":
		f := lootFn{F: "ench_random"}
		if o := asString(m["options"]); o != "" && !strings.HasPrefix(o, "#") {
			f.Ench = strings.TrimPrefix(nsID(o), "minecraft:")
		}
		return f, true, nil
	case "minecraft:set_enchantments":
		em, _ := m["enchantments"].(map[string]any)
		f := lootFn{F: "set_ench", Add: asBool(m["add"])}
		names := make([]string, 0, len(em))
		for n := range em {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			lvl, ok := snbtInt(em[n])
			if !ok {
				lc.note("set_enchantments with a varying level")
				return lootFn{}, false, nil
			}
			f.Enchs = append(f.Enchs, lootEnch{Ench: strings.TrimPrefix(nsID(n), "minecraft:"), Lvl: int(lvl)})
		}
		return f, len(f.Enchs) > 0, nil
	case "minecraft:enchant_with_levels":
		n, err := np("levels")
		return lootFn{F: "ench_levels", NP: n}, err == nil, err
	case "minecraft:set_damage":
		n, err := np("damage")
		return lootFn{F: "set_damage", NP: n, Add: asBool(m["add"])}, err == nil, err
	case "minecraft:set_name":
		name := componentText(m["name"])
		return lootFn{F: "set_name", Name: name}, name != "", nil
	case "minecraft:set_stew_effect":
		list, _ := m["effects"].([]any)
		f := lootFn{F: "set_stew"}
		for _, e := range list {
			em, _ := e.(map[string]any)
			f.Effects = append(f.Effects, strings.TrimPrefix(nsID(asString(em["type"])), "minecraft:"))
		}
		return f, len(f.Effects) > 0, nil
	case "minecraft:set_ominous_bottle_amplifier":
		n, err := np("amplifier")
		return lootFn{F: "set_ominous", NP: n}, err == nil, err
	case "minecraft:set_instrument":
		o := strings.TrimPrefix(strings.TrimPrefix(asString(m["options"]), "#"), "minecraft:")
		return lootFn{F: "set_instrument", Options: strings.TrimSuffix(o, "_goat_horns")}, true, nil
	case "minecraft:":
		return lootFn{}, false, errors.New("a function needs a type")
	}
	lc.note("function " + typ)
	return lootFn{}, false, nil
}

// entry reads one pool entry.
func (lc *lootConv) entry(m map[string]any) (lootEntry, error) {
	typ := nsID(asString(m["type"]))
	cv, _ := lootField(m, "condition", "conditions")
	conds, err := lc.conds(cv)
	if err != nil {
		return lootEntry{}, err
	}
	fv, _ := lootField(m, "modifier", "functions")
	fns, err := lc.fns(fv)
	if err != nil {
		return lootEntry{}, err
	}
	w, q := 1, 0
	if v, ok := snbtInt(m["weight"]); ok {
		w = int(v)
	}
	if v, ok := snbtInt(m["quality"]); ok {
		q = int(v)
	}
	switch typ {
	case "minecraft:item":
		it, ok := itemIDOf(asString(m["name"]))
		if !ok {
			return lootEntry{}, fmt.Errorf("unknown item %v", m["name"])
		}
		return lootEntry{Type: "item", ID: it, W: w, Q: q, Conditions: conds, Functions: fns}, nil
	case "minecraft:empty":
		return lootEntry{Type: "empty", W: w, Q: q, Conditions: conds}, nil
	case "minecraft:alternatives", "minecraft:group", "minecraft:sequence":
		kids, _ := m["children"].([]any)
		e := lootEntry{Type: strings.TrimPrefix(typ, "minecraft:"), Conditions: conds}
		for _, k := range kids {
			km, ok := k.(map[string]any)
			if !ok {
				return lootEntry{}, errors.New("bad child entry")
			}
			c, err := lc.entry(km)
			if err != nil {
				return lootEntry{}, err
			}
			e.Children = append(e.Children, c)
		}
		return e, nil
	case "minecraft:loot_table":
		v := m["value"]
		if l, ok := v.([]any); ok && len(l) == 1 {
			v = l[0]
		}
		ref := lootEntry{Type: "ref", W: w, Q: q, Conditions: conds, Functions: fns}
		switch x := v.(type) {
		case string:
			if strings.HasPrefix(x, "#") {
				lc.note("loot_table entries naming a tag")
				return lootEntry{Type: "empty", W: w, Q: q, Conditions: conds}, nil
			}
			ref.Ref = lootKey(x)
		case map[string]any:
			key := fmt.Sprintf("%s/inline%d", lc.base, len(lc.extra))
			lc.extra[key] = &lootTable{} // the key is taken before any table nested inside
			t, err := lc.table(x)
			if err != nil {
				return lootEntry{}, err
			}
			lc.extra[key] = t
			ref.Ref = key
		default:
			lc.note("loot_table entries naming several tables")
			return lootEntry{Type: "empty", W: w, Q: q, Conditions: conds}, nil
		}
		return ref, nil
	case "minecraft:tag":
		iv, _ := lootField(m, "items", "name")
		if s, ok := iv.(string); ok && !strings.HasPrefix(s, "#") && typ == "minecraft:tag" {
			if _, isName := m["name"]; isName { // the older form names the tag without its #
				iv = "#" + s
			}
		}
		names, ok := lc.itemNames(iv)
		if !ok {
			return lootEntry{}, fmt.Errorf("bad tag entry items %v", iv)
		}
		expand := asBool(m["expand"])
		switch {
		case expand: // each item its own leaf, with the entry's weight
			g := lootEntry{Type: "group", Conditions: conds}
			for _, n := range names {
				g.Children = append(g.Children, lootEntry{Type: "item", ID: itemByName[n], W: w, Q: q, Functions: fns})
			}
			return g, nil
		case len(names) == 1:
			return lootEntry{Type: "item", ID: itemByName[names[0]], W: w, Q: q, Conditions: conds, Functions: fns}, nil
		}
		lc.note("tag entries that drop every item at once")
		return lootEntry{Type: "empty", W: w, Q: q, Conditions: conds}, nil
	case "minecraft:dynamic", "minecraft:slots":
		lc.note(typ + " entries")
		return lootEntry{Type: "empty", W: w, Q: q, Conditions: conds}, nil
	}
	return lootEntry{}, fmt.Errorf("unknown entry type %q", typ)
}

// table reads a loot table; the table's own functions follow each pool's.
func (lc *lootConv) table(m map[string]any) (*lootTable, error) {
	fv, _ := lootField(m, "modifier", "functions")
	tableFns, err := lc.fns(fv)
	if err != nil {
		return nil, err
	}
	t := &lootTable{}
	pools, _ := m["pools"].([]any)
	for _, pv := range pools {
		pm, ok := pv.(map[string]any)
		if !ok {
			return nil, errors.New("bad pool")
		}
		rolls, ok := numberProvider(pm["rolls"])
		if !ok {
			return nil, fmt.Errorf("bad rolls %v", pm["rolls"])
		}
		p := lootPool{Rolls: *rolls}
		if bv, ok := pm["bonus_rolls"]; ok {
			b, ok := numberProvider(bv)
			if !ok {
				return nil, errors.New("bad bonus_rolls")
			}
			p.Bonus = b
		}
		cv, _ := lootField(pm, "condition", "conditions")
		if p.Conditions, err = lc.conds(cv); err != nil {
			return nil, err
		}
		pf, _ := lootField(pm, "modifier", "functions")
		if p.Functions, err = lc.fns(pf); err != nil {
			return nil, err
		}
		p.Functions = append(p.Functions, tableFns...)
		entries, ok := pm["entries"].([]any)
		if !ok {
			return nil, errors.New("a pool needs entries")
		}
		for _, ev := range entries {
			em, ok := ev.(map[string]any)
			if !ok {
				return nil, errors.New("bad entry")
			}
			e, err := lc.entry(em)
			if err != nil {
				return nil, err
			}
			p.Entries = append(p.Entries, e)
		}
		t.Pools = append(t.Pools, p)
	}
	return t, nil
}

// decodeJSON reads a data file as generic values (numbers as float64).
func decodeJSON(data []byte) (any, error) {
	var v any
	err := json.Unmarshal(data, &v)
	return v, err
}

// buildPackLoot reads the packs' predicates, item modifiers and loot
// tables (each id from the top pack's file) against the load's tags.
func buildPackLoot(pc *packContent, predFiles, modFiles, lootFiles map[string]packFile) {
	preds := map[string]any{}
	for id, raw := range vanillaPredicates {
		v, _ := decodeJSON([]byte(raw))
		preds[id] = v
	}
	for id, f := range predFiles {
		v, err := decodeJSON(f.data)
		if err != nil {
			log.Printf("datapacks: couldn't parse predicate %s from %s: %v", id, f.pack, err)
			delete(preds, id)
			continue
		}
		preds[id] = v
	}
	mods := map[string]any{}
	for id, f := range modFiles {
		v, err := decodeJSON(f.data)
		if err != nil {
			log.Printf("datapacks: couldn't parse item modifier %s from %s: %v", id, f.pack, err)
			continue
		}
		mods[id] = v
	}
	lc := newLootConv(pc.tags, preds, mods)
	pc.predRaw, pc.modRaw = preds, mods

	pc.predicates = map[string][]lootCond{}
	for id := range preds {
		cs, err := lc.conds(id) // through the reference, so a cycle is caught
		if err != nil {
			log.Printf("datapacks: couldn't load predicate %s: %v", id, err)
			continue
		}
		if n := lc.noteList(); n != "" {
			log.Printf("datapacks: predicate %s: not supported here: %s", id, n)
		}
		pc.predicates[id] = cs
	}
	pc.modifiers = map[string]itemModifier{}
	for id, raw := range mods {
		m, msg := parseLootFunction(lootFunctionForCommand(raw, mods, map[string]bool{id: true}))
		if msg != "" {
			log.Printf("datapacks: couldn't load item modifier %s: %s", id, msg)
			continue
		}
		pc.modifiers[id] = m
	}

	pc.loot = map[string]*lootTable{}
	ids := make([]string, 0, len(lootFiles))
	for id := range lootFiles {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		f := lootFiles[id]
		key := lootKey(id)
		lc.base = key
		v, err := decodeJSON(f.data)
		var t *lootTable
		if err == nil {
			m, ok := v.(map[string]any)
			if !ok {
				err = errors.New("not a loot table")
			} else {
				t, err = lc.table(m)
			}
		}
		if err != nil {
			log.Printf("datapacks: couldn't parse loot table %s from %s: %v", id, f.pack, err)
			t = &lootTable{} // LootTable.EMPTY stands in for a missing element
			lc.noteList()
		} else if n := lc.noteList(); n != "" {
			log.Printf("datapacks: loot table %s (from %s): not supported here: %s", id, f.pack, n)
		}
		pc.loot[key] = t
		switch {
		case strings.HasPrefix(key, "blocks/"):
			pc.blockLoot = true
		case strings.HasPrefix(key, "entities/"):
			pc.entityLoot = true
		}
	}
	for k, t := range lc.extra {
		pc.loot[k] = t
	}
}

// lootFunctionForCommand rewrites a decoded item modifier into the shape
// parseLootFunction reads: each function's "type" as "function", its
// "condition" as "conditions" (which the command refuses), and an id in a
// list as the modifier it names.
func lootFunctionForCommand(v any, mods map[string]any, seen map[string]bool) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, 0, len(x))
		for _, e := range x {
			out = append(out, lootFunctionForCommand(e, mods, seen))
		}
		return out
	case string:
		id, ok := parseResID(x)
		if raw, known := mods[id]; ok && known && !seen[id] {
			seen[id] = true
			defer delete(seen, id)
			return lootFunctionForCommand(raw, mods, seen)
		}
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, v := range x {
			out[k] = v
		}
		if _, ok := out["function"]; !ok {
			if t, ok := out["type"]; ok {
				out["function"] = t
				delete(out, "type")
			}
		}
		if c, ok := out["condition"]; ok {
			delete(out, "condition")
			if l, isList := c.([]any); isList {
				out["conditions"] = l
			} else {
				out["conditions"] = []any{c}
			}
		}
		if fs, ok := out["functions"]; ok && nsID(asString(out["function"])) == "minecraft:sequence" {
			out["functions"] = lootFunctionForCommand(fs, mods, seen)
		}
		return out
	}
	return v
}

// ---- lookups ------------------------------------------------------------

// packLootTable is a pack's table at a loot key, if a pack has one.
func packLootTable(key string) (*lootTable, bool) {
	pc := currentPack()
	if pc == nil || pc.loot == nil {
		return nil, false
	}
	t, ok := pc.loot[key]
	return t, ok
}

// blockLootName is the block whose loot table a block uses: a wall torch,
// sign, banner, head or coral fan drops like its standing form
// (BlockBehaviour.Properties.dropsLike), the rest their own.
func blockLootName(name string) string {
	var alt string
	switch {
	case strings.HasPrefix(name, "wall_"):
		alt = strings.TrimPrefix(name, "wall_")
	case strings.Contains(name, "_wall_"):
		alt = strings.Replace(name, "_wall_", "_", 1)
	default:
		return name
	}
	if _, _, ok := worldgen.BlockRangeOK(alt); ok {
		return alt
	}
	return name
}

// packBlockLoot is a pack's table for a block state, if a pack has one.
func packBlockLoot(state uint32) (*lootTable, bool) {
	pc := currentPack()
	if pc == nil || !pc.blockLoot {
		return nil, false
	}
	name, ok := worldgen.StateName(state)
	if !ok {
		return nil, false
	}
	t, ok := pc.loot["blocks/"+blockLootName(name)]
	return t, ok
}

// packEntityLoot is a pack's table for an entity type, if a pack has one.
func packEntityLoot(etype int32) (*lootTable, bool) {
	pc := currentPack()
	if pc == nil || !pc.entityLoot {
		return nil, false
	}
	name, ok := advEntityName[int(etype)]
	if !ok {
		return nil, false
	}
	t, ok := pc.loot["entities/"+name]
	return t, ok
}

// predicateFor is `execute if predicate`'s argument: a predicate id, or an
// inline predicate (SNBT) — the conditions to test, or the failure.
func predicateFor(arg string) ([]lootCond, string) {
	pc := currentPack()
	if strings.HasPrefix(arg, "{") || strings.HasPrefix(arg, "[") {
		v, err := parseSNBT(arg)
		if err != nil {
			return nil, err.Error()
		}
		var tags *tagRegistry
		var preds, mods map[string]any
		if pc != nil {
			tags, preds, mods = pc.tags, pc.predRaw, pc.modRaw
		}
		if preds == nil {
			preds = map[string]any{}
			for id, raw := range vanillaPredicates {
				d, _ := decodeJSON([]byte(raw))
				preds[id] = d
			}
		}
		cs, err := newLootConv(tags, preds, mods).conds(v)
		if err != nil {
			return nil, err.Error()
		}
		return cs, ""
	}
	id, ok := parseResID(arg)
	if !ok {
		return nil, fmt.Sprintf("Invalid ID: %s", arg)
	}
	if pc != nil {
		if cs, ok := pc.predicates[id]; ok {
			return cs, ""
		}
	} else if raw, ok := vanillaPredicates[id]; ok {
		d, _ := decodeJSON([]byte(raw))
		if cs, err := newLootConv(nil, map[string]any{}, nil).conds(d); err == nil {
			return cs, ""
		}
	}
	return nil, fmt.Sprintf("Can't find element '%s' in registry 'minecraft:predicate'", id)
}

// packItemModifier is an item modifier a pack defines.
func packItemModifier(id string) (itemModifier, bool) {
	pc := currentPack()
	if pc == nil {
		return nil, false
	}
	m, ok := pc.modifiers[id]
	return m, ok
}

// commandLootCtx is the context `execute if predicate` tests in (vanilla's
// LootContextParamSets.COMMAND): the source's position and its biome, the
// weather there (it rains only where weather runs, the overworld), and the
// type of the entity the source runs as. A command has no tool, killer or
// explosion, so the conditions that read them do not hold.
func (h *hub) commandLootCtx(s *execSource) *lootCtx {
	c := &lootCtx{rng: h.rng.Intn, randf: h.rng.Float64,
		pos: blockPos{floorInt(s.x), floorInt(s.y), floorInt(s.z)}, located: true,
		weatherKnown: true,
		raining:      s.dim == dimOverworld && h.raining,
		thundering:   s.dim == dimOverworld && h.thundering}
	if w := h.worldFor(s.dim); w != nil {
		c.biomeAt = w.BiomeAt3D
	}
	switch {
	case s.self.t != nil:
		c.thisType = "player"
	case s.self.m != nil:
		c.thisType = advEntityName[s.self.m.etype]
	}
	return c
}
