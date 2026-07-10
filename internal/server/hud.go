package server

import (
	"fmt"
	"strings"
)

// The HUD is a heads-up display pushed to the player's action bar (the line above
// the hotbar) — no client mod needed, just a System Chat message flagged as an
// overlay, refreshed a few times a second.
//
// It's deliberately modular: a HudWidget renders one independent segment, and the
// bar is just a composed list of widgets. Adding a field to the HUD = writing one
// widget and adding it to the list; widgets don't know about each other or the
// transport. The same widgets could feed a sidebar scoreboard or the tab list by
// swapping the renderer — the segments are display-agnostic.

// HudView is the read-only snapshot a widget renders from each refresh.
type HudView struct {
	Name     string
	X, Y, Z  float64
	Yaw      float32
	DayTime  uint64
	Online   int
	Gamemode int
	Biome    string // e.g. "minecraft:plains"
	Shard    int    // this pod's shard id; -1 = unsharded (widget hidden)
}

// HudWidget renders one HUD segment; return "" to show nothing this refresh.
type HudWidget interface {
	Render(v HudView) string
}

// HudWidgetFunc adapts a plain function to a HudWidget.
type HudWidgetFunc func(HudView) string

func (f HudWidgetFunc) Render(v HudView) string { return f(v) }

// defaultHud is the built-in set. To add a field, append a widget here (or build
// your own list and assign hub.hud) — that's the whole change.
func defaultHud() []HudWidget {
	return []HudWidget{
		HudWidgetFunc(func(v HudView) string {
			if v.Shard < 0 {
				return ""
			}
			return fmt.Sprintf("shard %d", v.Shard)
		}),
		HudWidgetFunc(func(v HudView) string { return clockString(v.DayTime) }),
		HudWidgetFunc(func(v HudView) string { return fmt.Sprintf("XYZ %.0f %.0f %.0f", v.X, v.Y, v.Z) }),
		HudWidgetFunc(func(v HudView) string { return "facing " + facingShort(v.Yaw) }),
		HudWidgetFunc(func(v HudView) string { return prettyBiome(v.Biome) }),
		HudWidgetFunc(func(v HudView) string { return gamemodeName(v.Gamemode) }),
		HudWidgetFunc(func(v HudView) string { return fmt.Sprintf("%d online", v.Online) }),
	}
}

// prettyBiome turns "minecraft:snowy_plains" into "Snowy Plains".
func prettyBiome(id string) string {
	name := strings.TrimPrefix(id, "minecraft:")
	words := strings.Split(name, "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

func gamemodeName(mode int) string {
	switch mode {
	case gmCreative:
		return "Creative"
	case gmAdventure:
		return "Adventure"
	case gmSpectator:
		return "Spectator"
	default:
		return "Survival"
	}
}

// renderHud composes the enabled widgets into one action-bar line.
func renderHud(widgets []HudWidget, v HudView) string {
	parts := make([]string, 0, len(widgets))
	for _, w := range widgets {
		if s := w.Render(v); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "   ")
}

// clockString formats day-time ticks as HH:MM (0 ticks = 06:00).
func clockString(dayTime uint64) string {
	t := dayTime % dayLengthTicks
	h := (t/1000 + 6) % 24
	m := (t % 1000) * 60 / 1000
	return fmt.Sprintf("%02d:%02d", h, m)
}

// facingShort maps a yaw to a compass letter.
func facingShort(yaw float32) string {
	switch playerFacing(yaw) {
	case "north":
		return "N"
	case "south":
		return "S"
	case "east":
		return "E"
	default:
		return "W"
	}
}
