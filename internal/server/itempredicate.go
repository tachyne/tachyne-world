package server

import (
	"fmt"
	"math"
	"strings"
)

// The item predicate argument (ItemPredicateArgument, ComponentPredicateParser)
// that /execute if items, the slot sources' filters and /clear read:
//
//	<item id> | #<item tag> | *   then optionally   [<condition>, …]
//
// Each condition is one or more alternatives split by |, each maybe negated
// by !, and each one of
//
//	<component>=<value>   the stack carries the component with that value
//	<predicate>~<value>   the stack passes a data component predicate
//	<component>           the stack carries the component
//
// count is a pseudo component and predicate (a MinMaxBounds over the stack
// size). A predicate id that names no predicate type but a component type
// tests that component's presence, its value the empty compound.
//
// The components are the ones an engine stack models (see
// itemComponentPresent); asking about any other is refused by name rather
// than answered wrongly. Of the predicate types, count, damage,
// enchantments, stored_enchantments, potion_contents (its potions) and
// custom_data (no engine stack carries custom data) are tested.

// itemPredicateTypes is 26.3's minecraft:data_component_predicate_type
// registry.
var itemPredicateTypes = map[string]bool{
	"minecraft:attribute_modifiers": true, "minecraft:bundle_contents": true, "minecraft:container": true,
	"minecraft:custom_data": true, "minecraft:damage": true, "minecraft:enchantments": true,
	"minecraft:firework_explosion": true, "minecraft:fireworks": true, "minecraft:jukebox_playable": true,
	"minecraft:potion_contents": true, "minecraft:stored_enchantments": true, "minecraft:trim": true,
	"minecraft:villager/variant": true, "minecraft:writable_book_content": true,
	"minecraft:written_book_content": true,
}

// parseItemPredicate is ItemPredicateArgument: the test an argument stands
// for, or the parse failure.
func parseItemPredicate(arg string) (func(invStack) bool, string) {
	base, conds, hasConds := strings.Cut(arg, "[")
	var tests []func(invStack) bool
	switch {
	case base == "*":
	case strings.HasPrefix(base, "#"):
		members, msg := itemTagMemberSet(base[1:])
		if msg != "" {
			return nil, msg
		}
		tests = append(tests, func(st invStack) bool { return members[st.item] })
	default:
		id, ok := itemByName[strings.TrimPrefix(base, "minecraft:")]
		if !ok || base == "" {
			return nil, fmt.Sprintf("Unknown item '%s'", nsID(base))
		}
		tests = append(tests, func(st invStack) bool { return st.item == id })
	}
	if hasConds {
		if !strings.HasSuffix(conds, "]") {
			return nil, "Expected ']'"
		}
		more, msg := parseItemConditions(conds[:len(conds)-1])
		if msg != "" {
			return nil, msg
		}
		tests = append(tests, more...)
	}
	return func(st invStack) bool {
		for _, t := range tests {
			if !t(st) {
				return false
			}
		}
		return true
	}, ""
}

// itemTagMemberSet is an item tag's items, or the unknown-tag failure.
func itemTagMemberSet(tag string) (map[int32]bool, string) {
	id := nsID(tag)
	names, ok := itemTagMembers[id]
	if !ok {
		return nil, fmt.Sprintf("Unknown item tag '%s'", id)
	}
	set := make(map[int32]bool, len(names))
	for _, n := range names {
		if it, ok := itemByName[n]; ok {
			set[it] = true
		}
	}
	return set, ""
}

// parseItemConditions reads the conditions between the brackets: a list of
// comma-separated conditions, each the alternatives split by |.
func parseItemConditions(s string) ([]func(invStack) bool, string) {
	var out []func(invStack) bool
	i := 0
	skip := func() {
		for i < len(s) && s[i] == ' ' {
			i++
		}
	}
	skip()
	if i >= len(s) {
		return nil, "" // [] — no conditions
	}
	for {
		var alts []func(invStack) bool
		for {
			skip()
			neg := false
			if i < len(s) && s[i] == '!' {
				neg = true
				i++
			}
			j := i
			for j < len(s) && identChar(s[j]) {
				j++
			}
			if j == i || !validIdent(s[i:j]) {
				return nil, "Invalid ID"
			}
			id := nsID(s[i:j])
			i = j
			var test func(invStack) bool
			var msg string
			if i < len(s) && (s[i] == '=' || s[i] == '~') {
				op := s[i]
				i++
				v, n, err := parseSNBTPrefix(s[i:])
				if err != nil {
					what := "component"
					if op == '~' {
						what = "predicate"
					}
					return nil, fmt.Sprintf("Malformed '%s' %s: '%v'", id, what, err)
				}
				i += n
				if op == '=' {
					test, msg = itemComponentValueTest(id, v)
				} else {
					test, msg = itemPredicateTest(id, v)
				}
			} else {
				test, msg = itemComponentPresenceTest(id)
			}
			if msg != "" {
				return nil, msg
			}
			if neg {
				t := test
				test = func(st invStack) bool { return !t(st) }
			}
			alts = append(alts, test)
			skip()
			if i < len(s) && s[i] == '|' {
				i++
				continue
			}
			break
		}
		out = append(out, anyItemTest(alts))
		skip()
		if i >= len(s) {
			return out, ""
		}
		if s[i] != ',' {
			return nil, "Expected ','"
		}
		i++
	}
}

