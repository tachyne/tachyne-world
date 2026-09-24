package server

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Game-rule names.
//
// Vanilla renamed the whole set to snake_case — doDaylightCycle became
// advance_time, doTileDrops became block_drops — and tachyne was still using
// the pre-rename spellings everywhere: in /gamerule, in tab completion and in
// the persisted settings.json.
//
// A straight rename would break muscle memory and every existing world's
// settings file, so BOTH spellings work: the canonical name is what /gamerule
// lists and what gets written from now on, and the legacy name is accepted
// silently. Old settings.json keys keep loading because the struct tags are
// unchanged — the file format is the one thing not being renamed.

// gameruleAlias maps a legacy rule name to its canonical vanilla name. Only
// the ones vanilla actually renamed appear here.
var gameruleAlias = map[string]string{
	"doDaylightCycle":           "advance_time",
	"doWeatherCycle":            "advance_weather",
	"doMobSpawning":             "spawn_mobs",
	"doTileDrops":               "block_drops",
	"doMobLoot":                 "mob_drops",
	"keepInventory":             "keep_inventory",
	"mobGriefing":               "mob_griefing",
	"naturalRegeneration":       "natural_health_regeneration",
	"fallDamage":                "fall_damage",
	"drowningDamage":            "drowning_damage",
	"fireDamage":                "fire_damage",
	"announceAdvancements":      "show_advancement_messages",
	"showDeathMessages":         "show_death_messages",
	"doImmediateRespawn":        "immediate_respawn",
	"randomTickSpeed":           "random_tick_speed",
	"playersSleepingPercentage": "players_sleeping_percentage",
	"locatorBar":                "locator_bar",
	"doTraderSpawning":          "spawn_wandering_traders",
	"freezeDamage":              "freeze_damage",
	"doVinesSpread":             "spread_vines",
	"spawnMonsters":             "spawn_monsters",
	"spawnerBlocksEnabled":      "spawner_blocks_work",
	"forgiveDeadPlayers":        "forgive_dead_players",
	"enderPearlsVanishOnDeath":  "ender_pearls_vanish_on_death",
	"doEntityDrops":             "entity_drops",
	"blockExplosionDropDecay":   "block_explosion_drop_decay",
	"mobExplosionDropDecay":     "mob_explosion_drop_decay",
	"tntExplosionDropDecay":     "tnt_explosion_drop_decay",
	"maxEntityCramming":         "max_entity_cramming",
	"spawnRadius":               "respawn_radius",
	"snowAccumulationHeight":    "max_snow_accumulation_height",
	"universalAnger":            "universal_anger",
	"doLimitedCrafting":         "limited_crafting",
}

// booleanRules is every boolean rule tachyne enforces, canonical names.
var booleanRules = []string{
	"keep_inventory", "advance_time", "spawn_mobs", "mob_griefing",
	"advance_weather", "block_drops", "mob_drops",
	"natural_health_regeneration", "fall_damage", "drowning_damage",
	"fire_damage", "show_advancement_messages", "show_death_messages",
	"immediate_respawn", "locator_bar",
	// Added 2026-07-26 — each gates something the engine actually does.
	"spawn_phantoms", "spawn_patrols", "spawn_wardens", "raids",
	"tnt_explodes", "water_source_conversion", "lava_source_conversion",
	"player_movement_check", "elytra_movement_check", "pvp",
	// Added 2026-09-18 — the rest of vanilla's set the engine has a
	// mechanic for.
	"freeze_damage", "spread_vines", "spawn_monsters", "spawner_blocks_work",
	"forgive_dead_players", "ender_pearls_vanish_on_death", "entity_drops",
	"block_explosion_drop_decay", "mob_explosion_drop_decay",
	"tnt_explosion_drop_decay", "spawn_wandering_traders", "universal_anger",
	// Added 2026-09-20 — vanilla rules the engine has a mechanic for.
	"allow_entering_nether_using_portals", "projectiles_can_break_blocks",
	"global_sound_events", "limited_crafting",
}

// numericRules is the same for the rules that take a number.
var numericRules = []string{"random_tick_speed", "players_sleeping_percentage", "max_entity_cramming", "respawn_radius", "max_snow_accumulation_height",
	// 1.21.9 replaced the boolean doFireTick with this radius: fire spreads
	// only within it of a player, -1 being everywhere and 0 nowhere.
	"fire_spread_radius_around_player",
	// The two nether-portal dwell delays, which were hard-coded at vanilla's
	// values rather than settable.
	"players_nether_portal_default_delay", "players_nether_portal_creative_delay"}

// canonicalRule resolves either spelling to the canonical name, and reports
// whether it is a rule at all.
func canonicalRule(name string) (string, bool) {
	if c, ok := gameruleAlias[name]; ok {
		return c, true
	}
	for _, r := range booleanRules {
		if r == name {
			return r, true
		}
	}
	for _, r := range numericRules {
		if r == name {
			return r, true
		}
	}
	return "", false
}

// isNumericRule reports whether a canonical rule takes a number.
func isNumericRule(name string) bool {
	for _, r := range numericRules {
		if r == name {
			return true
		}
	}
	return false
}

// evRuleQuery is /gamerule <rule> with no value: its current value.
type evRuleQuery struct {
	eid  int32
	rule string
}

func (evRuleQuery) isHubEvent() {}

// ruleValueText reads a rule's current value off the rules as they are
// persisted: the struct's JSON keys are the legacy camelCase names, which
// is what the alias table maps back to (a rule vanilla never renamed is its
// snake_case name in camelCase).
func (h *hub) ruleValueText(rule string) (string, bool) {
	raw, err := json.Marshal(h.rules)
	if err != nil {
		return "", false
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return "", false
	}
	// The key is the rule's camelCase name, or, for a field that kept its
	// pre-rename spelling, the legacy name the alias table maps from.
	parts := strings.Split(rule, "_")
	for i := 1; i < len(parts); i++ {
		if parts[i] != "" {
			parts[i] = strings.ToUpper(parts[i][:1]) + parts[i][1:]
		}
	}
	keys := []string{strings.Join(parts, "")}
	for legacy, canon := range gameruleAlias {
		if canon == rule {
			keys = append(keys, legacy)
		}
	}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			return fmt.Sprint(v), true
		}
	}
	return "", false
}
