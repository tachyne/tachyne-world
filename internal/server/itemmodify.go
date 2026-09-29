package server

import (
	"fmt"
	"math"
	"strings"
)

// Item modifiers (the item_modifier registry, LootItemFunction) for
// /item modify and the modifier tail of /item … from. A modifier is an id
// from the registry, or an inline SNBT function — one {function:…}
// compound, or a list of them applied in order (a sequence). Vanilla ships
// no item_modifier files, and a datapack cannot add any here, so every id
// is unknown; the inline functions are the ones the engine's loot tables
// run, plus the component-setting ones a command needs:
//
//	set_count, set_damage, enchant_randomly, enchant_with_levels,
//	set_enchantments, set_potion, set_name, set_lore, set_item,
//	set_components, limit_count, explosion_decay, furnace_smelt,
//	set_ominous_bottle_amplifier, set_instrument, set_stew_effect, sequence.
//
// A function that carries conditions is refused: the command context has
// none of the loot parameters they read.

// itemModifier is a parsed modifier: apply rewrites one stack.
type itemModifier func(h *hub, c *lootCtx, st invStack) invStack

// parseItemModifier reads the modifier argument.
func parseItemModifier(arg string) (itemModifier, string) {
	if !strings.HasPrefix(arg, "{") && !strings.HasPrefix(arg, "[") {
		return nil, fmt.Sprintf("Can't find element '%s' in registry 'minecraft:item_modifier'", nsID(arg))
	}
	v, err := parseSNBT(arg)
	if err != nil {
		return nil, fmt.Sprintf("Failed to parse structure: %v", err)
	}
	return parseLootFunction(v)
}

