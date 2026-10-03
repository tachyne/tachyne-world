package server

import (
	"fmt"
	"strings"
)

// Slot sources (SlotSourceArgument, 26.3): what /execute if items and if
// slots read from a holder. The argument is a slot range (container.*,
// weapon.mainhand …, a RangeSlotSource over the holder), else a slot
// source from the minecraft:slot_source registry by id or written inline as
// SNBT — a typed object, or a list of them for a group:
//
//	{type:"slot_range", slots:"hotbar.*"}          the range, of the holder
//	{type:"slot_range", slots:"armor.*", source:"this"}   …of the source's entity
//	{type:"group", terms:[…]}  or  […]             the terms' slots, in turn
//	{type:"filtered", slot_source:…, item_filter:{items:…, count:…, components:…, predicates:…}}
//	{type:"limit_slots", slot_source:…, limit:N}   the first N
//	{type:"empty"}
//
// No data pack adds registry entries, so an id names nothing. The contents
// source (the items inside a shulker box, a bundle or a loaded crossbow)
// is refused.

// slotSrc is one slot source provided for a holder (the loot context's
// CONTAINER); this is the source entity's own slots, nil when it has none.
type slotSrc func(container itemTarget, this *itemTarget) []slotAccess

// slotSrcRefusal is a slot source the engine cannot provide: its text is
// the whole failure, not a codec error.
type slotSrcRefusal string

func (e slotSrcRefusal) Error() string { return string(e) }

// parseSlotSourceArg reads a SlotSourceArgument.
func parseSlotSourceArg(arg string) (slotSrc, string) {
	if ids, ok := slotRanges[arg]; ok { // SlotRanges.tryRead
		return rangeSlotSrc(ids, false), ""
	}
	isID := arg != ""
	for i := 0; i < len(arg); i++ {
		isID = isID && identChar(arg[i])
	}
	if isID && validIdent(arg) {
		return nil, fmt.Sprintf("Can't find element '%s' in registry 'minecraft:slot_source'", nsID(arg))
	}
	v, err := parseSNBT(arg)
	if err != nil {
		return nil, err.Error()
	}
	src, err := decodeSlotSource(v, false)
	if err != nil {
		if r, ok := err.(slotSrcRefusal); ok {
			return nil, string(r)
		}
		return nil, "Failed to parse structure: " + err.Error()
	}
	return src, ""
}

// rangeSlotSrc is RangeSlotSource: the range's slots of the container, or
// of the source's entity.
func rangeSlotSrc(ids []int, this bool) slotSrc {
	return func(container itemTarget, self *itemTarget) []slotAccess {
		if this {
			if self == nil {
				return nil
			}
			return self.slots(ids)
		}
		return container.slots(ids)
	}
}

// lootEntityTargets are the LootContext.EntityTarget and BlockEntityTarget
// names a slot_range's source may give; of them a command's context holds
// only this (and the container).
var lootEntityTargets = map[string]bool{
	"this": true, "attacker": true, "direct_attacker": true, "attacking_player": true,
	"target_entity": true, "interacting_entity": true, "block_entity": true, "container": true,
}

