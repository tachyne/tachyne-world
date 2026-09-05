package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The distilled conditions in advancements_gen.go against the matcher, one
// advancement per newly observable trigger: the payload an engine site would
// send must satisfy the real criterion, and a near miss must not. This pins
// both the generator's distillation and the matcher for each trigger shape.
func critOf(t *testing.T, adv, crit string) *advCriterion {
	t.Helper()
	n := advByID[adv]
	if n == nil {
		t.Fatalf("no advancement %s", adv)
	}
	for i := range n.criteria {
		if n.criteria[i].name == crit {
			if n.criteria[i].unmatchable {
				t.Fatalf("%s/%s is still flagged unmatchable", adv, crit)
			}
			return &n.criteria[i]
		}
	}
	t.Fatalf("no criterion %s in %s", crit, adv)
	return nil
}

func TestAdvancementTriggerShapes(t *testing.T) {
	item := func(name string) int32 { return int32(itemByName[name]) }
	block := func(name string, props map[string]string) uint32 {
		s := worldgen.BlockBase(name)
		if len(props) == 0 {
			return s
		}
		return withProps(t, s, props)
	}
	cases := []struct {
		adv, crit string
		yes, no   advMatch
	}{
		{"minecraft:adventure/ol_betsy", "shot_crossbow",
			advMatch{item: item("crossbow")}, advMatch{item: item("bow")}},
		{"minecraft:adventure/totem_of_undying", "used_totem",
			advMatch{item: item("totem_of_undying")}, advMatch{item: item("stick")}},
		{"minecraft:husbandry/tactical_fishing", "cod_bucket",
			advMatch{item: item("cod_bucket")}, advMatch{item: item("water_bucket")}},
		{"minecraft:adventure/under_lock_and_key", "under_lock_and_key",
			advMatch{blockState: block("vault", map[string]string{"ominous": "false"}), item: item("trial_key")},
			advMatch{blockState: block("vault", map[string]string{"ominous": "true"}), item: item("trial_key")}},
		{"minecraft:adventure/revaulting", "revaulting",
			advMatch{blockState: block("vault", map[string]string{"ominous": "true"}), item: item("ominous_trial_key")},
			advMatch{blockState: block("vault", map[string]string{"ominous": "true"}), item: item("trial_key")}},
		{"minecraft:nether/charge_respawn_anchor", "charge_respawn_anchor",
			advMatch{blockState: block("respawn_anchor", map[string]string{"charges": "4"}), item: item("glowstone")},
			advMatch{blockState: block("respawn_anchor", map[string]string{"charges": "3"}), item: item("glowstone")}},
		{"minecraft:adventure/play_jukebox_in_meadows", "play_jukebox_in_meadows",
			advMatch{blockState: block("jukebox", nil), item: item("music_disc_cat"), biome: "meadow"},
			advMatch{blockState: block("jukebox", nil), item: item("music_disc_cat"), biome: "plains"}},
		{"minecraft:husbandry/safely_harvest_honey", "safely_harvest_honey",
			advMatch{blockState: block("beehive", map[string]string{"honey_level": "5"}), item: item("glass_bottle"), smokey: true},
			advMatch{blockState: block("beehive", map[string]string{"honey_level": "5"}), item: item("glass_bottle"), smokey: false}},
		{"minecraft:husbandry/make_a_sign_glow", "make_a_sign_glow",
			advMatch{blockState: block("oak_sign", nil), item: item("glow_ink_sac")},
			advMatch{blockState: block("oak_sign", nil), item: item("ink_sac")}},
		{"minecraft:adventure/summon_iron_golem", "summoned_golem",
			advMatch{entity: "iron_golem"}, advMatch{entity: "snow_golem"}},
		{"minecraft:nether/summon_wither", "summoned",
			advMatch{entity: "wither"}, advMatch{entity: "wither_skeleton"}},
		{"minecraft:husbandry/feed_snifflet", "feed_snifflet",
			advMatch{entity: "sniffer", baby: true, item: item("torchflower_seeds")},
			advMatch{entity: "sniffer", baby: false, item: item("torchflower_seeds")}},
		{"minecraft:adventure/craft_decorated_pot_using_only_sherds", "pot_crafted_using_only_sherds",
			advMatch{recipe: "decorated_pot", ingredients: []int32{item("angler_pottery_sherd"), item("arms_up_pottery_sherd"), item("blade_pottery_sherd"), item("brewer_pottery_sherd")}},
			advMatch{recipe: "decorated_pot", ingredients: []int32{item("brick"), item("brick"), item("brick"), item("brick")}}},
		{"minecraft:adventure/crafters_crafting_crafters", "crafter_crafted_crafter",
			advMatch{recipe: "crafter"}, advMatch{recipe: "dropper"}},
		{"minecraft:adventure/salvage_sherd", "desert_well",
			advMatch{lootTable: "archaeology/desert_well"}, advMatch{lootTable: "archaeology/desert_pyramid"}},
		{"minecraft:adventure/shoot_arrow", "shot_arrow",
			advMatch{damageDirect: "arrow", damageTags: map[string]bool{"is_projectile": true}, dealt: 4},
			advMatch{damageDirect: "player", damageTags: map[string]bool{}, dealt: 4}},
		{"minecraft:adventure/throw_trident", "shot_trident",
			advMatch{damageDirect: "trident", damageTags: map[string]bool{"is_projectile": true}},
			advMatch{damageDirect: "arrow", damageTags: map[string]bool{"is_projectile": true}}},
		{"minecraft:adventure/overoverkill", "overoverkill",
			advMatch{damageDirect: "player", mainhand: item("mace"), damageTags: map[string]bool{"mace_smash": true}, dealt: 120},
			advMatch{damageDirect: "player", mainhand: item("mace"), damageTags: map[string]bool{"mace_smash": true}, dealt: 99}},
		{"minecraft:story/deflect_arrow", "deflected_projectile",
			advMatch{blocked: true, damageTags: map[string]bool{"is_projectile": true}},
			advMatch{blocked: false, damageTags: map[string]bool{"is_projectile": true}}},
		{"minecraft:adventure/whos_the_pillager_now", "kill_pillager",
			advMatch{item: item("crossbow"), victims: []string{"pillager"}},
			advMatch{item: item("bow"), victims: []string{"pillager"}}},
		{"minecraft:adventure/two_birds_one_arrow", "two_birds",
			advMatch{item: item("crossbow"), victims: []string{"phantom", "phantom"}},
			advMatch{item: item("crossbow"), victims: []string{"phantom", "zombie"}}},
		{"minecraft:adventure/arbalistic", "arbalistic",
			advMatch{item: item("crossbow"), victims: []string{"zombie", "skeleton", "creeper", "spider", "pig"}},
			advMatch{item: item("crossbow"), victims: []string{"zombie", "zombie", "zombie", "zombie", "zombie"}}},
		{"minecraft:adventure/spyglass_at_ghast", "spyglass_at_ghast",
			advMatch{item: item("spyglass"), lookingAt: "ghast"}, advMatch{item: item("spyglass"), lookingAt: "blaze"}},
		{"minecraft:end/enter_end_gateway", "entered_end_gateway",
			advMatch{blockState: block("end_gateway", nil)}, advMatch{blockState: block("end_portal", nil)}},
		{"minecraft:husbandry/silk_touch_nest", "silk_touch_nest",
			advMatch{blockState: block("bee_nest", nil), count: 3, enchant: "silk_touch"},
			advMatch{blockState: block("bee_nest", nil), count: 2, enchant: "silk_touch"}},
		{"minecraft:adventure/bullseye", "bullseye",
			advMatch{signal: 15, distH: 31}, advMatch{signal: 15, distH: 12}},
		{"minecraft:end/levitate", "levitated",
			advMatch{distY: 50}, advMatch{distY: 49}},
		{"minecraft:nether/fast_travel", "travelled",
			advMatch{distH: 7000}, advMatch{distH: 6999}},
		{"minecraft:adventure/fall_from_world_height", "fall_from_world_height",
			advMatch{distY: 380, startY: 319, endY: -60}, advMatch{distY: 380, startY: 300, endY: -60}},
		{"minecraft:adventure/who_needs_rockets", "who_needs_rockets",
			advMatch{distY: 7, cause: "wind_charge"}, advMatch{distY: 7, cause: ""}},
		{"minecraft:adventure/lightning_rod_with_villager_no_fire", "lightning_rod_with_villager_no_fire",
			advMatch{bystander: true, noFire: true}, advMatch{bystander: true, noFire: false}},
		{"minecraft:nether/all_potions", "all_effects",
			advMatch{effects: allOf(effectNames, "fire_resistance", "infested", "invisibility", "jump_boost", "night_vision", "oozing", "poison", "regeneration", "resistance", "slow_falling", "slowness", "speed", "strength", "water_breathing", "weakness", "weaving", "wind_charged")},
			advMatch{effects: allOf(effectNames, "speed")}},
		{"minecraft:story/follow_ender_eye", "in_stronghold",
			advMatch{structure: "stronghold"}, advMatch{structure: ""}},
		{"minecraft:adventure/minecraft_trials_edition", "minecraft_trials_edition",
			advMatch{structure: "trial_chambers"}, advMatch{structure: "stronghold"}},
		{"minecraft:adventure/walk_on_powder_snow_with_leather_boots", "walk_on_powder_snow_with_leather_boots",
			advMatch{blockState: block("powder_snow", nil), feet: item("leather_boots")},
			advMatch{blockState: block("powder_snow", nil), feet: item("iron_boots")}},
	}
	for _, c := range cases {
		crit := critOf(t, c.adv, c.crit)
		if !c.yes.criterion(crit) {
			t.Errorf("%s/%s: the matching payload was rejected", c.adv, c.crit)
		}
		if c.no.criterion(crit) {
			t.Errorf("%s/%s: the near-miss payload was accepted", c.adv, c.crit)
		}
	}
}

func allOf(names map[string]int32, want ...string) map[int32]bool {
	m := map[int32]bool{}
	for _, n := range want {
		m[names[n]] = true
	}
	return m
}

// placed_block with offset location checks: the comparator reading a
// chiseled bookshelf must face it.
func TestPlacedBlockOffsetChecks(t *testing.T) {
	crit := critOf(t, "minecraft:adventure/read_power_of_chiseled_bookshelf", "comparator")
	comp := withProps(t, worldgen.BlockBase("comparator"), map[string]string{"facing": "north"})
	shelf := worldgen.BlockBase("chiseled_bookshelf")
	world := map[[3]int]uint32{{0, 0, 0}: comp, {0, 0, -1}: shelf}
	at := func(dx, dy, dz int) uint32 { return world[[3]int{dx, dy, dz}] }
	if !(advMatch{blockState: comp, blockAt: at}).criterion(crit) {
		t.Error("comparator facing north with a bookshelf to the north should match")
	}
	world[[3]int{0, 0, -1}] = worldgen.Stone
	if (advMatch{blockState: comp, blockAt: at}).criterion(crit) {
		t.Error("comparator facing a stone block must not match")
	}
}