// parseLootFunction reads one function, or a list as a sequence.
func parseLootFunction(v any) (itemModifier, string) {
	if list, ok := v.([]any); ok {
		fns := make([]itemModifier, 0, len(list))
		for _, e := range list {
			f, msg := parseLootFunction(e)
			if msg != "" {
				return nil, msg
			}
			fns = append(fns, f)
		}
		return func(h *hub, c *lootCtx, st invStack) invStack {
			for _, f := range fns {
				st = f(h, c, st)
			}
			return st
		}, ""
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, "Failed to parse structure: not a loot function"
	}
	name, _ := m["function"].(string)
	name = nsID(name)
	if conds, ok := m["conditions"].([]any); ok && len(conds) > 0 {
		return nil, fmt.Sprintf("The conditions of the loot function '%s' are not supported here", name)
	}
	bad := func(field string) (itemModifier, string) {
		return nil, fmt.Sprintf("Failed to parse structure: '%s' of %s", field, name)
	}
	add, _ := snbtInt(m["add"])
	switch name {
	case "minecraft:sequence":
		fns, ok := m["functions"].([]any)
		if !ok {
			return bad("functions")
		}
		return parseLootFunction(fns)
	case "minecraft:set_count": // SetItemCountFunction
		np, ok := numberProvider(m["count"])
		if !ok {
			return bad("count")
		}
		return func(h *hub, c *lootCtx, st invStack) invStack {
			n := int(c.np(np))
			if add != 0 {
				n += st.count
			}
			st.count = n
			return st
		}, ""
	case "minecraft:set_damage": // SetItemDamageFunction: the fraction LEFT
		np, ok := numberProvider(m["damage"])
		if !ok {
			return bad("damage")
		}
		return func(h *hub, c *lootCtx, st invStack) invStack {
			maxd, ok := wearMax(st)
			if !ok {
				return st
			}
			cur := 0.0
			if add != 0 {
				cur = 1 - float64(st.dmg)/float64(maxd)
			}
			left := clamp01(c.npFloat(np) + cur)
			st.dmg = int(math.Floor((1 - left) * float64(maxd)))
			return st
		}, ""
	case "minecraft:enchant_randomly": // EnchantRandomlyFunction
		var options []int8
		if o, ok := m["options"]; ok {
			ids, msg := enchantmentOptions(o)
			if msg != "" {
				return nil, msg
			}
			options = ids
		}
		onlyCompatible := true
		if v, ok := snbtInt(m["only_compatible"]); ok {
			onlyCompatible = v != 0
		}
		return func(h *hub, c *lootCtx, st invStack) invStack {
			if options == nil {
				return addEnchantments(st, enchRandomly(lootRand(c.rng), st.item))
			}
			var pool []int8
			for _, id := range options {
				if !onlyCompatible || st.item == itemBook || enchIsSupported(id, st.item) {
					pool = append(pool, id)
				}
			}
			if len(pool) == 0 {
				return st
			}
			id := pool[c.rng(len(pool))]
			return addEnchantments(st, enchList{{id: id, lvl: int8(1 + c.rng(enchDefs[id].maxLevel))}})
		}, ""
	case "minecraft:enchant_with_levels": // EnchantWithLevelsFunction
		np, ok := numberProvider(m["levels"])
		if !ok {
			return bad("levels")
		}
		if _, ok := m["options"]; ok {
			return nil, fmt.Sprintf("The options of the loot function '%s' are not supported here", name)
		}
		return func(h *hub, c *lootCtx, st invStack) invStack {
			return addEnchantments(st, enchWithLevels(lootRand(c.rng), st.item, int(c.np(np))))
		}, ""
	case "minecraft:set_enchantments": // SetEnchantmentsFunction
		em, ok := m["enchantments"].(map[string]any)
		if !ok {
			return bad("enchantments")
		}
		type lvl struct {
			id int8
			np *lootNP
		}
		var set []lvl
		for k, raw := range em {
			id, ok := enchByName[strings.TrimPrefix(k, "minecraft:")]
			if !ok {
				return nil, fmt.Sprintf("Can't find element '%s' of type 'minecraft:enchantment'", nsID(k))
			}
			np, ok := numberProvider(raw)
			if !ok {
				return bad("enchantments")
			}
			set = append(set, lvl{id, np})
		}
		return func(h *hub, c *lootCtx, st invStack) invStack {
			if st.item == itemBook {
				st.item = itemEnchantedBook
			}
			for _, e := range set {
				n := int(c.np(e.np))
				if add != 0 {
					n += st.enchLvl(e.id)
				}
				n = min(max(n, 0), 255)
				if n == 0 {
					st.ench = enchSetLevel(st.ench, e.id, 0)
					continue
				}
				st.ench = enchSetLevel(st.ench, e.id, int8(min(n, 127)))
			}
			return st
		}, ""
	case "minecraft:set_potion": // SetPotionFunction
		id, _ := m["id"].(string)
		p, ok := potionByVanillaName[strings.TrimPrefix(id, "minecraft:")]
		if !ok {
			return nil, fmt.Sprintf("Can't find element '%s' of type 'minecraft:potion'", nsID(id))
		}
		return func(_ *hub, _ *lootCtx, st invStack) invStack {
			if st.item == itemArrow {
				st.item = itemTippedArrow
			}
			st.potion = p
			return st
		}, ""
	case "minecraft:set_name": // SetNameFunction (custom_name or item_name)
		text, ok := textOf(m["name"])
		if !ok {
			return bad("name")
		}
		return func(_ *hub, _ *lootCtx, st invStack) invStack { st.name = text; return st }, ""
	case "minecraft:set_lore": // SetLoreFunction: replace_all, append, insert
		list, ok := m["lore"].([]any)
		if !ok {
			return bad("lore")
		}
		var lines []string
		for _, l := range list {
			s, ok := textOf(l)
			if !ok {
				return bad("lore")
			}
			lines = append(lines, s)
		}
		mode, _ := m["mode"].(string)
		offset := 0
		if o, ok := snbtInt(m["offset"]); ok {
			offset = int(o)
		}
		switch mode {
		case "", "replace_all", "append", "insert":
		default:
			return nil, fmt.Sprintf("The lore mode '%s' is not supported here", mode)
		}
		return func(_ *hub, _ *lootCtx, st invStack) invStack {
			old := st.tags.loreLines()
			var out []string
			switch mode {
			case "append":
				out = append(append(out, old...), lines...)
			case "insert":
				at := min(max(offset, 0), len(old))
				out = append(append(append(out, old[:at]...), lines...), old[at:]...)
			default:
				out = lines
			}
			if len(out) > 256 {
				out = out[:256]
			}
			st.tags.lore = strings.Join(out, "\n")
			return st
		}, ""
	case "minecraft:set_item": // SetItemFunction
		id, _ := m["item"].(string)
		item, ok := itemByName[strings.TrimPrefix(id, "minecraft:")]
		if !ok {
			return nil, fmt.Sprintf("Unknown item '%s'", nsID(id))
		}
		return func(_ *hub, _ *lootCtx, st invStack) invStack { st.item = item; return st }, ""
	case "minecraft:set_components": // SetComponentsFunction: the patch, removals as !id
		comps, ok := m["components"].(map[string]any)
		if !ok {
			return bad("components")
		}
		// Validate once against a probe; each stack gets the same patch.
		probe := invStack{item: itemByName["stick"], count: 1}
		for k, v := range comps {
			if msg := applyComponentPatch(&probe, k, v); msg != "" {
				return nil, msg
			}
		}
		return func(_ *hub, _ *lootCtx, st invStack) invStack {
			for k, v := range comps {
				cp := st
				if applyComponentPatch(&cp, k, v) == "" {
					st = cp
				}
			}
			return st
		}, ""
	case "minecraft:limit_count": // LimitCount: an int, or {min, max}
		lo, hi := math.MinInt32, math.MaxInt32
		switch l := m["limit"].(type) {
		case map[string]any:
			if v, ok := snbtInt(l["min"]); ok {
				lo = int(v)
			}
			if v, ok := snbtInt(l["max"]); ok {
				hi = int(v)
			}
		default:
			v, ok := snbtInt(l)
			if !ok {
				return bad("limit")
			}
			lo, hi = int(v), int(v)
		}
		return func(_ *hub, _ *lootCtx, st invStack) invStack {
			st.count = min(max(st.count, lo), hi)
			return st
		}, ""
	case "minecraft:explosion_decay": // no explosion radius in a command's context
		return func(_ *hub, _ *lootCtx, st invStack) invStack { return st }, ""
	case "minecraft:furnace_smelt": // SmeltItemFunction
		return func(_ *hub, _ *lootCtx, st invStack) invStack {
			if r, ok := smeltResult[st.item]; ok {
				st.item = r.Out
			}
			return st
		}, ""
	case "minecraft:set_ominous_bottle_amplifier":
		np, ok := numberProvider(m["amplifier"])
		if !ok {
			return bad("amplifier")
		}
		return func(h *hub, c *lootCtx, st invStack) invStack {
			if st.item == itemOminousBottle {
				st.potion = int8(min(max(int(c.np(np)), 0), 4)) + 1
			}
			return st
		}, ""
	case "minecraft:set_instrument":
		opts, _ := m["options"].(string)
		f := &lootFn{F: "set_instrument", Options: strings.TrimPrefix(strings.TrimPrefix(opts, "#"), "minecraft:")}
		if f.Options == "screaming_goat_horns" {
			f.Options = "screaming"
		}
		return func(h *hub, c *lootCtx, st invStack) invStack {
			if st.item != itemGoatHorn {
				return st
			}
			return h.applyChestExtraFn(c, f, st)
		}, ""
	case "minecraft:set_stew_effect":
		list, _ := m["effects"].([]any)
		var effs []string
		for _, e := range list {
			em, _ := e.(map[string]any)
			t, _ := em["type"].(string)
			if _, ok := effectNames[strings.TrimPrefix(t, "minecraft:")]; !ok {
				return nil, fmt.Sprintf("Can't find element '%s' of type 'minecraft:mob_effect'", nsID(t))
			}
			effs = append(effs, strings.TrimPrefix(t, "minecraft:"))
		}
		f := &lootFn{F: "set_stew", Effects: effs}
		return func(h *hub, c *lootCtx, st invStack) invStack {
			if st.item != itemByName["suspicious_stew"] {
				return st
			}
			return h.applyChestExtraFn(c, f, st)
		}, ""
	}
	return nil, fmt.Sprintf("The loot function '%s' is not supported here", name)
}

