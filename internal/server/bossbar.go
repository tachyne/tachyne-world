package server

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// /bossbar (BossBarCommands) and the custom boss bars it manages
// (CustomBossEvents): named bars an operator creates, styles, fills and
// shows to chosen players. They are saved with the world's settings, as
// vanilla keeps them in the level data, and a listed player who comes back
// sees theirs again.
//
// A new bar is white, a solid progress bar, value 0 of 100, visible, with
// nobody on it. Changing a bar's name, colour or style updates it in place
// for its viewers, as vanilla's UPDATE_NAME / UPDATE_STYLE operations do.

// customBossbar is one bar, in its saved form.
type customBossbar struct {
	Name     string          `json:"name"`
	NameJSON json.RawMessage `json:"name_json,omitempty"` // the name as the component it was given
	Visible  bool            `json:"visible"`
	Value    int             `json:"value"`
	Max      int             `json:"max"`
	Color    int32           `json:"color"`
	Overlay  int32           `json:"overlay"`
	Flags    uint8           `json:"flags,omitempty"`
	Players  []string        `json:"players,omitempty"` // UUIDs, online or not
}

var bossColours = []string{"pink", "blue", "red", "green", "yellow", "purple", "white"}
var bossStyles = []string{"progress", "notched_6", "notched_10", "notched_12", "notched_20"}

// bossColourCode is BossBarColor.getFormatting as a legacy code.
var bossColourCode = []string{"§d", "§9", "§c", "§a", "§e", "§5", "§f"}

func (b *customBossbar) progress() float32 {
	if b.Max <= 0 {
		return 0
	}
	f := float32(b.Value) / float32(b.Max)
	return min(max(f, 0), 1)
}

func (b *customBossbar) has(uuid string) bool {
	for _, u := range b.Players {
		if u == uuid {
			return true
		}
	}
	return false
}

// displayName is CustomBossEvent.getDisplayName: the name in square
// brackets, in the bar's colour.
func (b *customBossbar) displayName() string {
	cc := bossColourCode[min(max(int(b.Color), 0), len(bossColourCode)-1)]
	return cc + "[" + b.Name + "§r" + cc + "]§r"
}

// bossbarUUID is the bar's id on the wire, stable per bar id.
func bossbarUUID(id string) [16]byte {
	u := md5.Sum([]byte("tachyne:bossbar:" + id))
	u[6] = u[6]&0x0f | 0x30
	u[8] = u[8]&0x3f | 0x80
	return u
}

// normBossbarID is IdentifierArgument: a bare path takes the minecraft
// namespace.
func normBossbarID(id string) (string, bool) {
	ns, path, found := strings.Cut(id, ":")
	if !found {
		ns, path = "minecraft", id
	}
	if ns == "" || path == "" {
		return "", false
	}
	for _, r := range ns + path {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.' || r == '/') {
			return "", false
		}
	}
	if strings.ContainsRune(ns, '/') {
		return "", false
	}
	return ns + ":" + path, true
}

func (b *customBossbar) addFrame(id string) attachproto.BossBar {
	ev := bossBarAdd(bossbarUUID(id), b.Name, b.progress(), bossLook{b.Color, b.Overlay, b.Flags})
	ev.TitleJSON = b.NameJSON
	return ev
}

// viewers are the online players on the bar.
func (h *hub) bossbarViewers(players map[int32]*tracked, b *customBossbar) []*tracked {
	var out []*tracked
	for _, t := range players {
		if b.has(t.p.key()) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].p.name < out[j].p.name })
	return out
}

// bossbarsOnJoin shows a joining player the visible bars they are on
// (CustomBossEvent.onPlayerConnect).
func (h *hub) bossbarsOnJoin(t *tracked) {
	for id, b := range h.rules.Bossbars {
		if b.Visible && b.has(t.p.key()) {
			t.p.trySendEv(b.addFrame(id))
		}
	}
}

// bossbarUpdate sends a visible bar's in-place change to its viewers:
// ServerBossEvent.setName → UPDATE_NAME, setColor/setOverlay → UPDATE_STYLE.
func (h *hub) bossbarUpdate(players map[int32]*tracked, id string, b *customBossbar, op int32) {
	if !b.Visible {
		return
	}
	ev := attachproto.BossBar{UUID: bossbarUUID(id), Op: op, Title: b.Name, TitleJSON: b.NameJSON, Color: b.Color, Overlay: b.Overlay, Flags: b.Flags}
	for _, t := range h.bossbarViewers(players, b) {
		t.p.trySendEv(ev)
	}
}

const bossbarUsage = "Usage: /bossbar add <id> <name> | remove <id> | list | get <id> value|max|visible|players | set <id> name|color|style|value|max|visible|players …"

