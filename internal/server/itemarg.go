package server

import (
	"fmt"
	"strings"
)

// The item argument (ItemArgument: ItemParser) with its data components:
// `diamond_sword[enchantments={sharpness:5},custom_name="Excalibur",damage=10]`.
// The components the engine's item stacks carry are applied; any other
// component is refused by name rather than silently dropped.

// parseItemArg reads an item argument into a stack template (count 0).
func parseItemArg(arg string) (invStack, string) {
	name, comps, hasComps := strings.Cut(arg, "[")
	id, ok := itemByName[strings.TrimPrefix(name, "minecraft:")]
	if !ok {
		return invStack{}, fmt.Sprintf("Unknown item '%s'", nsID(name))
	}
	st := invStack{item: id}
	if !hasComps {
		return st, ""
	}
	if !strings.HasSuffix(comps, "]") {
		return invStack{}, "Expected ']' to close the component list"
	}
	comps = strings.TrimSpace(comps[:len(comps)-1])
	for comps != "" {
		if strings.HasPrefix(comps, "!") { // a removal: the default component goes
			end := strings.IndexByte(comps, ',')
			if end < 0 {
				end = len(comps)
			}
			comps = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(comps[end:]), ","))
			continue
		}
		key, rest, ok := strings.Cut(comps, "=")
		if !ok {
			return invStack{}, fmt.Sprintf("Expected '=' after component '%s'", strings.TrimSpace(comps))
		}
		key = nsID(strings.TrimSpace(key))
		v, n, err := parseSNBTPrefix(rest)
		if err != nil {
			return invStack{}, fmt.Sprintf("Malformed '%s' component: %v", key, err)
		}
		if msg := applyItemComponent(&st, key, v); msg != "" {
			return invStack{}, msg
		}
		comps = strings.TrimSpace(rest[n:])
		if comps != "" {
			if comps[0] != ',' {
				return invStack{}, fmt.Sprintf("Expected ',' after the '%s' component", key)
			}
			comps = strings.TrimSpace(comps[1:])
		}
	}
	return st, ""
}

// applyItemComponent sets one data component on a stack.
func applyItemComponent(st *invStack, key string, v any) string {
	bad := func() string { return fmt.Sprintf("Malformed '%s' component", key) }
	switch key {
	case "minecraft:enchantments", "minecraft:stored_enchantments":
		m, ok := v.(map[string]any)
		if !ok {
			return bad()
		}
		if lv, ok := m["levels"].(map[string]any); ok { // the older nested form
			m = lv
		}
		for name, lvl := range m {
			id, ok := enchByName[strings.TrimPrefix(name, "minecraft:")]
			if !ok {
				return fmt.Sprintf("Can't find element '%s' of type 'minecraft:enchantment'", nsID(name))
			}
			n, ok := snbtInt(lvl)
			if !ok || n < 1 || n > 255 {
				return bad()
			}
			st.ench = enchSetLevel(st.ench, id, int8(min(n, 127)))
		}
		if key == "minecraft:stored_enchantments" && st.item == itemBook {
			st.item = itemEnchantedBook
		}
	case "minecraft:custom_name", "minecraft:item_name":
		switch t := v.(type) {
		case string:
			st.name = t
		case map[string]any:
			if s, ok := t["text"].(string); ok {
				st.name = s
			} else {
				return bad()
			}
		default:
			return bad()
		}
	case "minecraft:damage":
		n, ok := snbtInt(v)
		if !ok || n < 0 {
			return bad()
		}
		st.dmg = int(n)
	case "minecraft:repair_cost":
		n, ok := snbtInt(v)
		if !ok || n < 0 {
			return bad()
		}
		st.repairCost = int(n)
	case "minecraft:potion_contents":
		name, _ := v.(string)
		if m, ok := v.(map[string]any); ok {
			name, _ = m["potion"].(string)
		}
		p, ok := potionByVanillaName[strings.TrimPrefix(name, "minecraft:")]
		if !ok {
			return fmt.Sprintf("Can't find element '%s' of type 'minecraft:potion'", nsID(name))
		}
		st.potion = p
	case "minecraft:dyed_color":
		if m, ok := v.(map[string]any); ok {
			v = m["rgb"]
		}
		n, ok := snbtInt(v)
		if !ok {
			return bad()
		}
		st.color = int32(n)
	default:
		return fmt.Sprintf("The '%s' component is not supported here", key)
	}
	return ""
}
