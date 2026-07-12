package server

import (
	"encoding/json"
	"fmt"
	"time"

	"tachyne/plugin"
)

// Bus protocol v2: the plugin event catalog, published as JSON.
//
// v1 subjects (mc.event.chat, mc.event.player_move, …) publish hand-rolled
// ad-hoc maps and stay for compatibility. v2 mirrors every in-process plugin
// event onto "mc.event.v2.<EventName()>" with the event struct itself as the
// payload — the same names, fields and semantics docs/PLUGINS.md documents,
// so an external observer and an in-process plugin read the same vocabulary.
// Cancelled events are NOT published (the action didn't happen). Observe
// only: the bus deliberately has no synchronous veto hooks — anything that
// must cancel or rewrite an action belongs in-process.

// registerBusBridge subscribes the v2 mirror for every event. Called only
// when a real bus backend is configured, so idle installs pay nothing.
func (h *hub) registerBusBridge() {
	bridgeEv[*plugin.PlayerJoinEvent](h)
	bridgeEv[*plugin.PlayerQuitEvent](h)
	bridgeEv[*plugin.PlayerChatEvent](h)
	bridgeEv[*plugin.PlayerCommandEvent](h)
	bridgeEv[*plugin.PlayerMoveEvent](h)
	bridgeEv[*plugin.BlockBreakEvent](h)
	bridgeEv[*plugin.BlockPlaceEvent](h)
	bridgeEv[*plugin.EntityDamageByEntityEvent](h)
	bridgeEv[*plugin.MobSpawnEvent](h)
	bridgeEv[*plugin.MobDeathEvent](h)
	bridgeEv[*plugin.WeatherChangeEvent](h)
	bridgeEv[*plugin.ThunderChangeEvent](h)
	bridgeEv[*plugin.TimeSetEvent](h)
	bridgeEv[*plugin.GameruleChangeEvent](h)
}

// bridgeEv mirrors one event type at Monitor priority, skipping cancelled
// events (ignoreCancelled) — observers see final outcomes only.
func bridgeEv[T plugin.Event](h *hub) {
	plugin.On(h.plugins, plugin.Monitor, true, func(ev T) {
		h.bus.publish("v2."+ev.EventName(), ev)
	})
}

// runOnHub runs fn on the hub goroutine and waits — the bus's query path
// (NATS handlers run per-message goroutines, so blocking here is safe). The
// timeout keeps a wedged hub from leaking NATS goroutines forever.
func (h *hub) runOnHub(fn func()) bool {
	done := make(chan struct{})
	h.psched.schedule(1, 0, func() { fn(); close(done) })
	select {
	case <-done:
		return true
	case <-time.After(5 * time.Second):
		return false
	}
}

// --- v2 command handlers (dispatched from executeCommand) -------------------

func busCmdWeather(h *hub, args json.RawMessage) (any, string) {
	a := struct {
		Kind     string `json:"kind"`
		Duration int    `json:"duration"`
	}{Duration: -1}
	if json.Unmarshal(args, &a) != nil ||
		(a.Kind != "clear" && a.Kind != "rain" && a.Kind != "thunder") {
		return nil, "weather requires kind=clear|rain|thunder [,duration ticks]"
	}
	h.post(evSetWeather{kind: a.Kind, duration: a.Duration})
	return nil, ""
}

func busCmdGamerule(h *hub, args json.RawMessage) (any, string) {
	var a struct {
		Rule string `json:"rule"`
		On   bool   `json:"on"`
		Num  int    `json:"num"`
	}
	if json.Unmarshal(args, &a) != nil {
		return nil, "gamerule requires rule[,on][,num]"
	}
	switch a.Rule {
	case "keepInventory", "doDaylightCycle", "doMobSpawning", "mobGriefing", "doWeatherCycle", "difficulty":
	default:
		return nil, fmt.Sprintf("unknown gamerule %q", a.Rule)
	}
	h.post(evSetRule{rule: a.Rule, on: a.On, num: a.Num})
	return nil, ""
}

