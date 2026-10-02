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
	seen := map[string]bool{}
	for comps != "" {
		if strings.HasPrefix(comps, "!") { // a removal: the component goes
			end := strings.IndexByte(comps, ',')
			if end < 0 {
				end = len(comps)
			}
			key := nsID(strings.TrimSpace(comps[1:end]))
			if msg := componentKnown(key, seen); msg != "" {
				return invStack{}, msg
			}
			if msg := removeItemComponent(&st, key); msg != "" {
				return invStack{}, msg
			}
			comps = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(comps[end:]), ","))
			continue
		}
		key, rest, ok := strings.Cut(comps, "=")
		if !ok {
			return invStack{}, fmt.Sprintf("Expected '=' after component '%s'", strings.TrimSpace(comps))
		}
		key = nsID(strings.TrimSpace(key))
		if msg := componentKnown(key, seen); msg != "" {
			return invStack{}, msg
		}
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

// componentKnown is ItemParser's check on a component id: one of 26.3's
// data component types, and not named twice in the list.
func componentKnown(key string, seen map[string]bool) string {
	if !dataComponentTypes[strings.TrimPrefix(key, "minecraft:")] {
		return fmt.Sprintf("Unknown item component '%s'", key)
	}
	if seen[key] {
		return fmt.Sprintf("Item component '%s' was repeated, but only one value can be specified", key)
	}
	seen[key] = true
	return ""
}

// dataComponentTypes is 26.3's minecraft:data_component_type registry.
var dataComponentTypes = func() map[string]bool {
	m := map[string]bool{}
	for _, n := range strings.Fields(`custom_data max_stack_size max_damage damage unbreakable use_effects
		custom_name minimum_attack_charge damage_type item_name item_model lore rarity enchantments
		can_place_on can_break attribute_modifiers custom_model_data tooltip_display repair_cost
		creative_slot_lock enchantment_glint_override intangible_projectile food consumable
		use_remainder use_cooldown damage_resistant tool weapon attack_range enchantable equippable
		repairable glider tooltip_style death_protection blocks_attacks piercing_weapon
		kinetic_weapon attack_animation interact_animation additional_trade_cost block_transformer
		villager_food stored_enchantments dye dyed_color map_id map_decorations map_post_processing
		charged_projectiles bundle_contents potion_contents potion_duration_scale
		suspicious_stew_effects writable_book_content written_book_content trim debug_stick_state
		entity_data bucket_entity_data block_entity_data instrument provides_trim_material
		ominous_bottle_amplifier jukebox_playable provides_banner_patterns recipes lodestone_tracker
		firework_explosion fireworks profile note_block_sound banner_patterns base_color
		pot_decorations container block_state bees sulfur_cube_content lock container_loot
		break_sound compostable cooking_fuel brewing_fuel mob_visibility villager/variant
		wolf/variant wolf/sound_variant wolf/collar fox/variant salmon/size parrot/variant
		tropical_fish/pattern tropical_fish/base_color tropical_fish/pattern_color mooshroom/variant
		rabbit/variant pig/variant pig/sound_variant cow/variant cow/sound_variant chicken/variant
		chicken/sound_variant zombie_nautilus/variant frog/variant horse/variant painting/variant
		llama/variant axolotl/variant cat/variant cat/sound_variant cat/collar sheep/color
		shulker/color provides_pottery_pattern sign_text_front sign_text_back waxed cushion/color`) {
		m[n] = true
	}
	return m
}()

// removeItemComponent is a !component in the list: the stack drops that
// component. The ones the engine keeps on a stack clear; one it does not
// model it cannot take off either, and says so.
func removeItemComponent(st *invStack, key string) string {
	switch key {
	case "minecraft:enchantments", "minecraft:stored_enchantments":
		st.ench = enchList{}
	case "minecraft:custom_name":
		st.name = ""
	case "minecraft:damage":
		st.dmg = 0
	case "minecraft:repair_cost":
		st.repairCost = 0
	case "minecraft:potion_contents":
		st.potion = potNone
	case "minecraft:dyed_color":
		st.color = 0
	case "minecraft:lore":
		st.tags.lore = ""
	case "minecraft:unbreakable":
		st.tags.unbreakable = false
	case "minecraft:can_break":
		st.tags.canBreak = ""
	case "minecraft:can_place_on":
		st.tags.canPlace = ""
	case "minecraft:trim":
		st.trimMat, st.trimPat = 0, 0
	case "minecraft:banner_patterns":
		st.pats = [6]bannerLayer{}
	case "minecraft:fireworks", "minecraft:firework_explosion":
		st.starID, st.flight = 0, 0
	case "minecraft:charged_projectiles":
		st.load = xbowLoad{}
	case "minecraft:bucket_entity_data":
		st.cube = cubeContent{}
	case "minecraft:entity_data":
		st.standTags = ""
	case "minecraft:profile":
		st.profile = ""
	case "minecraft:note_block_sound":
		st.noteSound = ""
	case "minecraft:written_book_content", "minecraft:writable_book_content":
		st.bookID = 0
	default:
		return fmt.Sprintf("The '%s' component is not supported here", key)
	}
	return ""
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
	case "minecraft:entity_data":
		m, ok := v.(map[string]any)
		if !ok {
			return bad()
		}
		if _, ok := m["id"].(string); !ok {
			return bad() // TypedEntityData needs its id
		}
		if st.item != itemArmorStand {
			return fmt.Sprintf("The '%s' component is not supported here", key)
		}
		if tags, ok := standTagsFromEntityData(m); ok {
			st.standTags = tags
		}
	case "minecraft:profile":
		// ResolvableProfile.CODEC: a name, or {name, id, properties}.
		if st.item != itemPlayerHead {
			return fmt.Sprintf("The '%s' component is not supported here", key)
		}
		p, ok := profileFromSNBT(v)
		if !ok {
			return bad()
		}
		st.profile = profileString(p)
	case "minecraft:note_block_sound":
		// Identifier.CODEC: any item may carry it; a placed player head
		// keeps it for the note block under it.
		id, ok := noteSoundFromSNBT(v)
		if !ok {
			return bad()
		}
		st.noteSound = id
	default:
		return applyItemTagComponent(st, key, v)
	}
	return ""
}
