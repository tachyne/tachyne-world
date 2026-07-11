package server

// The scoreboard: objectives, display slots, scores, and teams — the vanilla
// ServerScoreboard model, engine-owned. State is world-level (scoreboard.json),
// mutated by the op commands /scoreboard and /team and by the automatic
// criteria (deaths, totalKillCount, playerKillCount, health); every change
// broadcasts the matching domain frame and the whole board syncs to each
// player at join.

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

type sbObjective struct {
	Criteria string `json:"criteria"`
	Title    string `json:"title"`
	Hearts   bool   `json:"hearts,omitempty"`
}

type sbTeam struct {
	Title        string          `json:"title"`
	Prefix       string          `json:"prefix,omitempty"`
	Suffix       string          `json:"suffix,omitempty"`
	Color        int32           `json:"color"` // -1 = none
	FriendlyFire bool            `json:"ff,omitempty"`
	SeeInvisible bool            `json:"seeinvis,omitempty"`
	Visibility   int32           `json:"vis,omitempty"`
	Collision    int32           `json:"coll,omitempty"`
	Members      map[string]bool `json:"members,omitempty"`
}

type scoreboardState struct {
	Objectives map[string]*sbObjective     `json:"objectives"`
	Display    [3]string                   `json:"display"` // list, sidebar, below_name
	Scores     map[string]map[string]int32 `json:"scores"`  // owner → objective → value
	Teams      map[string]*sbTeam          `json:"teams"`
}

// sbStore persists the board (world-level, one file).
type sbStore struct {
	mu   sync.Mutex
	path string
}

func newScoreboard(path string) (*scoreboardState, *sbStore) {
	sb := &scoreboardState{
		Objectives: map[string]*sbObjective{},
		Scores:     map[string]map[string]int32{},
		Teams:      map[string]*sbTeam{},
	}
	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			json.Unmarshal(data, sb)
		}
	}
	return sb, &sbStore{path: path}
}

func (s *sbStore) flush(sb *scoreboardState) {
	if s == nil || s.path == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.MarshalIndent(sb, "", "  ")
	tmp := s.path + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil {
		os.Rename(tmp, s.path)
	}
}

// --- frame builders -----------------------------------------------------------

func (o *sbObjective) frame(name string, method int32) attachproto.Objective {
	return attachproto.Objective{Name: name, Method: method, Title: o.Title, Hearts: o.Hearts}
}

func (t *sbTeam) frame(name string, method int32, players []string) attachproto.Team {
	return attachproto.Team{Name: name, Method: method, Title: t.Title,
		Prefix: t.Prefix, Suffix: t.Suffix, Color: t.Color,
		FriendlyFire: t.FriendlyFire, SeeInvisible: t.SeeInvisible,
		Visibility: t.Visibility, Collision: t.Collision, Players: players}
}

// sbBroadcast sends a frame to every online player.
func (h *hub) sbBroadcast(players map[int32]*tracked, ev any) {
	for _, t := range players {
		t.p.trySendEv(ev)
	}
}

// sbSendAll syncs the whole board to one player (join).
func (h *hub) sbSendAll(t *tracked) {
	names := make([]string, 0, len(h.sb.Objectives))
	for name := range h.sb.Objectives {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.p.sendEv(h.sb.Objectives[name].frame(name, attachproto.ObjAdd))
	}
	for slot, obj := range h.sb.Display {
		if obj != "" {
			t.p.sendEv(attachproto.DisplaySlot{Slot: int32(slot), Objective: obj})
		}
	}
	owners := make([]string, 0, len(h.sb.Scores))
	for owner := range h.sb.Scores {
		owners = append(owners, owner)
	}
	sort.Strings(owners)
	for _, owner := range owners {
		for obj, v := range h.sb.Scores[owner] {
			t.p.sendEv(attachproto.Score{Owner: owner, Objective: obj, Value: v})
		}
	}
	teamNames := make([]string, 0, len(h.sb.Teams))
	for name := range h.sb.Teams {
		teamNames = append(teamNames, name)
	}
	sort.Strings(teamNames)
	for _, name := range teamNames {
		tm := h.sb.Teams[name]
		t.p.sendEv(tm.frame(name, attachproto.TeamAdd, sortedMembers(tm)))
	}
}

