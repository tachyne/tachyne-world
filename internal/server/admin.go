package server

import (
	"encoding/json"
	"fmt"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"log"
	"strconv"
	"strings"

	"github.com/tachyne/tachyne-world/plugin"
)

// Admin commands + world settings: /give /kill /summon /xp, the difficulty
// setting (scales hostile damage; peaceful clears and blocks hostiles), and a
// small gamerule set. Settings persist in settings.json.

const (
	diffPeaceful = 0
	diffEasy     = 1
	diffNormal   = 2
	diffHard     = 3
)

// worldRules is the persisted difficulty + gamerule state (hub-owned). The
// weather block is the vanilla WeatherData saved-data fields, snapshotted on
// every save so the cycle survives a restart mid-storm.
type worldRules struct {
	Difficulty    int  `json:"difficulty"`
	KeepInventory bool `json:"keepInventory"`
	DoDaylight    bool `json:"doDaylightCycle"`
	DoMobSpawning bool `json:"doMobSpawning"`
	MobGriefing   bool `json:"mobGriefing"`
	DoWeather     bool `json:"doWeatherCycle"`
	DoTileDrops   bool `json:"doTileDrops"`
	DoMobLoot     bool `json:"doMobLoot"`
	NaturalRegen  bool `json:"naturalRegeneration"`
	FallDamage    bool `json:"fallDamage"`
	DrownDamage   bool `json:"drowningDamage"`
	FireDamage    bool `json:"fireDamage"`
	AnnounceAdv   bool `json:"announceAdvancements"`
	ShowDeathMsgs bool `json:"showDeathMessages"`
	ImmediateResp bool `json:"doImmediateRespawn"`
	RandomTicks   int  `json:"randomTickSpeed"`
	// MaxCartSpeed is max_minecart_speed: the cap a cart may reach, in blocks
	// per second. Vanilla's default is 8, which is the engine's own top speed.
	MaxCartSpeed int `json:"maxMinecartSpeed"`
	// LimitedCrafting is limited_crafting: a player may only craft what their
	// recipe book has unlocked. Off by default, as in vanilla.
	LimitedCrafting bool `json:"doLimitedCrafting"`
	SleepPercent    int  `json:"playersSleepingPercentage"`
	LocatorBar      bool `json:"locatorBar"`
	// Added 2026-07-26. The JSON keys keep the historical spelling so an
	// existing settings.json still loads; only the COMMAND surface renamed.
	SpawnPhantoms  bool `json:"spawnPhantoms"`
	SpawnPatrols   bool `json:"spawnPatrols"`
	SpawnWardens   bool `json:"spawnWardens"`
	Raids          bool `json:"raids"`
	TNTExplodes    bool `json:"tntExplodes"`
	WaterSourceCnv bool `json:"waterSourceConversion"`
	LavaSourceCnv  bool `json:"lavaSourceConversion"`
	MovementCheck  bool `json:"playerMovementCheck"`
	ElytraCheck    bool `json:"elytraMovementCheck"`
	PvP            bool `json:"pvp"`
	DragonDefeated bool `json:"dragonDefeated,omitempty"` // the End's fight is won
	// The dragon is not in mobs.json (bosses are not persisted), so without
	// this a restart mid-fight handed it back its full health. Zero means "no
	// fight in progress"; a live fight writes what it has left.
	DragonHealth int           `json:"dragonHealth,omitempty"`
	Weather      *weatherSave  `json:"weather,omitempty"`
	Border       *worldBorder  `json:"border,omitempty"`
	EndGateways  []gatewayExit `json:"endGateways,omitempty"` // each gateway's remembered exit
	// SpawnerMobs are the spawners a spawn egg was used on: "dim,x,y,z" → the
	// entity name they spawn instead of their dungeon's (SpawnEggItem.useOn).
	SpawnerMobs map[string]string `json:"spawnerMobs,omitempty"`
	// WanderingTraderSpawner state (ServerLevelData): the countdown to the
	// next roll and the rising chance; zero for both means "never rolled".
	DoTraderSpawning bool `json:"doTraderSpawning"`
	// Added 2026-09-18 — vanilla's remaining rules the engine has a mechanic
	// for. Missing keys in an older settings.json keep the defaults below.
	FreezeDamage   bool `json:"freezeDamage"`
	SpreadVines    bool `json:"spreadVines"`
	SpawnMonsters  bool `json:"spawnMonsters"`
	SpawnerBlocks  bool `json:"spawnerBlocksWork"`
	ForgiveDead    bool `json:"forgiveDeadPlayers"`
	PearlsVanish   bool `json:"enderPearlsVanishOnDeath"`
	EntityDrops    bool `json:"entityDrops"`
	BlockDropDecay bool `json:"blockExplosionDropDecay"`
	MobDropDecay   bool `json:"mobExplosionDropDecay"`
	TNTDropDecay   bool `json:"tntExplosionDropDecay"`
	MaxCramming    int  `json:"maxEntityCramming"`
	RespawnRadius  int  `json:"respawnRadius"`
	MaxSnowHeight  int  `json:"maxSnowAccumulationHeight"`
	// FireSpreadRadius is vanilla's fire_spread_radius_around_player, which
	// replaced doFireTick in 1.21.9: fire only spreads and burns out within
	// this many blocks of a player. -1 is everywhere, 0 is nowhere (which is
	// what the old doFireTick=false meant).
	FireSpreadRadius int `json:"fireSpreadRadiusAroundPlayer"`
	// The nether-portal rules: whether a portal will take you there at all,
	// and how long you have to stand in one first (vanilla's two delays,
	// 80 ticks in survival and 1 in creative).
	AllowNether       bool `json:"allowEnteringNetherUsingPortals"`
	PortalDelay       int  `json:"playersNetherPortalDefaultDelay"`
	PortalDelayCreate int  `json:"playersNetherPortalCreativeDelay"`
	// projectiles_can_break_blocks: whether an arrow or a trident may break
	// what it hits (a decorated pot, dripstone).
	ProjectilesBreak bool `json:"projectilesCanBreakBlocks"`
	// global_sound_events: whether a wither waking, the dragon dying and an
	// end portal opening are heard across the whole dimension.
	GlobalSounds bool `json:"globalSoundEvents"`
	// LegacyFireTick is the boolean doFireTick a world saved before the switch.
	// loadRules folds a stored false into FireSpreadRadius 0 and drops it, so a
	// server that had fire turned off keeps it off. Never written back.
	LegacyFireTick *bool `json:"doFireTick,omitempty"`
	UniversalAnger bool  `json:"universalAnger"`
	// send_command_feedback / log_admin_commands: whether a command's
	// success line reaches its caller and the other operators, and whether
	// the server log records what an operator changed (cmdfeedback.go).
	SendCommandFeedback bool `json:"sendCommandFeedback"`
	LogAdminCommands    bool `json:"logAdminCommands"`
	TraderSpawnDelay    int  `json:"wanderingTraderSpawnDelay,omitempty"`
	TraderSpawnChance   int  `json:"wanderingTraderSpawnChance,omitempty"`
	// Forced is /forceload's chunks, every dimension's (ForcedChunksSavedData).
	Forced []forcedChunk `json:"forcedChunks,omitempty"`
	// WorldSpawn is /setworldspawn's point (LevelData's respawn data). It
	// outranks the -spawn flag: a flag is where a fresh world starts, the
	// command is where the running one was moved to.
	WorldSpawn *worldSpawnSave `json:"worldSpawn,omitempty"`
	// DefaultGamemode is /defaultgamemode's mode for new players; nil keeps
	// the -gamemode flag's.
	DefaultGamemode *int `json:"defaultGamemode,omitempty"`
	// Bossbars are /bossbar's custom bars by id (CustomBossEvents, which
	// vanilla keeps in the level data).
	Bossbars map[string]*customBossbar `json:"customBossEvents,omitempty"`
}