func (s *Server) cmdBossbar(p *player, args []string) {
	if !s.isOp(p.name) { // BossBarCommands: LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	s.onHub(func(players map[int32]*tracked) {
		t := players[p.eid]
		if t == nil {
			return
		}
		if msg := s.hub.bossbarCommand(players, t, args); msg != "" {
			cmdFail(p, msg)
		}
	})
}

// bossbarCommand runs one /bossbar on the hub; a non-empty string is the
// failure line.
func (h *hub) bossbarCommand(players map[int32]*tracked, t *tracked, args []string) string {
	ok := func(msg string) { h.cmdSuccess(players, t.p, msg, true) }
	if len(args) == 0 {
		return bossbarUsage
	}
	if args[0] == "list" {
		if len(args) != 1 {
			return bossbarUsage
		}
		if len(h.rules.Bossbars) == 0 {
			h.cmdSuccess(players, t.p, "There are no custom bossbars active", false)
			return ""
		}
		ids := make([]string, 0, len(h.rules.Bossbars))
		for id := range h.rules.Bossbars {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		names := make([]string, len(ids))
		for i, id := range ids {
			names[i] = h.rules.Bossbars[id].displayName()
		}
		h.cmdSuccess(players, t.p, fmt.Sprintf("There are %d custom bossbar(s) active: %s", len(ids), strings.Join(names, ", ")), false)
		return ""
	}
	if len(args) < 2 {
		return bossbarUsage
	}
	id, valid := normBossbarID(args[1])
	if !valid {
		return "Invalid ID: " + args[1]
	}
	if args[0] == "add" {
		if len(args) < 3 {
			return bossbarUsage
		}
		if h.rules.Bossbars[id] != nil {
			return fmt.Sprintf("A bossbar already exists with the ID '%s'", id)
		}
		name, why := componentArg(args[2:])
		if why != "" {
			return why
		}
		b := &customBossbar{Name: name, Visible: true, Max: 100, Color: attachproto.BossWhite, Overlay: attachproto.BossProgress}
		b.NameJSON, _ = componentJSON(strings.Join(args[2:], " "))
		if h.rules.Bossbars == nil {
			h.rules.Bossbars = map[string]*customBossbar{}
		}
		h.rules.Bossbars[id] = b
		h.saveRules()
		ok("Created custom bossbar " + b.displayName())
		return ""
	}
	b := h.rules.Bossbars[id]
	if b == nil {
		return fmt.Sprintf("No bossbar exists with the ID '%s'", id)
	}
	switch args[0] {
	case "remove":
		if len(args) != 2 {
			return bossbarUsage
		}
		if b.Visible {
			for _, v := range h.bossbarViewers(players, b) {
				v.p.trySendEv(bossBarRemove(bossbarUUID(id)))
			}
		}
		delete(h.rules.Bossbars, id)
		h.saveRules()
		ok("Removed custom bossbar " + b.displayName())
	case "get":
		if len(args) != 3 {
			return bossbarUsage
		}
		switch args[2] {
		case "value":
			ok(fmt.Sprintf("Custom bossbar %s has a value of %d", b.displayName(), b.Value))
		case "max":
			ok(fmt.Sprintf("Custom bossbar %s has a maximum of %d", b.displayName(), b.Max))
		case "visible":
			if b.Visible {
				ok(fmt.Sprintf("Custom bossbar %s is currently visible", b.displayName()))
			} else {
				ok(fmt.Sprintf("Custom bossbar %s is currently hidden", b.displayName()))
			}
		case "players":
			vs := h.bossbarViewers(players, b)
			if len(vs) == 0 {
				ok(fmt.Sprintf("Custom bossbar %s has no players currently online", b.displayName()))
				break
			}
			names := make([]string, len(vs))
			for i, v := range vs {
				names[i] = v.p.name
			}
			ok(fmt.Sprintf("Custom bossbar %s has %d player(s) currently online: %s", b.displayName(), len(vs), strings.Join(names, ", ")))
		default:
			return bossbarUsage
		}
	case "set":
		if len(args) < 3 {
			return bossbarUsage
		}
		return h.bossbarSet(players, t, id, b, args[2], args[3:])
	default:
		return bossbarUsage
	}
	return ""
}

// componentArg reads a name argument (the rest of the line).
func componentArg(args []string) (string, string) {
	name, why, isComp := parseTextComponent(strings.Join(args, " "))
	if !isComp {
		return "", "Invalid chat component: " + strings.Join(args, " ")
	}
	return name, why
}

func (h *hub) bossbarSet(players map[int32]*tracked, t *tracked, id string, b *customBossbar, what string, rest []string) string {
	ok := func(msg string) {
		h.saveRules()
		h.cmdSuccess(players, t.p, msg, true)
	}
	one := func() (string, bool) {
		if len(rest) != 1 {
			return "", false
		}
		return rest[0], true
	}
	switch what {
	case "name":
		if len(rest) == 0 {
			return bossbarUsage
		}
		name, why := componentArg(rest)
		if why != "" {
			return why
		}
		if name == b.Name {
			return "Nothing changed. That's already the name of this bossbar"
		}
		b.Name = name
		b.NameJSON, _ = componentJSON(strings.Join(rest, " "))
		h.bossbarUpdate(players, id, b, attachproto.BossBarTitle)
		ok(fmt.Sprintf("Custom bossbar %s has been renamed", b.displayName()))
	case "color":
		v, has := one()
		i := indexOf(bossColours, v)
		if !has || i < 0 {
			return bossbarUsage
		}
		if int32(i) == b.Color {
			return "Nothing changed. That's already the color of this bossbar"
		}
		b.Color = int32(i)
		h.bossbarUpdate(players, id, b, attachproto.BossBarStyle)
		ok(fmt.Sprintf("Custom bossbar %s has changed color", b.displayName()))
	case "style":
		v, has := one()
		i := indexOf(bossStyles, v)
		if !has || i < 0 {
			return bossbarUsage
		}
		if int32(i) == b.Overlay {
			return "Nothing changed. That's already the style of this bossbar"
		}
		b.Overlay = int32(i)
		h.bossbarUpdate(players, id, b, attachproto.BossBarStyle)
		ok(fmt.Sprintf("Custom bossbar %s has changed style", b.displayName()))
	case "value", "max":
		v, has := one()
		n, err := strconv.Atoi(v)
		if !has || err != nil {
			return bossbarUsage
		}
		if what == "value" {
			if n < 0 {
				return fmt.Sprintf("Integer must not be less than 0, found %d", n)
			}
			if n == b.Value {
				return "Nothing changed. That's already the value of this bossbar"
			}
			b.Value = n
		} else {
			if n < 1 {
				return fmt.Sprintf("Integer must not be less than 1, found %d", n)
			}
			if n == b.Max {
				return "Nothing changed. That's already the maximum of this bossbar"
			}
			b.Max = n
		}
		if b.Visible {
			for _, v := range h.bossbarViewers(players, b) {
				v.p.trySendEv(bossBarHealth(bossbarUUID(id), b.progress()))
			}
		}
		if what == "value" {
			ok(fmt.Sprintf("Custom bossbar %s has changed value to %d", b.displayName(), n))
		} else {
			ok(fmt.Sprintf("Custom bossbar %s has changed maximum to %d", b.displayName(), n))
		}
	case "visible":
		v, has := one()
		if !has || (v != "true" && v != "false") {
			return bossbarUsage
		}
		vis := v == "true"
		if vis == b.Visible {
			if vis {
				return "Nothing changed. The bossbar is already visible"
			}
			return "Nothing changed. The bossbar is already hidden"
		}
		b.Visible = vis
		for _, t := range h.bossbarViewers(players, b) {
			if vis {
				t.p.trySendEv(b.addFrame(id))
			} else {
				t.p.trySendEv(bossBarRemove(bossbarUUID(id)))
			}
		}
		if vis {
			ok(fmt.Sprintf("Custom bossbar %s is now visible", b.displayName()))
		} else {
			ok(fmt.Sprintf("Custom bossbar %s is now hidden", b.displayName()))
		}
	case "players":
		// No targets empties the bar; a selector that matches nobody does
		// too (getOptionalPlayers).
		var targets []*tracked
		if len(rest) > 1 {
			return bossbarUsage
		}
		if len(rest) == 1 {
			targets = h.commandTargets(players, t.p.eid, rest[0])
		}
		want := map[string]*tracked{}
		for _, x := range targets {
			want[x.p.key()] = x
		}
		changed := false
		kept := b.Players[:0:0]
		for _, u := range b.Players {
			if want[u] != nil {
				kept = append(kept, u)
				continue
			}
			changed = true
			if b.Visible {
				for _, x := range players {
					if x.p.key() == u {
						x.p.trySendEv(bossBarRemove(bossbarUUID(id)))
					}
				}
			}
		}
		for u, x := range want {
			if !b.has(u) {
				changed = true
				kept = append(kept, u)
				if b.Visible {
					x.p.trySendEv(b.addFrame(id))
				}
			}
		}
		if !changed {
			return "Nothing changed. Those players are already on the bossbar with nobody to add or remove"
		}
		sort.Strings(kept)
		b.Players = kept
		if len(b.Players) == 0 {
			ok(fmt.Sprintf("Custom bossbar %s no longer has any players", b.displayName()))
			break
		}
		names := make([]string, 0, len(targets))
		for _, x := range targets {
			names = append(names, x.p.name)
		}
		sort.Strings(names)
		ok(fmt.Sprintf("Custom bossbar %s now has %d player(s): %s", b.displayName(), len(targets), strings.Join(names, ", ")))
	default:
		return bossbarUsage
	}
	return ""
}

func indexOf(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return -1
}