func busCmdGive(h *hub, args json.RawMessage) (any, string) {
	a := struct {
		Player string `json:"player"`
		Item   string `json:"item"` // canonical item name — ids drift, names don't
		Count  int    `json:"count"`
	}{Count: 1}
	if json.Unmarshal(args, &a) != nil || a.Player == "" || a.Item == "" {
		return nil, "give requires player,item[,count]"
	}
	id, ok := itemByName[a.Item]
	if !ok {
		return nil, fmt.Sprintf("unknown item %q", a.Item)
	}
	h.post(evGive{target: a.Player, item: id, count: max(1, a.Count)})
	return nil, ""
}

func busCmdTeleport(h *hub, args json.RawMessage) (any, string) {
	var full struct {
		Player string  `json:"player"`
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Z      float64 `json:"z"`
	}
	if json.Unmarshal(args, &full) != nil || full.Player == "" {
		return nil, "teleport requires player,x,y,z"
	}
	found := false
	ok := h.runOnHub(func() {
		for _, t := range h.playersRef {
			if t.p.name == full.Player {
				h.teleportPlayer(h.playersRef, t, full.X, full.Y, full.Z)
				found = true
			}
		}
	})
	if !ok {
		return nil, "hub busy"
	}
	if !found {
		return nil, fmt.Sprintf("player %q not online", full.Player)
	}
	return nil, ""
}

func busCmdSpawn2(h *hub, args json.RawMessage) (any, string) {
	a := struct {
		Type      string  `json:"type"` // entity name, e.g. "zombie"
		Dim       int     `json:"dim"`
		X         float64 `json:"x"`
		Y         float64 `json:"y"` // 0 = snap to the surface
		Z         float64 `json:"z"`
		Behavior  string  `json:"behavior"`
		MaxHealth int     `json:"max_health"`
		Speed     float64 `json:"speed"`
		Damage    float64 `json:"damage"`
	}{}
	if json.Unmarshal(args, &a) != nil || a.Type == "" {
		return nil, "spawn2 requires type (entity name), x, z [,y,dim,behavior,max_health,speed,damage]"
	}
	etype, ok := entityByName[a.Type]
	if !ok {
		return nil, fmt.Sprintf("unknown entity %q", a.Type)
	}
	if a.Behavior != "" {
		if _, ok := behaviors[a.Behavior]; !ok {
			return nil, fmt.Sprintf("unknown behavior %q", a.Behavior)
		}
	}
	var eid int32
	errStr := ""
	done := h.runOnHub(func() {
		y := a.Y
		if y == 0 {
			y = float64(h.worldFor(a.Dim).SurfaceFeet(int(a.X), int(a.Z)))
		}
		m := h.spawnMobCause(h.playersRef, etype, a.Dim, a.X, y, a.Z, plugin.SpawnBus)
		if m == nil {
			errStr = "spawn cancelled by a plugin"
			return
		}
		h.applySpecies(h.playersRef, m)
		if a.Behavior != "" {
			h.applyBehavior(m, a.Behavior)
		}
		if a.MaxHealth > 0 {
			m.maxHealth, m.health = a.MaxHealth, a.MaxHealth
		}
		if a.Speed > 0 {
			m.ovrSpeed, m.speed = a.Speed, a.Speed
		}
		if a.Damage > 0 {
			m.ovrDamage = a.Damage
		}
		eid = m.eid
	})
	if !done {
		return nil, "hub busy"
	}
	if errStr != "" {
		return nil, errStr
	}
	return map[string]any{"eid": eid}, ""
}