func defaultRules() worldRules {
	return worldRules{Difficulty: diffNormal, DoDaylight: true, DoMobSpawning: true, DoTraderSpawning: true,
		MobGriefing: true, DoWeather: true, DoTileDrops: true,
		DoMobLoot: true, NaturalRegen: true, FallDamage: true, DrownDamage: true,
		FireDamage: true, AnnounceAdv: true, ShowDeathMsgs: true,
		RandomTicks: 3, SleepPercent: 100, LocatorBar: true, MaxCartSpeed: 8,
		SpawnPhantoms: true, SpawnPatrols: true, SpawnWardens: true, Raids: true,
		TNTExplodes: true, WaterSourceCnv: true, LavaSourceCnv: false,
		MovementCheck: true, ElytraCheck: true, PvP: true,
		FreezeDamage: true, SpreadVines: true, SpawnMonsters: true, SpawnerBlocks: true,
		ForgiveDead: true, PearlsVanish: true, EntityDrops: true,
		BlockDropDecay: true, MobDropDecay: true, TNTDropDecay: false,
		MaxCramming: maxEntityCramming, RespawnRadius: 10, MaxSnowHeight: 1,
		FireSpreadRadius: defaultFireSpreadRadius,
		AllowNether:      true, PortalDelay: portalDwellTicks, PortalDelayCreate: 0,
		ProjectilesBreak: true, GlobalSounds: true,
		SendCommandFeedback: true, LogAdminCommands: true}
}

