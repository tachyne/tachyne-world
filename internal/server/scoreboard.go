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
	"log"
	"sort"
	"strconv"
	"strings"
	"sync"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

type sbObjective struct {
	Criteria string `json:"criteria"`
	Title    string `json:"title"`
	Hearts   bool   `json:"hearts,omitempty"` // render type: hearts (else integer)
	// AutoUpdate is Objective.displayAutoUpdate (objectives modify …
	// displayautoupdate).
	AutoUpdate bool `json:"autoupdate,omitempty"`
	// NumberFormat is the objective's default number format (nil: the
	// client's own, the red number).
	NumberFormat *sbNumberFormat `json:"numfmt,omitempty"`

	stat      statKey // the stat a stat criteria names (sbcriteria.go)…
	statState int8    // …parsed once: 0 not yet, 1 a stat, -1 not a stat
}

// sbNumberFormat is a NumberFormat: blank (no number), fixed (text in
// place of the number) or styled (the number in a Style's colour and
// decorations). It rides the Objective and Score frames as
// attach.NumberFormat.
type sbNumberFormat struct {
	Kind          string `json:"kind"` // attach.NumberFormatBlank, …Fixed or …Styled
	Fixed         string `json:"fixed,omitempty"`
	Color         string `json:"color,omitempty"`
	Bold          bool   `json:"bold,omitempty"`
	Italic        bool   `json:"italic,omitempty"`
	Underlined    bool   `json:"underlined,omitempty"`
	Strikethrough bool   `json:"strikethrough,omitempty"`
	Obfuscated    bool   `json:"obfuscated,omitempty"`
}

// frame is the format as the attach frames carry it (nil: the default).
func (f *sbNumberFormat) frame() *attachproto.NumberFormat {
	if f == nil {
		return nil
	}
	return &attachproto.NumberFormat{Kind: f.Kind, Fixed: f.Fixed, Color: f.Color, Bold: f.Bold,
		Italic: f.Italic, Underlined: f.Underlined, Strikethrough: f.Strikethrough, Obfuscated: f.Obfuscated}
}

// sbScoreExtra is what a Score carries beyond its value: a display name in
// place of its owner's, and a number format overriding the objective's.
type sbScoreExtra struct {
	Display      string          `json:"display,omitempty"`
	NumberFormat *sbNumberFormat `json:"numfmt,omitempty"`
}

type sbTeam struct {
	Title        string          `json:"title"`
	Prefix       string          `json:"prefix,omitempty"`
	Suffix       string          `json:"suffix,omitempty"`
	Color        int32           `json:"color"` // -1 = none
	FriendlyFire bool            `json:"-"`     // saved inverted as noff: see MarshalJSON
	SeeInvisible bool            `json:"-"`     // saved inverted as noseeinvis
	Visibility   int32           `json:"vis,omitempty"`
	Collision    int32           `json:"coll,omitempty"`
	DeathVis     int32           `json:"deathvis,omitempty"` // deathMessageVisibility: 0 always, 1 never, 2 hide for other teams, 3 hide for own team
	Members      map[string]bool `json:"members,omitempty"`
}

// A team saves its two vanilla-true options inverted, so an absent key
// reads as vanilla's default (PlayerTeam: allowFriendlyFire and
// seeFriendlyInvisibles both start true). Saves written before this kept
// them as "ff"/"seeinvis" with the engine's old default of false; those
// keys are no longer read, so every such team comes back with vanilla's
// defaults — and friendly fire was not enforced then, so teammates who
// could hurt each other still can.
func (t sbTeam) MarshalJSON() ([]byte, error) {
	type plain sbTeam
	return json.Marshal(struct {
		plain
		NoFF       bool `json:"noff,omitempty"`
		NoSeeInvis bool `json:"noseeinvis,omitempty"`
	}{plain(t), !t.FriendlyFire, !t.SeeInvisible})
}