// applyComponentPatch is one entry of a component patch: key=value sets,
// !key removes.
func applyComponentPatch(st *invStack, key string, v any) string {
	if k, ok := strings.CutPrefix(key, "!"); ok {
		k = nsID(k)
		if !dataComponentTypes[strings.TrimPrefix(k, "minecraft:")] {
			return fmt.Sprintf("Unknown item component '%s'", k)
		}
		return removeItemComponent(st, k)
	}
	key = nsID(key)
	if !dataComponentTypes[strings.TrimPrefix(key, "minecraft:")] {
		return fmt.Sprintf("Unknown item component '%s'", key)
	}
	return applyItemComponent(st, key, v)
}

// numberProvider reads a NumberProvider: a number, {type:constant,value},
// {type:uniform,min,max} (or a bare {min,max}), {type:binomial,n,p}.
func numberProvider(v any) (*lootNP, bool) {
	if f, ok := snbtFloat(v); ok {
		return &lootNP{T: "const", V: f}, true
	}
	if b, ok := v.(bool); ok {
		return &lootNP{T: "const", V: map[bool]float64{true: 1}[b]}, true
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, false
	}
	t, _ := m["type"].(string)
	switch nsID(t) {
	case "minecraft:constant":
		f, ok := snbtFloat(m["value"])
		return &lootNP{T: "const", V: f}, ok
	case "minecraft:", "minecraft:uniform":
		lo, ok1 := numberProvider(m["min"])
		hi, ok2 := numberProvider(m["max"])
		return &lootNP{T: "uniform", Min: lo, Max: hi}, ok1 && ok2
	case "minecraft:binomial":
		n, ok1 := numberProvider(m["n"])
		p, ok2 := numberProvider(m["p"])
		return &lootNP{T: "binomial", N: n, P: p}, ok1 && ok2
	}
	return nil, false
}