// summonable maps /summon names to entity types.
var summonable = map[string]int{
	"cow": entityCow, "chicken": entityChicken, "pig": entityPig, "sheep": entitySheep,
	"zombie": entityZombie, "skeleton": entitySkeleton, "spider": entitySpider,
	"creeper": entityCreeper, "husk": entityHusk, "stray": entityStray,
	"drowned": entityDrowned, "slime": entitySlime, "enderman": entityEnderman,
	"witch": entityWitch, "ender_dragon": entityEnderDragon,
}

// summonableType is SummonCommand's entity argument, for the mobs the engine
// runs: the table above (every roster species joins it at init), and the
// Nether's own kit.
func summonableType(name string) (int, bool) {
	name = strings.TrimPrefix(name, "minecraft:")
	if et, ok := summonable[name]; ok {
		return et, true
	}
	if et, ok := entityByName[name]; ok && (netherConfigured(et) || et == entitySulfurCube) {
		return et, true
	}
	return 0, false
}

func (s *Server) cmdGive(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission to give items.")
		return
	}
	if len(args) < 2 {
		p.tell("Usage: /give <player|@selector> <item> [count]")
		return
	}
	item, ok := itemByName[strings.TrimPrefix(args[1], "minecraft:")]
	if !ok {
		p.tell("Unknown item: " + args[1])
		return
	}
	count := 1
	if len(args) >= 3 {
		if n, err := strconv.Atoi(args[2]); err == nil && n > 0 && n <= 6400 {
			count = n
		}
	}
	s.hub.post(evGive{target: args[0], by: p.eid, item: item, count: count})
	s.ok(p, fmt.Sprintf("Gave %d × %s to %s", count, args[1], args[0]))
}

func (s *Server) cmdKill(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	target := p.name
	if len(args) >= 1 {
		target = args[0]
	}
	s.hub.post(evKill{target: target, by: p.eid})
}

