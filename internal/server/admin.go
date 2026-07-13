package server

import (
	"encoding/json"
	"fmt"
	attachproto "github.com/tachyne/tachyne-common/attach"
	"os"
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
	Difficulty     int          `json:"difficulty"`
	KeepInventory  bool         `json:"keepInventory"`
	DoDaylight     bool         `json:"doDaylightCycle"`
	DoMobSpawning  bool         `json:"doMobSpawning"`
	MobGriefing    bool         `json:"mobGriefing"`
	DoWeather      bool         `json:"doWeatherCycle"`
	DoFireTick     bool         `json:"doFireTick"`
	DoTileDrops    bool         `json:"doTileDrops"`
	DoMobLoot      bool         `json:"doMobLoot"`
	NaturalRegen   bool         `json:"naturalRegeneration"`
	FallDamage     bool         `json:"fallDamage"`
	DrownDamage    bool         `json:"drowningDamage"`
	FireDamage     bool         `json:"fireDamage"`
	AnnounceAdv    bool         `json:"announceAdvancements"`
	ShowDeathMsgs  bool         `json:"showDeathMessages"`
	ImmediateResp  bool         `json:"doImmediateRespawn"`
	RandomTicks    int          `json:"randomTickSpeed"`
	SleepPercent   int          `json:"playersSleepingPercentage"`
	DragonDefeated bool         `json:"dragonDefeated,omitempty"` // the End's fight is won
	Weather        *weatherSave `json:"weather,omitempty"`
}

func defaultRules() worldRules {
	return worldRules{Difficulty: diffNormal, DoDaylight: true, DoMobSpawning: true,
		MobGriefing: true, DoWeather: true, DoFireTick: true, DoTileDrops: true,
		DoMobLoot: true, NaturalRegen: true, FallDamage: true, DrownDamage: true,
		FireDamage: true, AnnounceAdv: true, ShowDeathMsgs: true,
		RandomTicks: 3, SleepPercent: 100}
}

// diffMult scales hostile-mob damage by difficulty (vanilla-ish).
func (h *hub) diffMult() float32 {
	switch h.rules.Difficulty {
	case diffEasy:
		return 0.5
	case diffHard:
		return 1.5
	}
	return 1
}

// summonable maps /summon names to entity types.
var summonable = map[string]int{
	"cow": entityCow, "chicken": entityChicken, "pig": entityPig, "sheep": entitySheep,
	"zombie": entityZombie, "skeleton": entitySkeleton, "spider": entitySpider,
	"creeper": entityCreeper, "husk": entityHusk, "stray": entityStray,
	"drowned": entityDrowned, "slime": entitySlime, "enderman": entityEnderman,
	"witch": entityWitch, "ender_dragon": entityEnderDragon,
}

func (s *Server) cmdGive(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission to give items.")
		return
	}
	if len(args) < 2 {
		p.tell("Usage: /give <player> <item> [count]")
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
	s.hub.post(evGive{target: args[0], item: item, count: count})
	p.tell(fmt.Sprintf("Gave %d × %s to %s", count, args[1], args[0]))
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
	s.hub.post(evKill{target: target})
}

func (s *Server) cmdXP(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) < 3 || args[0] != "add" {
		p.tell("Usage: /xp add <player> <levels>")
		return
	}
	n, err := strconv.Atoi(args[2])
	if err != nil {
		p.tell("Usage: /xp add <player> <levels>")
		return
	}
	s.hub.post(evXPLevels{target: args[1], levels: n})
	p.tell(fmt.Sprintf("Gave %d levels to %s", n, args[1]))
}

func (s *Server) cmdSummon(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) < 1 {
		p.tell("Usage: /summon <mob> [x z]")
		return
	}
	et, ok := summonable[strings.TrimPrefix(args[0], "minecraft:")]
	if !ok {
		p.tell("Unknown mob: " + args[0])
		return
	}
	x, z := int(p.x), int(p.z)+2
	if len(args) >= 3 {
		if xx, err := strconv.Atoi(args[1]); err == nil {
			x = xx
		}
		if zz, err := strconv.Atoi(args[2]); err == nil {
			z = zz
		}
	}
	s.hub.post(evSummon{etype: et, x: x, z: z, dim: p.dim, y: p.y})
	p.tell("Summoned " + args[0])
}

func (s *Server) cmdDifficulty(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) != 1 {
		p.tell("Usage: /difficulty <peaceful|easy|normal|hard>")
		return
	}
	d := map[string]int{"peaceful": diffPeaceful, "easy": diffEasy, "normal": diffNormal, "hard": diffHard}
	v, ok := d[args[0]]
	if !ok {
		p.tell("Usage: /difficulty <peaceful|easy|normal|hard>")
		return
	}
	s.hub.post(evSetRule{rule: "difficulty", num: v})
	p.tell("Difficulty set to " + args[0])
}