func anyItemTest(alts []func(invStack) bool) func(invStack) bool {
	if len(alts) == 1 {
		return alts[0]
	}
	return func(st invStack) bool {
		for _, t := range alts {
			if t(st) {
				return true
			}
		}
		return false
	}
}

// commonItemComponents are DataComponents.COMMON_ITEM_COMPONENTS: every
// item carries them.
var commonItemComponents = map[string]bool{
	"minecraft:max_stack_size": true, "minecraft:lore": true, "minecraft:enchantments": true,
	"minecraft:repair_cost": true, "minecraft:use_effects": true, "minecraft:attribute_modifiers": true,
	"minecraft:rarity": true, "minecraft:break_sound": true, "minecraft:tooltip_display": true,
	"minecraft:attack_animation": true, "minecraft:interact_animation": true,
}

// itemComponentPresent is ItemStack.has for the components an engine stack
// models: whether st carries key, and known false when the engine cannot
// tell.
func itemComponentPresent(st invStack, key string) (present, known bool) {
	if commonItemComponents[key] {
		return true, true
	}
	switch key {
	case "minecraft:damage", "minecraft:max_damage":
		_, ok := itemMaxDurability[st.item]
		return ok, true
	case "minecraft:stored_enchantments":
		return st.item == itemEnchantedBook, true
	case "minecraft:custom_name":
		return st.name != "", true
	case "minecraft:unbreakable":
		return st.tags.unbreakable, true
	case "minecraft:dyed_color":
		return st.color != 0, true
	case "minecraft:potion_contents":
		switch itemNameOf[st.item] {
		case "potion", "splash_potion", "lingering_potion", "tipped_arrow":
			return true, true
		}
		return st.potion != potNone, true
	case "minecraft:trim":
		return st.trimMat != 0, true
	case "minecraft:banner_patterns":
		return strings.HasSuffix(itemNameOf[st.item], "_banner") || st.pats[0].patPlus1 != 0, true
	case "minecraft:map_id":
		return st.mapID != 0, true
	case "minecraft:can_break":
		return st.tags.canBreak != "", true
	case "minecraft:can_place_on":
		return st.tags.canPlace != "", true
	}
	return false, false
}

func untestable(key string) string {
	return fmt.Sprintf("Item predicate component '%s' can't be tested on this server yet", key)
}

// itemComponentPresenceTest is a bare component in the conditions.
func itemComponentPresenceTest(key string) (func(invStack) bool, string) {
	if key != "minecraft:count" && !dataComponentTypes[strings.TrimPrefix(key, "minecraft:")] {
		return nil, fmt.Sprintf("Unknown item component '%s'", key)
	}
	if key == "minecraft:count" { // the pseudo component is always there
		return func(invStack) bool { return true }, ""
	}
	if _, known := itemComponentPresent(invStack{}, key); !known {
		return nil, untestable(key)
	}
	return func(st invStack) bool {
		ok, _ := itemComponentPresent(st, key)
		return ok
	}, ""
}