// cmdXP is ExperienceCommand (/xp and /experience): add, set or query a
// player's experience in points (the default) or levels.
func (s *Server) cmdXP(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	const usage = "Usage: /xp add|set <targets> <amount> [points|levels] | /xp query <target> points|levels"
	unit := "points"
	switch {
	case len(args) == 3 && args[0] == "query":
		unit = args[2]
	case len(args) == 4 && (args[0] == "add" || args[0] == "set"):
		unit = args[3]
	case len(args) == 3 && (args[0] == "add" || args[0] == "set"):
	default:
		p.tell(usage)
		return
	}
	if unit != "points" && unit != "levels" {
		p.tell(usage)
		return
	}
	n := 0
	if args[0] != "query" {
		v, err := strconv.Atoi(args[2])
		if err != nil || (args[0] == "set" && v < 0) {
			p.tell(usage)
			return
		}
		n = v
	}
	s.hub.post(evXP{op: args[0], target: args[1], by: p.eid, amount: n, levels: unit == "levels"})
}

func (s *Server) cmdSummon(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) < 1 {
		p.tell("Usage: /summon <mob> [<x> <y> <z>]")
		return
	}
	et, ok := summonableType(args[0])
	if !ok {
		p.tell("Unknown entity: " + args[0])
		return
	}
	x, y, z := p.x, p.y, p.z+2
	if len(args) >= 4 { // /summon <mob> <x> <y> <z>, with ~ and ^ like vanilla
		nx, ny, nz, ok := parsePosition(args[1:], p.x, p.y, p.z, p.yaw, p.pitch)
		if !ok {
			p.tell("Usage: /summon <mob> [<x> <y> <z>]")
			return
		}
		x, y, z = nx, ny, nz
	} else if len(args) >= 3 { // the old two-argument form: x and z
		if nx, ok := parseCoord(args[1], p.x); ok {
			x = nx
		}
		if nz, ok := parseCoord(args[2], p.z); ok {
			z = nz
		}
	}
	s.hub.post(evSummon{etype: et, x: x, z: z, dim: p.dim, y: y})
	s.ok(p, "Summoned "+args[0])
}

func (s *Server) cmdDifficulty(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	names := [...]string{"Peaceful", "Easy", "Normal", "Hard"}
	cur := int(s.hub.difficultyPub.Load())
	if len(args) == 0 { // DifficultyCommand's query form
		if cur >= 0 && cur < len(names) {
			p.tell("The difficulty is " + names[cur])
		}
		return
	}
	if len(args) != 1 {
		p.tell("Usage: /difficulty [peaceful|easy|normal|hard]")
		return
	}
	d := map[string]int{"peaceful": diffPeaceful, "easy": diffEasy, "normal": diffNormal, "hard": diffHard}
	v, ok := d[args[0]]
	if !ok {
		p.tell("Usage: /difficulty <peaceful|easy|normal|hard>")
		return
	}
	if v == cur {
		p.tell("The difficulty did not change; it is already set to " + names[v])
		return
	}
	s.hub.post(evSetRule{rule: "difficulty", num: v})
	s.ok(p, "The difficulty has been set to "+names[v])
}

func (s *Server) cmdGamerule(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) == 1 { // the query form: "Gamerule <rule> is currently set to: <value>"
		rule, ok := canonicalRule(args[0])
		if !ok {
			p.tell("Unknown gamerule: " + args[0])
			return
		}
		s.hub.post(evRuleQuery{eid: p.eid, rule: rule})
		return
	}
	if len(args) != 2 {
		p.tell("Gamerules: " + strings.Join(append(append([]string{}, booleanRules...), numericRules...), " "))
		return
	}
	// Either spelling works: the pre-rename names are aliases now, not errors.
	rule, ok := canonicalRule(args[0])
	if !ok {
		p.tell("Unknown gamerule: " + args[0])
		return
	}
	if isNumericRule(rule) {
		n, err := strconv.Atoi(args[1])
		if err != nil {
			p.tell("Invalid integer '" + args[1] + "'")
			return
		}
		lo, hi := gameruleBounds(rule)
		if n < lo {
			p.tell(fmt.Sprintf("Integer must not be less than %d, found %d", lo, n))
			return
		}
		if n > hi {
			p.tell(fmt.Sprintf("Integer must not be more than %d, found %d", hi, n))
			return
		}
		s.hub.post(evSetRule{rule: rule, num: n})
	} else {
		if args[1] != "true" && args[1] != "false" {
			p.tell("/gamerule " + rule + " <true|false>")
			return
		}
		s.hub.post(evSetRule{rule: rule, on: args[1] == "true"})
	}
	args[0] = rule
	s.ok(p, fmt.Sprintf("Gamerule %s = %s", args[0], args[1]))
}