// decodeSlotSource is SlotSources.DIRECT_CODEC (typed only for a list's
// elements).
func decodeSlotSource(v any, typedOnly bool) (slotSrc, error) {
	switch t := v.(type) {
	case string:
		return nil, fmt.Errorf("Unknown registry key in minecraft:slot_source: %s", nsID(t))
	case []any:
		if typedOnly {
			return nil, fmt.Errorf("Not a map: %v", t)
		}
		return decodeSlotGroup(t)
	case map[string]any:
		typ, _ := t["type"].(string)
		switch nsID(typ) {
		case "minecraft:slot_range":
			name, _ := t["slots"].(string)
			ids, ok := slotRanges[name]
			if !ok {
				return nil, fmt.Errorf("Unknown slot range: %s", name)
			}
			from := "container"
			if s, has := t["source"]; has {
				from, _ = s.(string)
				if !lootEntityTargets[from] {
					return nil, fmt.Errorf("Unknown source: %v", s)
				}
			}
			switch from {
			case "container":
				return rangeSlotSrc(ids, false), nil
			case "this":
				return rangeSlotSrc(ids, true), nil
			}
			// The command's loot context has no such entity: no slots.
			return func(itemTarget, *itemTarget) []slotAccess { return nil }, nil
		case "minecraft:group":
			terms, has := t["terms"]
			if !has {
				return nil, fmt.Errorf("No key terms in MapLike")
			}
			list, ok := terms.([]any)
			if !ok {
				list = []any{terms}
			}
			return decodeSlotGroup(list)
		case "minecraft:filtered":
			inner, err := decodeSlotHolder(t)
			if err != nil {
				return nil, err
			}
			f, has := t["item_filter"]
			if !has {
				return nil, fmt.Errorf("No key item_filter in MapLike")
			}
			test, err := decodeItemFilter(f)
			if err != nil {
				return nil, err
			}
			return func(c itemTarget, self *itemTarget) []slotAccess { // SlotCollection.filter
				var out []slotAccess
				for _, sl := range inner(c, self) {
					if test(sl.get()) {
						out = append(out, sl)
					}
				}
				return out
			}, nil
		case "minecraft:limit_slots":
			inner, err := decodeSlotHolder(t)
			if err != nil {
				return nil, err
			}
			n, ok := snbtInt(t["limit"])
			if !ok || n < 1 {
				return nil, fmt.Errorf("Value must be positive: %v", t["limit"])
			}
			return func(c itemTarget, self *itemTarget) []slotAccess { // SlotCollection.limit
				out := inner(c, self)
				if int64(len(out)) > n {
					out = out[:n]
				}
				return out
			}, nil
		case "minecraft:empty":
			return func(itemTarget, *itemTarget) []slotAccess { return nil }, nil
		case "minecraft:contents":
			return nil, slotSrcRefusal("The contents slot source can't be read on this server yet")
		}
		return nil, fmt.Errorf("Unknown registry key in minecraft:slot_source_type: %s", nsID(typ))
	}
	return nil, fmt.Errorf("Not a slot source: %v", v)
}

// decodeSlotHolder is a transformed source's slot_source field
// (SlotSources.CODEC: a registry id or a direct source).
func decodeSlotHolder(m map[string]any) (slotSrc, error) {
	v, has := m["slot_source"]
	if !has {
		return nil, fmt.Errorf("No key slot_source in MapLike")
	}
	return decodeSlotSource(v, false)
}

// decodeSlotGroup is GroupSlotSource: each term's slots in turn.
func decodeSlotGroup(list []any) (slotSrc, error) {
	terms := make([]slotSrc, 0, len(list))
	for _, e := range list {
		s, err := decodeSlotSource(e, true)
		if err != nil {
			return nil, err
		}
		terms = append(terms, s)
	}
	return func(c itemTarget, self *itemTarget) []slotAccess { // SlotCollection.concat
		var out []slotAccess
		for _, s := range terms {
			out = append(out, s(c, self)...)
		}
		return out
	}, nil
}

// decodeItemFilter is ItemPredicate.CODEC (the advancement form): items,
// count, components (exact) and predicates, every one present must hold.
func decodeItemFilter(v any) (func(invStack) bool, error) {
	m, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("Not a map: %v", v)
	}
	var tests []func(invStack) bool
	if x, has := m["items"]; has {
		set := map[int32]bool{}
		for _, n := range holderSetNames(x) {
			if tag, isTag := strings.CutPrefix(n, "#"); isTag {
				members, msg := itemTagMemberSet(tag)
				if msg != "" {
					return nil, slotSrcRefusal(msg)
				}
				for it := range members {
					set[it] = true
				}
				continue
			}
			it, ok := itemByName[strings.TrimPrefix(nsID(n), "minecraft:")]
			if !ok {
				return nil, fmt.Errorf("Unknown registry key in minecraft:item: %s", nsID(n))
			}
			set[it] = true
		}
		tests = append(tests, func(st invStack) bool { return set[st.item] })
	}
	if x, has := m["count"]; has {
		lo, hi, ok := intBounds(x)
		if !ok {
			return nil, fmt.Errorf("Invalid count: %v", x)
		}
		tests = append(tests, func(st invStack) bool { return int64(st.count) >= lo && int64(st.count) <= hi })
	}
	for _, field := range []string{"components", "predicates"} {
		x, has := m[field]
		if !has {
			continue
		}
		cm, ok := x.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("Not a map: %v", x)
		}
		for k, val := range cm {
			var test func(invStack) bool
			var msg string
			if field == "components" {
				test, msg = itemComponentValueTest(nsID(k), val)
			} else {
				test, msg = itemPredicateTest(nsID(k), val)
			}
			if msg != "" {
				return nil, slotSrcRefusal(msg)
			}
			tests = append(tests, test)
		}
	}
	return func(st invStack) bool {
		for _, t := range tests {
			if !t(st) {
				return false
			}
		}
		return true
	}, nil
}