// itemComponentValueTest is component=value: the stack carries the
// component and its value equals the one given.
func itemComponentValueTest(key string, v any) (func(invStack) bool, string) {
	if key == "minecraft:count" {
		return itemPredicateTest(key, v)
	}
	if !dataComponentTypes[strings.TrimPrefix(key, "minecraft:")] {
		return nil, fmt.Sprintf("Unknown item component '%s'", key)
	}
	malformed := func(why string) string { return fmt.Sprintf("Malformed '%s' component: '%s'", key, why) }
	switch key {
	case "minecraft:max_stack_size":
		n, ok := snbtInt(v)
		if !ok || n < 1 || n > 99 {
			return nil, malformed("Value must be within range [1;99]")
		}
		return func(st invStack) bool { return int64(stackCap(st.item)) == n }, ""
	case "minecraft:max_damage":
		n, ok := snbtInt(v)
		if !ok || n < 1 {
			return nil, malformed("Value must be positive")
		}
		return func(st invStack) bool {
			max, has := itemMaxDurability[st.item]
			return has && int64(max) == n
		}, ""
	case "minecraft:enchantments", "minecraft:stored_enchantments":
		var want invStack
		if msg := applyItemComponent(&want, key, v); msg != "" {
			return nil, malformed(msg)
		}
		wantSet := enchSet(want.ench)
		stored := key == "minecraft:stored_enchantments"
		return func(st invStack) bool {
			if ok, _ := itemComponentPresent(st, key); !ok {
				return false
			}
			have := map[int8]int8{} // an enchanted book's enchantments are its stored ones
			if stored == (st.item == itemEnchantedBook) {
				have = enchSet(st.ench)
			}
			return enchSetEqual(have, wantSet)
		}, ""
	case "minecraft:damage", "minecraft:custom_name", "minecraft:repair_cost", "minecraft:potion_contents",
		"minecraft:dyed_color", "minecraft:lore", "minecraft:unbreakable", "minecraft:trim",
		"minecraft:banner_patterns", "minecraft:can_break", "minecraft:can_place_on":
		// Checked against a copy of the stack with the component written
		// fresh from the value: equal when nothing changed.
		var probe invStack
		if msg := applyItemComponent(&probe, key, v); msg != "" {
			return nil, malformed(msg)
		}
		return func(st invStack) bool {
			if ok, _ := itemComponentPresent(st, key); !ok {
				return false
			}
			p := st
			if removeItemComponent(&p, key) != "" || applyItemComponent(&p, key, v) != "" {
				return false
			}
			return p == st
		}, ""
	}
	return nil, untestable(key)
}

// enchSet is a stack's enchantments by id.
func enchSet(e enchList) map[int8]int8 {
	out := map[int8]int8{}
	for _, a := range e {
		if a.lvl > 0 {
			out[a.id] = a.lvl
		}
	}
	return out
}

func enchSetEqual(a, b map[int8]int8) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// intBounds is MinMaxBounds.Ints read from SNBT: a whole number (exactly
// it) or {min, max} (either may be missing).
func intBounds(v any) (lo, hi int64, ok bool) {
	lo, hi = math.MinInt64, math.MaxInt64
	if n, isNum := snbtInt(v); isNum {
		if _, isBool := v.(bool); !isBool {
			return n, n, true
		}
	}
	m, isMap := v.(map[string]any)
	if !isMap {
		return 0, 0, false
	}
	for k := range m {
		if k != "min" && k != "max" {
			return 0, 0, false
		}
	}
	if x, has := m["min"]; has {
		n, ok := snbtInt(x)
		if !ok {
			return 0, 0, false
		}
		lo = n
	}
	if x, has := m["max"]; has {
		n, ok := snbtInt(x)
		if !ok {
			return 0, 0, false
		}
		hi = n
	}
	if lo > hi {
		return 0, 0, false // "Min value must be less than or equal to max value"
	}
	return lo, hi, true
}

