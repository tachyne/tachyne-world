package server

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// /waypoint (WaypointCommand):
//
//	/waypoint list
//	/waypoint modify <waypoint> color <team colour> | color hex <rrggbb> | color reset
//	/waypoint modify <waypoint> style reset | style set <style>
//
// Every living entity carries a locator-bar icon (Waypoint.Icon: a style
// asset and an optional colour); a player's is what the others' locator
// bars draw. Changing it re-tracks the waypoint (mutateIcon: untrack,
// change, track), so every receiver redraws it at once. A player keeps the
// icon in their data (LivingEntity's locator_bar_icon). All the feedback is
// sendSuccess(…, false): its caller only.

// waypointIcon is Waypoint.Icon.
type waypointIcon struct {
	style    string // the waypoint_style asset; "" = minecraft:default
	color    int32  // RGB, when hasColor
	hasColor bool
}

// savedWaypointIcon is the icon in a player's saved data.
type savedWaypointIcon struct {
	Style string `json:"style,omitempty"`
	Color *int32 `json:"color,omitempty"`
}

func (ic waypointIcon) save() *savedWaypointIcon {
	if ic.style == "" && !ic.hasColor {
		return nil
	}
	s := &savedWaypointIcon{Style: ic.style}
	if ic.hasColor {
		c := ic.color
		s.Color = &c
	}
	return s
}

func (s *savedWaypointIcon) load() waypointIcon {
	if s == nil {
		return waypointIcon{}
	}
	ic := waypointIcon{style: s.Style}
	if s.Color != nil {
		ic.color, ic.hasColor = *s.Color, true
	}
	return ic
}

// teamColors are TeamColor's sixteen, in order, with their text colours.
var teamColors = []struct {
	name string
	rgb  int32
}{
	{"black", 0x000000}, {"dark_blue", 0x0000AA}, {"dark_green", 0x00AA00}, {"dark_aqua", 0x00AAAA},
	{"dark_red", 0xAA0000}, {"dark_purple", 0xAA00AA}, {"gold", 0xFFAA00}, {"gray", 0xAAAAAA},
	{"dark_gray", 0x555555}, {"blue", 0x5555FF}, {"green", 0x55FF55}, {"aqua", 0x55FFFF},
	{"red", 0xFF5555}, {"light_purple", 0xFF55FF}, {"yellow", 0xFFFF55}, {"white", 0xFFFFFF},
}

func teamColorNames() []string {
	out := make([]string, len(teamColors))
	for i, c := range teamColors {
		out[i] = c.name
	}
	return out
}

// parseHexColor is HexColorArgument: three hex digits (each doubled) or six.
func parseHexColor(s string) (int32, bool) {
	switch len(s) {
	case 3:
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	case 6:
	default:
		return 0, false
	}
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	return int32(v), true
}

func (s *Server) cmdWaypoint(p *player, args []string) {
	if !s.isOp(p.name) { // WaypointCommand: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	s.onHub(func(players map[int32]*tracked) { s.hub.runWaypoint(players, p, args) })
}

const waypointUsage = "Usage: /waypoint list | /waypoint modify <waypoint> color <color>|hex <color>|reset | " +
	"/waypoint modify <waypoint> style reset|set <style>"

// runWaypoint is WaypointCommand on the hub.
func (h *hub) runWaypoint(players map[int32]*tracked, p *player, args []string) {
	fail := func(msg string) { cmdFail(p, msg) }
	info := func(msg string) { h.cmdSuccess(players, p, msg, false) }
	if len(args) == 0 {
		fail(waypointUsage)
		return
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			fail(waypointUsage)
			return
		}
		dim := dimOverworld
		if me := players[p.eid]; me != nil {
			dim = me.dim
		}
		// The level's waypoint manager holds the transmitting players.
		var names []string
		for _, t := range players {
			if t.dim == dim && waypointTransmits(t) {
				names = append(names, t.p.name)
			}
		}
		sort.Strings(names)
		key := dimType(dim).Key
		if len(names) == 0 {
			info(fmt.Sprintf("No waypoints in %s", key))
			return
		}
		info(fmt.Sprintf("%d waypoint(s) in %s: %s", len(names), key, strings.Join(names, ", ")))
	case "modify":
		if len(args) < 4 {
			fail(waypointUsage)
			return
		}
		ents := h.commandEntities(players, p.eid, args[1])
		switch {
		case len(ents) == 0:
			fail("No entity was found")
			return
		case len(ents) > 1:
			fail("Only one entity is allowed, but the provided selector allows more than one")
			return
		}
		e := ents[0]
		icon := e.living().wpIcon
		var msg string
		switch op, rest := args[2], args[3:]; {
		case op == "color" && len(rest) == 1 && rest[0] == "reset":
			icon.color, icon.hasColor = 0, false
			msg = "Reset waypoint color"
		case op == "color" && len(rest) == 2 && rest[0] == "hex":
			c, ok := parseHexColor(rest[1])
			if !ok {
				fail(fmt.Sprintf("Invalid hex color code '%s'", rest[1]))
				return
			}
			icon.color, icon.hasColor = c, true
			msg = fmt.Sprintf("Waypoint color is now %06X", c)
		case op == "color" && len(rest) == 1:
			found := false
			for _, tc := range teamColors {
				if tc.name == rest[0] {
					icon.color, icon.hasColor, found = tc.rgb, true, true
				}
			}
			if !found {
				fail(fmt.Sprintf("Unknown color '%s'", rest[0]))
				return
			}
			msg = "Waypoint color is now " + rest[0]
		case op == "style" && len(rest) == 1 && rest[0] == "reset":
			icon.style = "" // WaypointStyleAssets.DEFAULT
			msg = "Waypoint style changed"
		case op == "style" && len(rest) == 2 && rest[0] == "set":
			id, ok := parseResourceID(rest[1])
			if !ok {
				fail("Invalid ID")
				return
			}
			icon.style = id
			if id == "minecraft:default" {
				icon.style = ""
			}
			msg = "Waypoint style changed"
		default:
			fail(waypointUsage)
			return
		}
		e.living().wpIcon = icon
		if e.t != nil {
			h.waypointRestyle(players, e.t)
		}
		if e.m != nil {
			h.mobWaypointRestyle(players, e.m)
		}
		info(msg)
	default:
		fail(waypointUsage)
	}
}

// waypointRestyle is mutateIcon's untrack and track: every receiver holding
// the transmitter drops it and takes it back with the new icon.
func (h *hub) waypointRestyle(players map[int32]*tracked, t *tracked) {
	gone := attachproto.Waypoint{Op: waypointUntrack, UUID: t.p.uuid}
	for _, o := range players {
		if _, ok := o.wpTracked[t.p.eid]; ok {
			o.p.trySendEv(gone)
			o.p.trySendEv(waypointFor(t, waypointTrack))
		}
	}
}

// mobWaypointRestyle is waypointRestyle for a transmitting mob.
func (h *hub) mobWaypointRestyle(players map[int32]*tracked, m *mob) {
	gone := attachproto.Waypoint{Op: waypointUntrack, UUID: m.uuid}
	for _, o := range players {
		if _, ok := o.wpTracked[m.eid]; ok {
			o.p.trySendEv(gone)
			o.p.trySendEv(mobWaypointFor(m))
		}
	}
}