func busCmdMobSet(h *hub, args json.RawMessage) (any, string) {
	var a struct {
		EID       int32   `json:"eid"`
		MaxHealth int     `json:"max_health"`
		Health    int     `json:"health"`
		Speed     float64 `json:"speed"`
		Damage    float64 `json:"damage"`
		Behavior  string  `json:"behavior"`
	}
	if json.Unmarshal(args, &a) != nil || a.EID == 0 {
		return nil, "mobset requires eid [,max_health,health,speed,damage,behavior]"
	}
	errStr := ""
	done := h.runOnHub(func() {
		m := h.mobs[a.EID]
		if m == nil {
			errStr = fmt.Sprintf("no mob with eid %d", a.EID)
			return
		}
		if a.MaxHealth > 0 {
			m.maxHealth = a.MaxHealth
			if m.health > a.MaxHealth {
				m.health = a.MaxHealth
			}
		}
		if a.Health > 0 {
			m.health = min(a.Health, m.maxHealth)
		}
		if a.Speed > 0 {
			m.ovrSpeed, m.speed = a.Speed, a.Speed
		}
		if a.Damage > 0 {
			m.ovrDamage = a.Damage
		}
		if a.Behavior != "" && !h.applyBehavior(m, a.Behavior) {
			errStr = fmt.Sprintf("unknown behavior %q", a.Behavior)
		}
	})
	if !done {
		return nil, "hub busy"
	}
	return nil, errStr
}

// --- v2 queries (request-reply) ---------------------------------------------

func busQueryPlayers(h *hub) (any, string) {
	type prow struct {
		EID      int32   `json:"eid"`
		Name     string  `json:"name"`
		X        float64 `json:"x"`
		Y        float64 `json:"y"`
		Z        float64 `json:"z"`
		Dim      int     `json:"dim"`
		Gamemode int     `json:"gamemode"`
		Health   float32 `json:"health"`
	}
	var out []prow
	if !h.runOnHub(func() {
		for eid, t := range h.playersRef {
			out = append(out, prow{EID: eid, Name: t.p.name, X: t.x, Y: t.y, Z: t.z,
				Dim: t.dim, Gamemode: t.gamemode, Health: t.health})
		}
	}) {
		return nil, "hub busy"
	}
	return map[string]any{"players": out}, ""
}

func busQueryMobs(h *hub, args json.RawMessage) (any, string) {
	a := struct {
		Dim *int `json:"dim"`
	}{}
	json.Unmarshal(args, &a)
	type mrow struct {
		EID    int32   `json:"eid"`
		Type   string  `json:"type"`
		X      float64 `json:"x"`
		Y      float64 `json:"y"`
		Z      float64 `json:"z"`
		Dim    int     `json:"dim"`
		Health int     `json:"health"`
		Max    int     `json:"max_health"`
	}
	var out []mrow
	if !h.runOnHub(func() {
		for eid, m := range h.mobs {
			if a.Dim != nil && m.dim != *a.Dim {
				continue
			}
			out = append(out, mrow{EID: eid, Type: entityNameByID[m.etype],
				X: m.x, Y: m.y, Z: m.z, Dim: m.dim, Health: m.health, Max: m.maxHealth})
		}
	}) {
		return nil, "hub busy"
	}
	return map[string]any{"mobs": out}, ""
}

func busQueryBlock(h *hub, args json.RawMessage) (any, string) {
	var full struct {
		X   int `json:"x"`
		Y   int `json:"y"`
		Z   int `json:"z"`
		Dim int `json:"dim"`
	}
	if json.Unmarshal(args, &full) != nil {
		return nil, "block requires x,y,z[,dim]"
	}
	state := h.worldFor(full.Dim).At(full.X, full.Y, full.Z) // world has its own lock
	return map[string]any{"state": state}, ""
}

func busQueryWorld(h *hub) (any, string) {
	var out map[string]any
	if !h.runOnHub(func() {
		out = map[string]any{
			"age":        h.tick.Load(),
			"day_time":   h.dayTime.Load() % dayLengthTicks,
			"raining":    h.raining,
			"thundering": h.thundering,
			"difficulty": h.rules.Difficulty,
			"players":    len(h.playersRef),
			"mobs":       len(h.mobs),
		}
	}) {
		return nil, "hub busy"
	}
	return out, ""
}