func (s *Server) cmdGamerule(p *player, args []string) {
	if !s.isOp(p.name) {
		p.tell("You don't have permission.")
		return
	}
	if len(args) != 2 {
		p.tell("Gamerules: keepInventory doDaylightCycle doMobSpawning mobGriefing doWeatherCycle doFireTick doTileDrops doMobLoot naturalRegeneration fallDamage drowningDamage fireDamage announceAdvancements showDeathMessages doImmediateRespawn randomTickSpeed playersSleepingPercentage")
		return
	}
	switch args[0] {
	case "keepInventory", "doDaylightCycle", "doMobSpawning", "mobGriefing", "doWeatherCycle",
		"doFireTick", "doTileDrops", "doMobLoot", "naturalRegeneration", "fallDamage",
		"drowningDamage", "fireDamage", "announceAdvancements", "showDeathMessages",
		"doImmediateRespawn":
		if args[1] != "true" && args[1] != "false" {
			p.tell("/gamerule " + args[0] + " <true|false>")
			return
		}
		s.hub.post(evSetRule{rule: args[0], on: args[1] == "true"})
	case "randomTickSpeed", "playersSleepingPercentage":
		n, err := strconv.Atoi(args[1])
		if err != nil || n < 0 || n > 1000 {
			p.tell("/gamerule " + args[0] + " <0-1000>")
			return
		}
		s.hub.post(evSetRule{rule: args[0], num: n})
	default:
		p.tell("Unknown gamerule: " + args[0])
		return
	}
	p.tell(fmt.Sprintf("Gamerule %s = %s", args[0], args[1]))
}

type evGive struct {
	target string
	item   int32
	count  int
}
type evKill struct{ target string }
type evXPLevels struct {
	target string
	levels int
}
type evSummon struct {
	etype int
	x, z  int
	dim   int
	y     float64
}
type evSetRule struct {
	rule string
	on   bool
	num  int
}

func (evGive) isHubEvent()     {}
func (evKill) isHubEvent()     {}
func (evXPLevels) isHubEvent() {}
func (evSummon) isHubEvent()   {}
func (evSetRule) isHubEvent()  {}

// applyRule mutates the hub's world rules (and persists them).
func (h *hub) applyRule(players map[int32]*tracked, e evSetRule) {
	switch e.rule {
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
	case "keepInventory":
		h.rules.KeepInventory = e.on
	case "doDaylightCycle":
		h.rules.DoDaylight = e.on
	case "doMobSpawning":
		h.rules.DoMobSpawning = e.on
	case "mobGriefing":
		h.rules.MobGriefing = e.on
	case "doWeatherCycle":
		h.rules.DoWeather = e.on
	case "doFireTick":
		h.rules.DoFireTick = e.on
	case "doTileDrops":
		h.rules.DoTileDrops = e.on
	case "doMobLoot":
		h.rules.DoMobLoot = e.on
	case "naturalRegeneration":
		h.rules.NaturalRegen = e.on
	case "fallDamage":
		h.rules.FallDamage = e.on
	case "drowningDamage":
		h.rules.DrownDamage = e.on
	case "fireDamage":
		h.rules.FireDamage = e.on
	case "announceAdvancements":
		h.rules.AnnounceAdv = e.on
	case "showDeathMessages":
		h.rules.ShowDeathMsgs = e.on
	case "doImmediateRespawn":
		h.rules.ImmediateResp = e.on
	case "randomTickSpeed":
		h.rules.RandomTicks = e.num
	case "playersSleepingPercentage":
		h.rules.SleepPercent = e.num
	}
	h.saveRules()
	h.plugins.Fire(&plugin.GameruleChangeEvent{Rule: e.rule, On: e.on, Num: e.num})
}

// loadRules / saveRules persist the world settings as plain JSON.
func (h *hub) loadRules() {
	if h.rulesPath == "" {
		return
	}
	if data, err := os.ReadFile(h.rulesPath); err == nil {
		json.Unmarshal(data, &h.rules)
	}
	h.difficultyPub.Store(int32(h.rules.Difficulty))
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

func (h *hub) saveRules() {
	if h.rulesPath == "" {
		return
	}
	h.rules.Weather = &weatherSave{
		ClearTime: h.clearTime, RainTime: h.rainTime, ThunderTime: h.thunderTime,
		Raining: h.rainFlag, Thundering: h.thunderFlag,
	}
	data, _ := json.MarshalIndent(h.rules, "", "  ")
	os.WriteFile(h.rulesPath, data, 0o644)
}
