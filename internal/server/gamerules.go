package server

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
	"doFireTick":                "fire_ticks",
}

// booleanRules is every boolean rule tachyne enforces, canonical names.
var booleanRules = []string{
	"keep_inventory", "advance_time", "spawn_mobs", "mob_griefing",
	"advance_weather", "fire_ticks", "block_drops", "mob_drops",
	"natural_health_regeneration", "fall_damage", "drowning_damage",
	"fire_damage", "show_advancement_messages", "show_death_messages",
	"immediate_respawn", "locator_bar",
	// Added 2026-07-26 — each gates something the engine actually does.
	"spawn_phantoms", "spawn_patrols", "spawn_wardens", "raids",
	"tnt_explodes", "water_source_conversion", "lava_source_conversion",
	"player_movement_check", "elytra_movement_check", "pvp",
}

// numericRules is the same for the rules that take a number.
var numericRules = []string{"random_tick_speed", "players_sleeping_percentage"}

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