func sortedMembers(t *sbTeam) []string {
	out := make([]string, 0, len(t.Members))
	for m := range t.Members {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// sbSetScore sets one score and broadcasts it (no-op when unchanged, so
// per-second gauge criteria don't spam every client).
func (h *hub) sbSetScore(players map[int32]*tracked, owner, obj string, v int32) {
	m := h.sb.Scores[owner]
	if m == nil {
		m = map[string]int32{}
		h.sb.Scores[owner] = m
	}
	if old, ok := m[obj]; ok && old == v {
		return
	}
	m[obj] = v
	h.sbDirty = true
	h.sbBroadcast(players, attachproto.Score{Owner: owner, Objective: obj, Value: v})
}

// sbCriteria updates every objective tracking an automatic criteria: delta
// arithmetic for counters (deaths, kills), absolute for gauges (health).
func (h *hub) sbCriteria(players map[int32]*tracked, criteria, owner string, delta int32, absolute bool) {
	for name, o := range h.sb.Objectives {
		if o.Criteria != criteria {
			continue
		}
		v := delta
		if !absolute {
			v += h.sb.Scores[owner][name]
		}
		h.sbSetScore(players, owner, name, v)
	}
}

// --- /scoreboard and /team ------------------------------------------------------

type evScoreboardCmd struct {
	p    *player
	args []string
}

func (evScoreboardCmd) isHubEvent() {}

type evTeamCmd struct {
	p    *player
	args []string
}

func (evTeamCmd) isHubEvent() {}

var sbSlotNames = map[string]int32{
	"list": attachproto.SlotList, "sidebar": attachproto.SlotSidebar,
	"belowname": attachproto.SlotBelowName, "below_name": attachproto.SlotBelowName,
}

// sbValidCriteria is the accepted objective criteria set: dummy (command-set
// only) plus the automatic ones the engine feeds.
var sbValidCriteria = map[string]bool{
	"dummy": true, "deaths": true, "totalKillCount": true,
	"playerKillCount": true, "health": true,
}

func (h *hub) cmdScoreboard(players map[int32]*tracked, e evScoreboardCmd) {
	tell := func(msg string) { e.p.trySendEv(chatEv(msg)) }
	a := e.args
	switch {
	case len(a) >= 4 && a[0] == "objectives" && a[1] == "add":
		name, criteria := a[2], a[3]
		if !sbValidCriteria[criteria] {
			tell("Unknown criteria (dummy, deaths, totalKillCount, playerKillCount, health)")
			return
		}
		if _, ok := h.sb.Objectives[name]; ok {
			tell("An objective with that name already exists")
			return
		}
		title := name
		if len(a) > 4 {
			title = strings.Join(a[4:], " ")
		}
		o := &sbObjective{Criteria: criteria, Title: title, Hearts: criteria == "health"}
		h.sb.Objectives[name] = o
		h.sbBroadcast(players, o.frame(name, attachproto.ObjAdd))
		tell(fmt.Sprintf("Created objective [%s]", title))
	case len(a) == 3 && a[0] == "objectives" && a[1] == "remove":
		name := a[2]
		if _, ok := h.sb.Objectives[name]; !ok {
			tell("No such objective")
			return
		}
		delete(h.sb.Objectives, name)
		for slot, obj := range h.sb.Display {
			if obj == name {
				h.sb.Display[slot] = ""
			}
		}
		for _, scores := range h.sb.Scores {
			delete(scores, name)
		}
		h.sbBroadcast(players, attachproto.Objective{Name: name, Method: attachproto.ObjRemove})
		tell("Removed objective " + name)
	case len(a) >= 2 && a[0] == "objectives" && a[1] == "list":
		names := make([]string, 0, len(h.sb.Objectives))
		for n := range h.sb.Objectives {
			names = append(names, n)
		}
		sort.Strings(names)
		tell(fmt.Sprintf("Objectives (%d): %s", len(names), strings.Join(names, ", ")))
	case len(a) >= 3 && a[0] == "objectives" && a[1] == "setdisplay":
		slot, ok := sbSlotNames[a[2]]
		if !ok {
			tell("Usage: /scoreboard objectives setdisplay <list|sidebar|belowname> [objective]")
			return
		}
		obj := ""
		if len(a) > 3 {
			obj = a[3]
			if _, ok := h.sb.Objectives[obj]; !ok {
				tell("No such objective")
				return
			}
		}
		h.sb.Display[slot] = obj
		h.sbBroadcast(players, attachproto.DisplaySlot{Slot: slot, Objective: obj})
		tell("Display slot updated")
	case len(a) == 5 && a[0] == "players" && (a[1] == "set" || a[1] == "add" || a[1] == "remove"):
		owner, obj := a[2], a[3]
		if _, ok := h.sb.Objectives[obj]; !ok {
			tell("No such objective")
			return
		}
		n, err := strconv.Atoi(a[4])
		if err != nil {
			tell("Not a number: " + a[4])
			return
		}
		v := int32(n)
		switch a[1] {
		case "add":
			v = h.sb.Scores[owner][obj] + v
		case "remove":
			v = h.sb.Scores[owner][obj] - v
		}
		h.sbSetScore(players, owner, obj, v)
		tell(fmt.Sprintf("Set %s's %s to %d", owner, obj, v))
	case len(a) >= 3 && a[0] == "players" && a[1] == "reset":
		owner := a[2]
		if len(a) > 3 {
			delete(h.sb.Scores[owner], a[3])
			h.sbBroadcast(players, attachproto.Score{Owner: owner, Objective: a[3], Reset: true})
		} else {
			delete(h.sb.Scores, owner)
			h.sbBroadcast(players, attachproto.Score{Owner: owner, Reset: true})
		}
		tell("Reset " + owner)
	case len(a) >= 2 && a[0] == "players" && a[1] == "list":
		names := make([]string, 0, len(h.sb.Scores))
		for n := range h.sb.Scores {
			names = append(names, n)
		}
		sort.Strings(names)
		tell(fmt.Sprintf("Tracked entities (%d): %s", len(names), strings.Join(names, ", ")))
	default:
		tell("Usage: /scoreboard objectives <add|remove|list|setdisplay> … | players <set|add|remove|reset|list> …")
		return
	}
	h.sbDirty = true
}

var sbTeamColors = map[string]int32{
	"black": 0, "dark_blue": 1, "dark_green": 2, "dark_aqua": 3, "dark_red": 4,
	"dark_purple": 5, "gold": 6, "gray": 7, "dark_gray": 8, "blue": 9,
	"green": 10, "aqua": 11, "red": 12, "light_purple": 13, "yellow": 14,
	"white": 15, "reset": -1,
}

func (h *hub) cmdTeam(players map[int32]*tracked, e evTeamCmd) {
	tell := func(msg string) { e.p.trySendEv(chatEv(msg)) }
	a := e.args
	team := func(name string) *sbTeam {
		t := h.sb.Teams[name]
		if t == nil {
			tell("No such team")
		}
		return t
	}
	switch {
	case len(a) >= 2 && a[0] == "add":
		name := a[1]
		if _, ok := h.sb.Teams[name]; ok {
			tell("A team with that name already exists")
			return
		}
		title := name
		if len(a) > 2 {
			title = strings.Join(a[2:], " ")
		}
		t := &sbTeam{Title: title, Color: -1, Members: map[string]bool{}}
		h.sb.Teams[name] = t
		h.sbBroadcast(players, t.frame(name, attachproto.TeamAdd, nil))
		tell("Created team " + name)
	case len(a) == 2 && a[0] == "remove":
		if team(a[1]) == nil {
			return
		}
		delete(h.sb.Teams, a[1])
		h.sbBroadcast(players, attachproto.Team{Name: a[1], Method: attachproto.TeamRemove})
		tell("Removed team " + a[1])
	case len(a) >= 2 && a[0] == "join":
		t := team(a[1])
		if t == nil {
			return
		}
		who := e.p.name
		if len(a) > 2 {
			who = a[2]
		}
		for name, other := range h.sb.Teams { // vanilla: joining leaves the old team
			if name != a[1] && other.Members[who] {
				delete(other.Members, who)
				h.sbBroadcast(players, attachproto.Team{Name: name,
					Method: attachproto.TeamRemovePlayers, Players: []string{who}})
			}
		}
		t.Members[who] = true
		h.sbBroadcast(players, attachproto.Team{Name: a[1],
			Method: attachproto.TeamAddPlayers, Players: []string{who}})
		tell(fmt.Sprintf("Added %s to %s", who, a[1]))
	case len(a) >= 2 && a[0] == "leave":
		who := e.p.name
		if len(a) > 1 && a[0] == "leave" && len(a) == 2 {
			who = a[1]
		}
		for name, t := range h.sb.Teams {
			if t.Members[who] {
				delete(t.Members, who)
				h.sbBroadcast(players, attachproto.Team{Name: name,
					Method: attachproto.TeamRemovePlayers, Players: []string{who}})
				tell(fmt.Sprintf("Removed %s from %s", who, name))
			}
		}
	case len(a) == 1 && a[0] == "list":
		names := make([]string, 0, len(h.sb.Teams))
		for n := range h.sb.Teams {
			names = append(names, n)
		}
		sort.Strings(names)
		tell(fmt.Sprintf("Teams (%d): %s", len(names), strings.Join(names, ", ")))
	case len(a) >= 4 && a[0] == "modify":
		t := team(a[1])
		if t == nil {
			return
		}
		val := strings.Join(a[3:], " ")
		switch a[2] {
		case "color":
			c, ok := sbTeamColors[val]
			if !ok {
				tell("Unknown color")
				return
			}
			t.Color = c
		case "prefix":
			t.Prefix = val
		case "suffix":
			t.Suffix = val
		case "displayname", "displayName":
			t.Title = val
		case "friendlyfire", "friendlyFire":
			t.FriendlyFire = val == "true"
		case "nametagvisibility", "nametagVisibility":
			vis := map[string]int32{"always": 0, "never": 1, "hideForOtherTeams": 2, "hideForOwnTeam": 3}
			v, ok := vis[val]
			if !ok {
				tell("always|never|hideForOtherTeams|hideForOwnTeam")
				return
			}
			t.Visibility = v
		case "collisionrule", "collisionRule":
			coll := map[string]int32{"always": 0, "never": 1, "pushOtherTeams": 2, "pushOwnTeam": 3}
			v, ok := coll[val]
			if !ok {
				tell("always|never|pushOtherTeams|pushOwnTeam")
				return
			}
			t.Collision = v
		default:
			tell("Options: color, prefix, suffix, displayName, friendlyFire, nametagVisibility, collisionRule")
			return
		}
		h.sbBroadcast(players, t.frame(a[1], attachproto.TeamUpdate, nil))
		tell("Team updated")
	default:
		tell("Usage: /team <add|remove|join|leave|list|modify> …")
		return
	}
	h.sbDirty = true
}