// itemPredicateTest is predicate~value.
func itemPredicateTest(key string, v any) (func(invStack) bool, string) {
	malformed := func(why string) string { return fmt.Sprintf("Malformed '%s' predicate: '%s'", key, why) }
	switch key {
	case "minecraft:count":
		lo, hi, ok := intBounds(v)
		if !ok {
			return nil, malformed("Not a number or a range")
		}
		return func(st invStack) bool { return int64(st.count) >= lo && int64(st.count) <= hi }, ""
	case "minecraft:damage": // DamagePredicate
		m, ok := v.(map[string]any)
		if !ok {
			return nil, malformed("Not a map")
		}
		dlo, dhi := int64(math.MinInt64), int64(math.MaxInt64)
		ulo, uhi := dlo, dhi
		if x, has := m["damage"]; has {
			if dlo, dhi, ok = intBounds(x); !ok {
				return nil, malformed("Invalid damage range")
			}
		}
		if x, has := m["durability"]; has {
			if ulo, uhi, ok = intBounds(x); !ok {
				return nil, malformed("Invalid durability range")
			}
		}
		return func(st invStack) bool {
			max, has := itemMaxDurability[st.item]
			if !has { // no damage component
				return false
			}
			left := int64(max - st.dmg)
			return left >= ulo && left <= uhi && int64(st.dmg) >= dlo && int64(st.dmg) <= dhi
		}, ""
	case "minecraft:enchantments", "minecraft:stored_enchantments": // EnchantmentsPredicate
		list, ok := v.([]any)
		if !ok {
			return nil, malformed("Not a list")
		}
		type enchPred struct {
			ids    map[int8]bool // nil: any enchantment
			lo, hi int64
			anyLvl bool
		}
		preds := make([]enchPred, 0, len(list))
		for _, e := range list {
			m, ok := e.(map[string]any)
			if !ok {
				return nil, malformed("Not a map")
			}
			p := enchPred{lo: math.MinInt64, hi: math.MaxInt64, anyLvl: true}
			if x, has := m["enchantments"]; has {
				p.ids = map[int8]bool{}
				for _, n := range holderSetNames(x) {
					if strings.HasPrefix(n, "#") {
						return nil, malformed(fmt.Sprintf("Can't find tag '%s' of type 'minecraft:enchantment'", nsID(n[1:])))
					}
					id, ok := enchByName[strings.TrimPrefix(nsID(n), "minecraft:")]
					if !ok {
						return nil, malformed(fmt.Sprintf("Can't find element '%s' of type 'minecraft:enchantment'", nsID(n)))
					}
					p.ids[id] = true
				}
			}
			if x, has := m["levels"]; has {
				if p.lo, p.hi, ok = intBounds(x); !ok {
					return nil, malformed("Invalid levels range")
				}
				p.anyLvl = false
			}
			preds = append(preds, p)
		}
		stored := key == "minecraft:stored_enchantments"
		return func(st invStack) bool {
			if ok, _ := itemComponentPresent(st, key); !ok {
				return false
			}
			have := map[int8]int8{}
			if stored == (st.item == itemEnchantedBook) {
				have = enchSet(st.ench)
			}
			for _, p := range preds { // EnchantmentPredicate.containedIn, every one
				hit := false
				switch {
				case p.ids != nil:
					for id := range p.ids {
						if lvl := have[id]; lvl > 0 && (p.anyLvl || int64(lvl) >= p.lo && int64(lvl) <= p.hi) {
							hit = true
						}
					}
				case !p.anyLvl:
					for _, lvl := range have {
						if int64(lvl) >= p.lo && int64(lvl) <= p.hi {
							hit = true
						}
					}
				default:
					hit = len(have) > 0
				}
				if !hit {
					return false
				}
			}
			return true
		}, ""
	case "minecraft:potion_contents": // PotionsPredicate, its potions
		m, ok := v.(map[string]any)
		if !ok {
			return nil, malformed("Not a map")
		}
		if _, has := m["effects"]; has {
			return nil, untestable(key + " effects")
		}
		var want map[int8]bool
		if x, has := m["potions"]; has {
			want = map[int8]bool{}
			for _, n := range holderSetNames(x) {
				if strings.HasPrefix(n, "#") {
					return nil, malformed(fmt.Sprintf("Can't find tag '%s' of type 'minecraft:potion'", nsID(n[1:])))
				}
				id, ok := potionByVanillaName[strings.TrimPrefix(nsID(n), "minecraft:")]
				if !ok {
					return nil, malformed(fmt.Sprintf("Can't find element '%s' of type 'minecraft:potion'", nsID(n)))
				}
				want[id] = true
			}
		}
		return func(st invStack) bool {
			if ok, _ := itemComponentPresent(st, key); !ok {
				return false
			}
			return want == nil || (st.potion != potNone && want[st.potion])
		}, ""
	case "minecraft:custom_data":
		if _, ok := v.(map[string]any); !ok {
			if _, isStr := v.(string); !isStr {
				return nil, malformed("Not a compound")
			}
		}
		// No engine stack carries custom_data, and a predicate on a
		// component the stack lacks fails.
		return func(invStack) bool { return false }, ""
	}
	if itemPredicateTypes[key] {
		return nil, untestable(key)
	}
	if dataComponentTypes[strings.TrimPrefix(key, "minecraft:")] {
		// createComponentExistencePredicate: the value is a Unit, {}.
		if m, ok := v.(map[string]any); !ok || len(m) != 0 {
			return nil, malformed("Not a unit")
		}
		return itemComponentPresenceTest(key)
	}
	return nil, fmt.Sprintf("Unknown item predicate '%s'", key)
}

// holderSetNames reads a HolderSet as written: one id or #tag, or a list of
// ids.
func holderSetNames(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