func (t *sbTeam) UnmarshalJSON(b []byte) error {
	type plain sbTeam
	var v struct {
		plain
		NoFF       bool `json:"noff"`
		NoSeeInvis bool `json:"noseeinvis"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*t = sbTeam(v.plain)
	t.FriendlyFire, t.SeeInvisible = !v.NoFF, !v.NoSeeInvis
	return nil
}

type scoreboardState struct {
	Objectives map[string]*sbObjective     `json:"objectives"`
	Display    [sbSlotCount]string         `json:"display"` // list, sidebar, below_name, sidebar.team.<colour>…
	Scores     map[string]map[string]int32 `json:"scores"`  // owner → objective → value
	Teams      map[string]*sbTeam          `json:"teams"`
	// Unlocked is the trigger objectives each owner may /trigger once
	// (Score.locked, false after /scoreboard players enable).
	Unlocked map[string]map[string]bool `json:"unlocked,omitempty"`
	// Extras is each score's display name and number format (players
	// display name|numberformat), owner → objective.
	Extras map[string]map[string]*sbScoreExtra `json:"extras,omitempty"`
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
		if err := loadStore(path, sb); err != nil {
			log.Fatal(err)
		}
		for _, o := range sb.Objectives {
			if o.Criteria == "deaths" { // saved before the vanilla name
				o.Criteria = "deathCount"
			}
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
	writeStore(s.path, data)
}

// --- frame builders -----------------------------------------------------------

func (o *sbObjective) frame(name string, method int32) attachproto.Objective {
	return attachproto.Objective{Name: name, Method: method, Title: o.Title, Hearts: o.Hearts, Format: o.NumberFormat.frame()}
}

// sbScoreFrame is one score as the Score frame carries it, its own number
// format with it.
func (h *hub) sbScoreFrame(owner, obj string, v int32) attachproto.Score {
	sc := attachproto.Score{Owner: owner, Objective: obj, Value: v}
	if x := h.sb.Extras[owner][obj]; x != nil {
		sc.Format = x.NumberFormat.frame()
	}
	return sc
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
			t.p.sendEv(h.sbScoreFrame(owner, obj, v))
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
	h.sbBroadcast(players, h.sbScoreFrame(owner, obj, v))
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

// sbSlotCount is DisplaySlot's nineteen: list, sidebar, below_name and a
// sidebar per team colour, which a player on a team of that colour sees in
// place of the plain sidebar (the client makes that choice).
const sbSlotCount = 19

var sbSlotNames = func() map[string]int32 {
	m := map[string]int32{
		"list": attachproto.SlotList, "sidebar": attachproto.SlotSidebar,
		"belowname": attachproto.SlotBelowName, "below_name": attachproto.SlotBelowName,
	}
	// sidebar.team.<ChatFormatting colour>, ids 3-18 in the colour order.
	for i, c := range []string{"black", "dark_blue", "dark_green", "dark_aqua", "dark_red", "dark_purple",
		"gold", "gray", "dark_gray", "blue", "green", "aqua", "red", "light_purple", "yellow", "white"} {
		m["sidebar.team."+c] = int32(3 + i)
	}
	return m
}()

// sbValidCriteria is the accepted objective criteria set: dummy (command-set
// only) plus the automatic ones the engine feeds.
var sbValidCriteria = map[string]bool{
	"dummy": true, "trigger": true, "deathCount": true, "totalKillCount": true,
	"playerKillCount": true, "health": true, "food": true, "air": true,
	"armor": true, "xp": true, "level": true,
}

// sbReadOnly are ObjectiveCriteria's read-only gauges: the game keeps them,
// and /scoreboard players set|add|remove refuses them.
var sbReadOnly = map[string]bool{"health": true, "food": true, "air": true, "armor": true, "xp": true, "level": true}

// sbHolders is ScoreHolderArgument: "*" is every tracked score holder, a
// selector the scoreboard names of the entities it picks (a player's name,
// any other entity's UUID), and anything else a name taken as it is.
func (h *hub) sbHolders(players map[int32]*tracked, by int32, arg string) []string {
	switch {
	case arg == "*":
		out := make([]string, 0, len(h.sb.Scores))
		for owner := range h.sb.Scores {
			out = append(out, owner)
		}
		sort.Strings(out)
		return out
	case strings.HasPrefix(arg, "@"):
		var out []string
		for _, en := range h.commandEntitiesAll(players, by, arg) {
			out = append(out, en.scoreName())
		}
		return out
	}
	return []string{arg}
}

// scoreName is Entity.getScoreboardName: a player's name, any other
// entity's UUID.
func (e cmdEntity) scoreName() string {
	switch {
	case e.t != nil:
		return e.t.p.name
	case e.o != nil:
		return uuidString(e.o.uuid)
	}
	return uuidString(e.m.uuid)
}

// sbFormatted is Objective.getFormattedDisplayName: the title in brackets.
func (o *sbObjective) sbFormatted() string { return "[" + o.Title + "]" }

// componentArgText is a ComponentArgument's text: a JSON component
// flattened, a quoted string without its quotes, anything else as typed.
func componentArgText(s string) string {
	s = strings.TrimSpace(s)
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return flattenComponent(v)
	}
	return unquoteArg(s)
}

// parseNumberFormat reads a NumberFormat argument: blank, fixed <component>
// or styled <style> (a Style in JSON or SNBT: color, bold, italic,
// underlined, strikethrough, obfuscated). No words at all is none (a
// clear).
func parseNumberFormat(a []string) (*sbNumberFormat, bool) {
	if len(a) == 0 {
		return nil, true
	}
	switch a[0] {
	case "blank":
		if len(a) == 1 {
			return &sbNumberFormat{Kind: attachproto.NumberFormatBlank}, true
		}
	case "fixed":
		if len(a) > 1 {
			return &sbNumberFormat{Kind: attachproto.NumberFormatFixed, Fixed: componentArgText(strings.Join(a[1:], " "))}, true
		}
	case "styled":
		if len(a) > 1 {
			text := strings.Join(a[1:], " ")
			var style map[string]any
			if json.Unmarshal([]byte(text), &style) != nil {
				v, err := parseSNBT(text)
				m, isCompound := v.(map[string]any)
				if err != nil || !isCompound {
					return nil, false
				}
				style = m
			}
			f := &sbNumberFormat{Kind: attachproto.NumberFormatStyled}
			if c, isString := style["color"].(string); isString {
				f.Color = c
			}
			flag := func(key string) bool {
				switch v := style[key].(type) {
				case bool:
					return v
				case int64:
					return v != 0
				}
				return false
			}
			f.Bold, f.Italic, f.Underlined = flag("bold"), flag("italic"), flag("underlined")
			f.Strikethrough, f.Obfuscated = flag("strikethrough"), flag("obfuscated")
			return f, true
		}
	}
	return nil, false
}

// sbExtra is a score's extras, made on first use.
func (h *hub) sbExtra(owner, obj string) *sbScoreExtra {
	if h.sb.Extras == nil {
		h.sb.Extras = map[string]map[string]*sbScoreExtra{}
	}
	m := h.sb.Extras[owner]
	if m == nil {
		m = map[string]*sbScoreExtra{}
		h.sb.Extras[owner] = m
	}
	x := m[obj]
	if x == nil {
		x = &sbScoreExtra{}
		m[obj] = x
	}
	return x
}

// sbResetScore is Scoreboard.resetSinglePlayerScore: the score goes, with
// its lock and its extras.
func (h *hub) sbResetScore(players map[int32]*tracked, owner, obj string) {
	delete(h.sb.Scores[owner], obj)
	delete(h.sb.Unlocked[owner], obj)
	delete(h.sb.Extras[owner], obj)
	h.sbBroadcast(players, attachproto.Score{Owner: owner, Objective: obj, Reset: true})
}

// sbOperation is OperationArgument's operations on a target score a and a
// source score b: the new pair, or an error line.
func sbOperation(op string, a, b int32) (int32, int32, string) {
	switch op {
	case "=":
		return b, b, ""
	case "+=":
		return a + b, b, ""
	case "-=":
		return a - b, b, ""
	case "*=":
		return a * b, b, ""
	case "/=":
		if b == 0 {
			return a, b, "Cannot divide by zero"
		}
		return floorDiv32(a, b), b, ""
	case "%=":
		if b == 0 {
			return a, b, "Cannot divide by zero"
		}
		m := a % b // Mth.positiveModulo is Math.floorMod: the divisor's sign
		if m != 0 && (m < 0) != (b < 0) {
			m += b
		}
		return m, b, ""
	case "<":
		return min(a, b), b, ""
	case ">":
		return max(a, b), b, ""
	case "><":
		return b, a, ""
	}
	return a, b, "Invalid operation"
}

// floorDiv32 is Math.floorDiv on ints.
func floorDiv32(a, b int32) int32 {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func (h *hub) cmdScoreboard(players map[int32]*tracked, e evScoreboardCmd) {
	tell := func(msg string) { e.p.trySendEv(chatEv(msg)) }             // a failure
	ok := func(msg string) { h.cmdSuccess(players, e.p, msg, true) }    // sendSuccess(…, true)
	info := func(msg string) { h.cmdSuccess(players, e.p, msg, false) } // sendSuccess(…, false)
	objective := func(name string) *sbObjective {
		o := h.sb.Objectives[name]
		if o == nil {
			tell("Unknown scoreboard objective '" + name + "'")
		}
		return o
	}
	writable := func(name string) *sbObjective { // ObjectiveArgument.getWritableObjective
		o := objective(name)
		if o != nil && sbReadOnly[o.Criteria] {
			tell("Scoreboard objective '" + name + "' is read-only")
			return nil
		}
		return o
	}
	holders := func(arg string) []string {
		hs := h.sbHolders(players, e.p.eid, arg)
		if len(hs) == 0 {
			tell("No relevant score holders could be found")
		}
		return hs
	}
	a := e.args
	switch {
	case len(a) >= 4 && a[0] == "objectives" && a[1] == "add":
		name := a[2]
		criteria, valid := sbCriteriaName(a[3])
		if !valid {
			tell("Unknown criterion '" + a[3] + "'")
			return
		}
		if _, exists := h.sb.Objectives[name]; exists {
			tell("An objective already exists by that name")
			return
		}
		title := name
		if len(a) > 4 {
			title = componentArgText(strings.Join(a[4:], " "))
		}
		o := &sbObjective{Criteria: criteria, Title: title, Hearts: criteria == "health"}
		h.sb.Objectives[name] = o
		h.sbBroadcast(players, o.frame(name, attachproto.ObjAdd))
		ok("Created new objective " + o.sbFormatted())
	case len(a) == 3 && a[0] == "objectives" && a[1] == "remove":
		name := a[2]
		o := objective(name)
		if o == nil {
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
		for _, un := range h.sb.Unlocked {
			delete(un, name)
		}
		for _, x := range h.sb.Extras {
			delete(x, name)
		}
		h.sbBroadcast(players, attachproto.Objective{Name: name, Method: attachproto.ObjRemove})
		ok("Removed objective " + o.sbFormatted())
	case len(a) == 2 && a[0] == "objectives" && a[1] == "list":
		names := make([]string, 0, len(h.sb.Objectives))
		for n := range h.sb.Objectives {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) == 0 {
			info("There are no objectives")
			return
		}
		shown := make([]string, len(names))
		for i, n := range names {
			shown[i] = h.sb.Objectives[n].sbFormatted()
		}
		info(fmt.Sprintf("There are %d objective(s): %s", len(names), strings.Join(shown, ", ")))
		return
	case len(a) >= 3 && len(a) <= 4 && a[0] == "objectives" && a[1] == "setdisplay":
		slot, known := sbSlotNames[a[2]]
		if !known {
			tell("Unknown display slot '" + a[2] + "'")
			return
		}
		if len(a) == 3 { // clearDisplaySlot
			if h.sb.Display[slot] == "" {
				tell("Nothing changed. That display slot is already empty")
				return
			}
			h.sb.Display[slot] = ""
			h.sbBroadcast(players, attachproto.DisplaySlot{Slot: slot})
			ok("Cleared any objectives in display slot " + a[2])
			break
		}
		o := objective(a[3])
		if o == nil {
			return
		}
		if h.sb.Display[slot] == a[3] {
			tell("Nothing changed. That display slot is already showing that objective")
			return
		}
		h.sb.Display[slot] = a[3]
		h.sbBroadcast(players, attachproto.DisplaySlot{Slot: slot, Objective: a[3]})
		ok(fmt.Sprintf("Set display slot %s to show objective %s", a[2], o.Title))
	case len(a) >= 4 && a[0] == "objectives" && a[1] == "modify":
		name := a[2]
		o := objective(name)
		if o == nil {
			return
		}
		rest := a[4:]
		switch a[3] {
		case "displayname":
			if len(rest) == 0 {
				tell("Usage: /scoreboard objectives modify <objective> displayname <displayName>")
				return
			}
			title := componentArgText(strings.Join(rest, " "))
			if title == o.Title {
				return // setDisplayName: an unchanged name says nothing
			}
			o.Title = title
			h.sbBroadcast(players, o.frame(name, attachproto.ObjUpdate))
			ok(fmt.Sprintf("Changed the display name of %s to %s", name, o.sbFormatted()))
		case "rendertype":
			if len(rest) != 1 || (rest[0] != "hearts" && rest[0] != "integer") {
				tell("Usage: /scoreboard objectives modify <objective> rendertype hearts|integer")
				return
			}
			hearts := rest[0] == "hearts"
			if hearts == o.Hearts {
				return
			}
			o.Hearts = hearts
			h.sbBroadcast(players, o.frame(name, attachproto.ObjUpdate))
			ok("Changed the render type of objective " + o.sbFormatted())
		case "displayautoupdate":
			v, isBool := parseSelectorBool(strings.Join(rest, ""))
			if len(rest) != 1 || !isBool {
				tell("Usage: /scoreboard objectives modify <objective> displayautoupdate true|false")
				return
			}
			if v == o.AutoUpdate {
				return
			}
			o.AutoUpdate = v
			if v {
				ok(fmt.Sprintf("Enabled display auto-update for objective %s", o.sbFormatted()))
			} else {
				ok(fmt.Sprintf("Disabled display auto-update for objective %s", o.sbFormatted()))
			}
		case "numberformat":
			nf, valid := parseNumberFormat(rest)
			if !valid {
				tell("Usage: /scoreboard objectives modify <objective> numberformat [blank|fixed <contents>|styled <style>]")
				return
			}
			o.NumberFormat = nf
			h.sbBroadcast(players, o.frame(name, attachproto.ObjUpdate))
			if nf != nil {
				ok("Changed default number format of objective " + name)
			} else {
				ok("Cleared default number format of objective " + name)
			}
		default:
			tell("Usage: /scoreboard objectives modify <objective> displayname|rendertype|displayautoupdate|numberformat …")
			return
		}
	case len(a) == 5 && a[0] == "players" && (a[1] == "set" || a[1] == "add" || a[1] == "remove"):
		o := writable(a[3])
		if o == nil {
			return
		}
		n, err := strconv.ParseInt(a[4], 10, 32)
		if err != nil || (a[1] != "set" && n < 0) {
			tell("Invalid integer '" + a[4] + "'")
			return
		}
		owners := holders(a[2])
		if len(owners) == 0 {
			return
		}
		var last int32
		for _, owner := range owners {
			v := int32(n)
			switch a[1] {
			case "add":
				v = h.sb.Scores[owner][a[3]] + int32(n)
			case "remove":
				v = h.sb.Scores[owner][a[3]] - int32(n)
			}
			h.sbSetScore(players, owner, a[3], v)
			last = v
		}
		single := len(owners) == 1
		switch {
		case a[1] == "set" && single:
			ok(fmt.Sprintf("Set %s for %s to %d", o.sbFormatted(), owners[0], n))
		case a[1] == "set":
			ok(fmt.Sprintf("Set %s for %d entities to %d", o.sbFormatted(), len(owners), n))
		case a[1] == "add" && single:
			ok(fmt.Sprintf("Added %d to %s for %s (now %d)", n, o.sbFormatted(), owners[0], last))
		case a[1] == "add":
			ok(fmt.Sprintf("Added %d to %s for %d entities", n, o.sbFormatted(), len(owners)))
		case single:
			ok(fmt.Sprintf("Removed %d from %s for %s (now %d)", n, o.sbFormatted(), owners[0], last))
		default:
			ok(fmt.Sprintf("Removed %d from %s for %d entities", n, o.sbFormatted(), len(owners)))
		}
	case len(a) == 4 && a[0] == "players" && a[1] == "get":
		o := objective(a[3])
		if o == nil {
			return
		}
		owners := holders(a[2])
		if len(owners) == 0 {
			return
		}
		if len(owners) > 1 {
			tell("Only one entity is allowed, but the provided selector allows more than one")
			return
		}
		v, has := h.sb.Scores[owners[0]][a[3]]
		if !has {
			tell(fmt.Sprintf("Can't get value of %s for %s; none is set", a[3], owners[0]))
			return
		}
		info(fmt.Sprintf("%s has %d %s", owners[0], v, o.sbFormatted()))
		return
	case len(a) == 7 && a[0] == "players" && a[1] == "operation":
		to := writable(a[3])
		if to == nil {
			return
		}
		from := objective(a[6])
		if from == nil {
			return
		}
		if _, _, bad := sbOperation(a[4], 0, 1); bad != "" {
			tell(bad)
			return
		}
		targets := holders(a[2])
		if len(targets) == 0 {
			return
		}
		sources := holders(a[5])
		if len(sources) == 0 {
			return
		}
		var last int32
		for _, target := range targets {
			if _, has := h.sb.Scores[target][a[3]]; !has {
				h.sbSetScore(players, target, a[3], 0) // getOrCreatePlayerScore
			}
			tv := h.sb.Scores[target][a[3]]
			for _, source := range sources {
				same := source == target && a[6] == a[3] // one score on both sides
				if _, has := h.sb.Scores[source][a[6]]; !has && !same {
					h.sbSetScore(players, source, a[6], 0) // made before the operation runs
				}
				sv := h.sb.Scores[source][a[6]]
				if same {
					sv = tv
				}
				nt, ns, bad := sbOperation(a[4], tv, sv)
				if bad != "" {
					tell(bad)
					return
				}
				tv = nt
				if !same {
					h.sbSetScore(players, source, a[6], ns)
				}
			}
			h.sbSetScore(players, target, a[3], tv)
			last = tv
		}
		if len(targets) == 1 {
			ok(fmt.Sprintf("Set %s for %s to %d", to.sbFormatted(), targets[0], last))
		} else {
			ok(fmt.Sprintf("Updated %s for %d entities", to.sbFormatted(), len(targets)))
		}
	case len(a) >= 5 && a[0] == "players" && a[1] == "display" && (a[2] == "name" || a[2] == "numberformat"):
		o := objective(a[4])
		if o == nil {
			return
		}
		owners := holders(a[3])
		if len(owners) == 0 {
			return
		}
		rest := a[5:]
		single := len(owners) == 1
		who := owners[0]
		if !single {
			who = fmt.Sprintf("%d entities", len(owners))
		}
		if a[2] == "name" {
			display := ""
			if len(rest) > 0 {
				display = componentArgText(strings.Join(rest, " "))
			}
			for _, owner := range owners {
				if _, has := h.sb.Scores[owner][a[4]]; !has {
					h.sbSetScore(players, owner, a[4], 0) // getOrCreatePlayerScore
				}
				h.sbExtra(owner, a[4]).Display = display
			}
			if display == "" {
				ok(fmt.Sprintf("Cleared display name for %s in %s", who, o.sbFormatted()))
			} else {
				ok(fmt.Sprintf("Changed display name to %s for %s in %s", display, who, o.sbFormatted()))
			}
			break
		}
		nf, valid := parseNumberFormat(rest)
		if !valid {
			tell("Usage: /scoreboard players display numberformat <targets> <objective> [blank|fixed <contents>|styled <style>]")
			return
		}
		for _, owner := range owners {
			if _, has := h.sb.Scores[owner][a[4]]; !has {
				h.sbSetScore(players, owner, a[4], 0)
			}
			h.sbExtra(owner, a[4]).NumberFormat = nf
			// numberFormatOverride: the score is sent again, with its format.
			h.sbBroadcast(players, h.sbScoreFrame(owner, a[4], h.sb.Scores[owner][a[4]]))
		}
		if nf == nil {
			ok(fmt.Sprintf("Cleared number format for %s in %s", who, o.sbFormatted()))
		} else {
			ok(fmt.Sprintf("Changed number format for %s in %s", who, o.sbFormatted()))
		}
	case len(a) == 4 && a[0] == "players" && a[1] == "enable":
		// ScoreboardCommand.enableTrigger: a trigger objective only; the
		// score is made (at 0) if the owner had none, and unlocked.
		o := objective(a[3])
		if o == nil {
			return
		}
		if o.Criteria != "trigger" {
			tell("Enable only works on trigger-objectives")
			return
		}
		owners := holders(a[2])
		if len(owners) == 0 {
			return
		}
		enabled := 0
		for _, owner := range owners {
			if h.sbUnlocked(owner, a[3]) {
				continue
			}
			if _, has := h.sb.Scores[owner][a[3]]; !has {
				h.sbSetScore(players, owner, a[3], 0)
			}
			if h.sb.Unlocked == nil {
				h.sb.Unlocked = map[string]map[string]bool{}
			}
			if h.sb.Unlocked[owner] == nil {
				h.sb.Unlocked[owner] = map[string]bool{}
			}
			h.sb.Unlocked[owner][a[3]] = true
			enabled++
		}
		switch {
		case enabled == 0:
			tell("Nothing changed. That trigger is already enabled")
			return
		case len(owners) == 1:
			ok(fmt.Sprintf("Enabled trigger %s for %s", o.sbFormatted(), owners[0]))
		default:
			ok(fmt.Sprintf("Enabled trigger %s for %d entities", o.sbFormatted(), enabled))
		}
	case len(a) >= 3 && len(a) <= 4 && a[0] == "players" && a[1] == "reset":
		owners := holders(a[2])
		if len(owners) == 0 {
			return
		}
		if len(a) == 4 {
			o := objective(a[3])
			if o == nil {
				return
			}
			for _, owner := range owners {
				h.sbResetScore(players, owner, a[3])
			}
			if len(owners) == 1 {
				ok(fmt.Sprintf("Reset %s for %s", o.sbFormatted(), owners[0]))
			} else {
				ok(fmt.Sprintf("Reset %s for %d entities", o.sbFormatted(), len(owners)))
			}
			break
		}
		for _, owner := range owners {
			delete(h.sb.Scores, owner)
			delete(h.sb.Unlocked, owner)
			delete(h.sb.Extras, owner)
			h.sbBroadcast(players, attachproto.Score{Owner: owner, Reset: true})
		}
		if len(owners) == 1 {
			ok("Reset all scores for " + owners[0])
		} else {
			ok(fmt.Sprintf("Reset all scores for %d entities", len(owners)))
		}
	case len(a) == 2 && a[0] == "players" && a[1] == "list":
		names := make([]string, 0, len(h.sb.Scores))
		for n := range h.sb.Scores {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) == 0 {
			info("There are no tracked entities")
			return
		}
		info(fmt.Sprintf("There are %d tracked entity/entities: %s", len(names), strings.Join(names, ", ")))
		return
	case len(a) == 3 && a[0] == "players" && a[1] == "list":
		owners := holders(a[2])
		if len(owners) == 0 {
			return
		}
		owner := owners[0]
		scores := h.sb.Scores[owner]
		if len(scores) == 0 {
			info(owner + " has no scores to show")
			return
		}
		objs := make([]string, 0, len(scores))
		for obj := range scores {
			objs = append(objs, obj)
		}
		sort.Strings(objs)
		info(fmt.Sprintf("%s has %d score(s):", owner, len(objs)))
		for _, obj := range objs {
			title := obj
			if o := h.sb.Objectives[obj]; o != nil {
				title = o.sbFormatted()
			}
			info(fmt.Sprintf("%s: %d", title, scores[obj]))
		}
		return
	default:
		tell("Usage: /scoreboard objectives <add|remove|list|setdisplay|modify> … | players <set|add|remove|reset|list|get|enable|operation|display> …")
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

// sbTeamFormatted is PlayerTeam.getFormattedDisplayName: the title in
// brackets.
func (t *sbTeam) sbTeamFormatted() string { return "[" + t.Title + "]" }

// sbVisibilityNames and sbCollisionNames are Team.Visibility's and
// Team.CollisionRule's serialized names by id, with their display names.
var (
	sbVisibilityIDs  = map[string]int32{"always": 0, "never": 1, "hideForOtherTeams": 2, "hideForOwnTeam": 3}
	sbVisibilityText = []string{"Always", "Never", "Hide for other teams", "Hide for own team"}
	sbCollisionIDs   = map[string]int32{"always": collAlways, "never": collNever, "pushOtherTeams": collPushOtherTeams, "pushOwnTeam": collPushOwnTeam}
	sbCollisionText  = []string{"Always", "Never", "Push other teams", "Push own team"}
)

// sbLeaveTeam is Scoreboard.removePlayerFromTeam: the member leaves
// whatever team it is on. It reports whether it was on one.
func (h *hub) sbLeaveTeam(players map[int32]*tracked, who string) bool {
	for name, t := range h.sb.Teams {
		if t.Members[who] {
			delete(t.Members, who)
			h.sbBroadcast(players, attachproto.Team{Name: name,
				Method: attachproto.TeamRemovePlayers, Players: []string{who}})
			return true
		}
	}
	return false
}

func (h *hub) cmdTeam(players map[int32]*tracked, e evTeamCmd) {
	tell := func(msg string) { e.p.trySendEv(chatEv(msg)) }
	ok := func(msg string) { h.cmdSuccess(players, e.p, msg, true) }
	info := func(msg string) { h.cmdSuccess(players, e.p, msg, false) }
	a := e.args
	team := func(name string) *sbTeam {
		t := h.sb.Teams[name]
		if t == nil {
			tell("Unknown team '" + name + "'")
		}
		return t
	}
	holders := func(arg string) []string {
		hs := h.sbHolders(players, e.p.eid, arg)
		if len(hs) == 0 {
			tell("No relevant score holders could be found")
		}
		return hs
	}
	switch {
	case len(a) >= 2 && a[0] == "add":
		name := a[1]
		if _, exists := h.sb.Teams[name]; exists {
			tell("A team already exists by that name")
			return
		}
		title := name
		if len(a) > 2 {
			title = componentArgText(strings.Join(a[2:], " "))
		}
		// PlayerTeam's defaults: friendly fire on, friendly invisibles seen.
		t := &sbTeam{Title: title, Color: -1, FriendlyFire: true, SeeInvisible: true, Members: map[string]bool{}}
		h.sb.Teams[name] = t
		h.sbBroadcast(players, t.frame(name, attachproto.TeamAdd, nil))
		ok("Created team " + t.sbTeamFormatted())
	case len(a) == 2 && a[0] == "remove":
		t := team(a[1])
		if t == nil {
			return
		}
		delete(h.sb.Teams, a[1])
		h.sbBroadcast(players, attachproto.Team{Name: a[1], Method: attachproto.TeamRemove})
		ok("Removed team " + t.sbTeamFormatted())
	case len(a) == 2 && a[0] == "empty":
		t := team(a[1])
		if t == nil {
			return
		}
		if len(t.Members) == 0 {
			tell("Nothing changed. That team is already empty")
			return
		}
		members := sortedMembers(t)
		t.Members = map[string]bool{}
		h.sbBroadcast(players, attachproto.Team{Name: a[1], Method: attachproto.TeamRemovePlayers, Players: members})
		ok(fmt.Sprintf("Removed %d member(s) from team %s", len(members), t.sbTeamFormatted()))
	case len(a) >= 2 && len(a) <= 3 && a[0] == "join":
		t := team(a[1])
		if t == nil {
			return
		}
		who := []string{e.p.name}
		if len(a) == 3 {
			if who = holders(a[2]); len(who) == 0 {
				return
			}
		}
		for _, w := range who {
			h.sbLeaveTeam(players, w) // addPlayerToTeam: off the old team first
			t.Members[w] = true
			h.sbBroadcast(players, attachproto.Team{Name: a[1],
				Method: attachproto.TeamAddPlayers, Players: []string{w}})
		}
		if len(who) == 1 {
			ok(fmt.Sprintf("Added %s to team %s", who[0], t.sbTeamFormatted()))
		} else {
			ok(fmt.Sprintf("Added %d members to team %s", len(who), t.sbTeamFormatted()))
		}
	case len(a) == 2 && a[0] == "leave":
		who := holders(a[1])
		if len(who) == 0 {
			return
		}
		left, last := 0, ""
		for _, w := range who {
			if h.sbLeaveTeam(players, w) {
				left, last = left+1, w
			}
		}
		if left == 1 {
			ok(fmt.Sprintf("Removed %s from any team", last))
		} else { // the response tracker counts those that were on a team, 0 included
			ok(fmt.Sprintf("Removed %d members from any team", left))
		}
	case len(a) == 1 && a[0] == "list":
		names := make([]string, 0, len(h.sb.Teams))
		for n := range h.sb.Teams {
			names = append(names, n)
		}
		sort.Strings(names)
		if len(names) == 0 {
			info("There are no teams")
			return
		}
		shown := make([]string, len(names))
		for i, n := range names {
			shown[i] = h.sb.Teams[n].sbTeamFormatted()
		}
		info(fmt.Sprintf("There are %d team(s): %s", len(names), strings.Join(shown, ", ")))
		return
	case len(a) == 2 && a[0] == "list":
		t := team(a[1])
		if t == nil {
			return
		}
		if len(t.Members) == 0 {
			info("There are no members on team " + t.sbTeamFormatted())
			return
		}
		members := sortedMembers(t)
		info(fmt.Sprintf("Team %s has %d member(s): %s", t.sbTeamFormatted(), len(members), strings.Join(members, ", ")))
		return
	case len(a) >= 3 && a[0] == "modify":
		t := team(a[1])
		if t == nil {
			return
		}
		val := strings.Join(a[3:], " ")
		boolVal := func() (bool, bool) {
			b, isBool := parseSelectorBool(val)
			if !isBool {
				tell("Invalid boolean, expected 'true' or 'false' but found '" + val + "'")
			}
			return b, isBool
		}
		switch a[2] {
		case "color":
			c, known := sbTeamColors[val]
			if !known {
				tell("Unknown color '" + val + "'")
				return
			}
			if c == t.Color {
				tell("Nothing changed. That team already has that color")
				return
			}
			t.Color = c
			if c < 0 {
				ok("Cleared the color for team " + t.sbTeamFormatted())
			} else {
				ok(fmt.Sprintf("Updated the color for team %s to %s", t.sbTeamFormatted(), val))
			}
		case "prefix":
			t.Prefix = componentArgText(val)
			info("Team prefix set to " + t.Prefix)
		case "suffix":
			t.Suffix = componentArgText(val)
			info("Team suffix set to " + t.Suffix)
		case "displayName":
			title := componentArgText(val)
			if title == t.Title {
				tell("Nothing changed. That team already has that name")
				return
			}
			t.Title = title
			ok("Updated the name of team " + t.sbTeamFormatted())
		case "friendlyFire":
			b, isBool := boolVal()
			if !isBool {
				return
			}
			if b == t.FriendlyFire {
				if b {
					tell("Nothing changed. Friendly fire is already enabled for that team")
				} else {
					tell("Nothing changed. Friendly fire is already disabled for that team")
				}
				return
			}
			t.FriendlyFire = b
			if b {
				ok("Enabled friendly fire for team " + t.sbTeamFormatted())
			} else {
				ok("Disabled friendly fire for team " + t.sbTeamFormatted())
			}
		case "seeFriendlyInvisibles":
			b, isBool := boolVal()
			if !isBool {
				return
			}
			if b == t.SeeInvisible {
				if b {
					tell("Nothing changed. That team can already see invisible teammates")
				} else {
					tell("Nothing changed. That team already can't see invisible teammates")
				}
				return
			}
			t.SeeInvisible = b
			if b {
				ok("Team " + t.sbTeamFormatted() + " can now see invisible teammates")
			} else {
				ok("Team " + t.sbTeamFormatted() + " can no longer see invisible teammates")
			}
		case "nametagVisibility", "deathMessageVisibility":
			v, known := sbVisibilityIDs[val]
			if !known {
				tell("always|never|hideForOtherTeams|hideForOwnTeam")
				return
			}
			field, what := &t.Visibility, "Nametag visibility"
			if a[2] == "deathMessageVisibility" {
				field, what = &t.DeathVis, "Death message visibility"
			}
			if *field == v {
				tell("Nothing changed. " + what + " is already that value")
				return
			}
			*field = v
			ok(fmt.Sprintf("%s for team %s is now \"%s\"", what, t.sbTeamFormatted(), sbVisibilityText[v]))
		case "collisionRule":
			v, known := sbCollisionIDs[val]
			if !known {
				tell("always|never|pushOtherTeams|pushOwnTeam")
				return
			}
			if t.Collision == v {
				tell("Nothing changed. Collision rule is already that value")
				return
			}
			t.Collision = v
			ok(fmt.Sprintf("Collision rule for team %s is now \"%s\"", t.sbTeamFormatted(), sbCollisionText[v]))
		default:
			tell("Options: color, prefix, suffix, displayName, friendlyFire, seeFriendlyInvisibles, nametagVisibility, deathMessageVisibility, collisionRule")
			return
		}
		h.sbBroadcast(players, t.frame(a[1], attachproto.TeamUpdate, nil))
	default:
		tell("Usage: /team <add|remove|empty|join|leave|list|modify> …")
		return
	}
	h.sbDirty = true
}

type evTriggerCmd struct {
	p    *player
	args []string
}

func (evTriggerCmd) isHubEvent() {}

func (h *hub) sbUnlocked(owner, obj string) bool { return h.sb.Unlocked[owner][obj] }

// cmdTrigger is TriggerCommand, open to every player: /trigger <objective>
// [add|set <value>] changes the player's own score on a trigger objective
// an operator has enabled for them, once — the trigger locks again after.
func (h *hub) cmdTrigger(players map[int32]*tracked, e evTriggerCmd) {
	tell := func(msg string) { e.p.trySendEv(chatEv(msg)) }
	a := e.args
	if len(a) != 1 && len(a) != 3 {
		tell("Usage: /trigger <objective> [add|set <value>]")
		return
	}
	obj, owner := a[0], e.p.name
	o, ok := h.sb.Objectives[obj]
	if !ok {
		tell("Unknown scoreboard objective '" + obj + "'")
		return
	}
	if o.Criteria != "trigger" {
		tell("You can only trigger objectives that are 'trigger' type")
		return
	}
	if !h.sbUnlocked(owner, obj) {
		tell("You cannot trigger this objective yet")
		return
	}
	cur := h.sb.Scores[owner][obj]
	v, msg := cur+1, fmt.Sprintf("Triggered [%s]", o.Title)
	if len(a) == 3 {
		n, err := strconv.Atoi(a[2])
		if err != nil {
			tell("Not a number: " + a[2])
			return
		}
		switch a[1] {
		case "add":
			v, msg = cur+int32(n), fmt.Sprintf("Triggered [%s] (added %d to value)", o.Title, n)
		case "set":
			v, msg = int32(n), fmt.Sprintf("Triggered [%s] (set value to %d)", o.Title, n)
		default:
			tell("Usage: /trigger <objective> [add|set <value>]")
			return
		}
	}
	delete(h.sb.Unlocked[owner], obj) // Score.lock
	h.sbSetScore(players, owner, obj, v)
	h.sbDirty = true
	tell(msg)
}

// sbGauges feeds the read-only criteria — health, food, air, armor, xp,
// level — once a second; sbSetScore sends only the values that moved.
func (h *hub) sbGauges(players map[int32]*tracked) {
	if len(h.sb.Objectives) == 0 {
		return
	}
	for _, t := range players {
		if t.dead {
			continue
		}
		h.sbCriteria(players, "health", t.p.name, int32(t.health+t.absorption+0.5), true)
		h.sbCriteria(players, "food", t.p.name, int32(t.food), true)
		h.sbCriteria(players, "air", t.p.name, int32(t.air), true)
		h.sbCriteria(players, "armor", t.p.name, int32(t.armorPoints()), true)
		h.sbCriteria(players, "xp", t.p.name, int32(totalXP(t.xpLevel, t.xpPoints)), true)
		h.sbCriteria(players, "level", t.p.name, int32(t.xpLevel), true)
	}
}

// deathMessageReaches is ServerPlayer.die's team rule for who reads a
// player's death message: everyone, nobody, only their team, or everyone
// but their team.
func (h *hub) deathMessageReaches(dead, reader string) bool {
	tn := h.teamOf(dead)
	if tn == "" {
		return true
	}
	same := h.teamOf(reader) == tn
	switch h.sb.Teams[tn].DeathVis {
	case 1:
		return false
	case 2:
		return same
	case 3:
		return !same
	}
	return true
}