type evGive struct {
	target string
	by     int32 // the caller, for @s/@p and distance predicates
	item   int32
	count  int
}
type evKill struct {
	target string
	by     int32
}
type evXP struct {
	op     string // add, set, query
	target string
	by     int32
	amount int
	levels bool // levels, else points
}
type evSummon struct {
	etype   int
	x, y, z float64
	dim     int
}
type evSetRule struct {
	rule string
	on   bool
	num  int
}

func (evGive) isHubEvent()    {}
func (evKill) isHubEvent()    {}
func (evXP) isHubEvent()      {}
func (evSummon) isHubEvent()  {}
func (evSetRule) isHubEvent() {}

// applyRule mutates the hub's world rules (and persists them).
func (h *hub) applyRule(players map[int32]*tracked, e evSetRule) {
	rule := e.rule
	if c, ok := canonicalRule(rule); ok {
		rule = c
	}
	switch rule {
	case "difficulty":
		h.rules.Difficulty = e.num
		h.difficultyPub.Store(int32(e.num))
		for _, t := range players { // live clients see the change too
			t.p.trySendEv(attachproto.Difficulty{Level: int32(e.num)})
		}
		if e.num == diffPeaceful { // peaceful clears the night out
			for _, m := range h.mobs {
				if m.hostile {
					h.removeMob(players, m)
				}
			}
		}
	case "keep_inventory":
		h.rules.KeepInventory = e.on
	case "advance_time":
		h.rules.DoDaylight = e.on
	case "spawn_mobs":
		h.rules.DoMobSpawning = e.on
	case "mob_griefing":
		h.rules.MobGriefing = e.on
	case "advance_weather":
		h.rules.DoWeather = e.on
	case "block_drops":
		h.rules.DoTileDrops = e.on
	case "mob_drops":
		h.rules.DoMobLoot = e.on
	case "natural_health_regeneration":
		h.rules.NaturalRegen = e.on
	case "fall_damage":
		h.rules.FallDamage = e.on
	case "drowning_damage":
		h.rules.DrownDamage = e.on
	case "fire_damage":
		h.rules.FireDamage = e.on
	case "show_advancement_messages":
		h.rules.AnnounceAdv = e.on
	case "show_death_messages":
		h.rules.ShowDeathMsgs = e.on
	case "immediate_respawn":
		h.rules.ImmediateResp = e.on
	case "random_tick_speed":
		h.rules.RandomTicks = e.num
	case "max_minecart_speed":
		h.rules.MaxCartSpeed = max(1, e.num)
	case "limited_crafting":
		h.rules.LimitedCrafting = e.on
	case "max_entity_cramming":
		h.rules.MaxCramming = max(0, e.num)
	case "respawn_radius":
		h.rules.RespawnRadius = max(0, e.num)
	case "fire_spread_radius_around_player":
		h.rules.FireSpreadRadius = max(-1, e.num)
	case "players_nether_portal_default_delay":
		h.rules.PortalDelay = max(0, e.num)
	case "players_nether_portal_creative_delay":
		h.rules.PortalDelayCreate = max(0, e.num)
	case "allow_entering_nether_using_portals":
		h.rules.AllowNether = e.on
	case "projectiles_can_break_blocks":
		h.rules.ProjectilesBreak = e.on
	case "send_command_feedback":
		h.rules.SendCommandFeedback = e.on
	case "log_admin_commands":
		h.rules.LogAdminCommands = e.on
	case "global_sound_events":
		h.rules.GlobalSounds = e.on
	case "max_snow_accumulation_height":
		h.rules.MaxSnowHeight = min(8, max(0, e.num))
	case "universal_anger":
		h.rules.UniversalAnger = e.on
	case "freeze_damage":
		h.rules.FreezeDamage = e.on
	case "spread_vines":
		h.rules.SpreadVines = e.on
	case "spawn_monsters":
		h.rules.SpawnMonsters = e.on
	case "spawner_blocks_work":
		h.rules.SpawnerBlocks = e.on
	case "forgive_dead_players":
		h.rules.ForgiveDead = e.on
	case "ender_pearls_vanish_on_death":
		h.rules.PearlsVanish = e.on
	case "entity_drops":
		h.rules.EntityDrops = e.on
	case "block_explosion_drop_decay":
		h.rules.BlockDropDecay = e.on
	case "mob_explosion_drop_decay":
		h.rules.MobDropDecay = e.on
	case "tnt_explosion_drop_decay":
		h.rules.TNTDropDecay = e.on
	case "spawn_wandering_traders":
		h.rules.DoTraderSpawning = e.on
	case "players_sleeping_percentage":
		h.rules.SleepPercent = e.num
	case "locator_bar":
		h.rules.LocatorBar = e.on
	case "spawn_phantoms":
		h.rules.SpawnPhantoms = e.on
	case "spawn_patrols":
		h.rules.SpawnPatrols = e.on
	case "spawn_wardens":
		h.rules.SpawnWardens = e.on
	case "raids":
		h.rules.Raids = e.on
	case "tnt_explodes":
		h.rules.TNTExplodes = e.on
	case "water_source_conversion":
		h.rules.WaterSourceCnv = e.on
	case "lava_source_conversion":
		h.rules.LavaSourceCnv = e.on
	case "player_movement_check":
		h.rules.MovementCheck = e.on
	case "elytra_movement_check":
		h.rules.ElytraCheck = e.on
	case "pvp":
		h.rules.PvP = e.on
	}
	h.saveRules()
	h.plugins.Fire(&plugin.GameruleChangeEvent{Rule: e.rule, On: e.on, Num: e.num})
}