// enchantmentOptions reads enchant_randomly's options: one id, a #tag, or a
// list of ids.
func enchantmentOptions(v any) ([]int8, string) {
	var names []string
	switch o := v.(type) {
	case string:
		if tag, ok := strings.CutPrefix(o, "#"); ok {
			return nil, fmt.Sprintf("The enchantment tag '#%s' is not supported here", nsID(tag))
		}
		names = []string{o}
	case []any:
		for _, e := range o {
			s, ok := e.(string)
			if !ok {
				return nil, "Failed to parse structure: options"
			}
			names = append(names, s)
		}
	default:
		return nil, "Failed to parse structure: options"
	}
	ids := make([]int8, 0, len(names))
	for _, n := range names {
		id, ok := enchByName[strings.TrimPrefix(n, "minecraft:")]
		if !ok {
			return nil, fmt.Sprintf("Can't find element '%s' of type 'minecraft:enchantment'", nsID(n))
		}
		ids = append(ids, id)
	}
	return ids, ""
}

// addEnchantments is EnchantmentHelper.updateEnchantments adding: each
// enchantment goes on at its level, a book becoming an enchanted book.
func addEnchantments(st invStack, e enchList) invStack {
	if e == (enchList{}) {
		return st
	}
	if st.item == itemBook {
		st.item = itemEnchantedBook
	}
	for _, a := range e {
		if a.lvl > 0 {
			st.ench = enchSetLevel(st.ench, a.id, a.lvl)
		}
	}
	return st
}