// loadRules / saveRules persist the world settings as plain JSON.
func (h *hub) loadRules() {
	if h.rulesPath == "" {
		return
	}
	if err := loadStore(h.rulesPath, &h.rules); err != nil {
		log.Fatal(err)
	}
	if b := h.rules.LegacyFireTick; b != nil {
		if !*b { // the old doFireTick=false is the new radius 0
			h.rules.FireSpreadRadius = 0
		}
		h.rules.LegacyFireTick = nil
	}
	h.difficultyPub.Store(int32(h.rules.Difficulty))
	if h.rules.Border != nil {
		h.border = *h.rules.Border
	}
	if ws := h.rules.Weather; ws != nil {
		h.clearTime, h.rainTime, h.thunderTime = ws.ClearTime, ws.RainTime, ws.ThunderTime
		h.rainFlag, h.thunderFlag = ws.Raining, ws.Thundering
		// vanilla prepareWeather: a restored storm resumes at full level (no
		// fade-in replay for a sky that was already down-pouring).
		if ws.Raining {
			h.rainLevel, h.raining = 1, true
			if ws.Thundering {
				h.thunderLevel, h.thundering = 1, true
			}
		}
	}
}

// saveBorder persists the border through the same settings file the rules use.
func (h *hub) saveBorder() {
	h.rules.Border = &h.border
	h.saveRules()
}

func (h *hub) saveRules() {
	if h.rulesPath == "" {
		return
	}
	h.rules.Weather = &weatherSave{
		ClearTime: h.clearTime, RainTime: h.rainTime, ThunderTime: h.thunderTime,
		Raining: h.rainFlag, Thundering: h.thunderFlag,
	}
	data, _ := json.MarshalIndent(h.rules, "", "  ")
	writeStore(h.rulesPath, data)
}
