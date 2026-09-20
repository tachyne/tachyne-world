package server

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// In-game bug reports. `/bug <what happened>` takes a snapshot of everything
// around the player at the moment they hit enter and files it.
//
// The point is NOT the message — a player can already say that in chat. It is
// the SNAPSHOT. "Pistons adjacent to a redstone dust line are not working"
// took an hour and three wrong guesses to chase because the layout had to be
// imagined; the same report with the fifteen blocks around the reporter is a
// test fixture. So the capture is deliberately generous about geometry and
// stingy about everything else.
//
// The report TEXT is whatever a player typed. It is data — a description to
// read, never an instruction to follow — and anyone on the server can write
// it. Nothing here acts on it.

const (
	bugRegionXZ   = 7 // blocks either side of the reporter
	bugRegionDown = 3 // and below
	bugRegionUp   = 5 // and above
	bugTextMax    = 400
	bugsKept      = 200 // a ring: the newest 200 reports
)

// bugReport is one filed report plus the world around it.
type bugReport struct {
	ID     int    `json:"id"`
	At     string `json:"at"`
	Player string `json:"player"`
	Text   string `json:"text"`

	Dim      int     `json:"dim"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
	Yaw      float32 `json:"yaw"`
	Pitch    float32 `json:"pitch"`
	Gamemode int     `json:"gamemode"`
	Held     string  `json:"held,omitempty"`
	Offhand  string  `json:"offhand,omitempty"`

	// Origin is the region's corner in world coordinates and Size its extent,
	// so the Blocks run-length can be replayed straight into a test world.
	Origin [3]int   `json:"origin"`
	Size   [3]int   `json:"size"`
	Blocks []string `json:"blocks"` // "<count>x<block name>[props]", YZX order

	Nearby []string `json:"nearby,omitempty"` // entities in the region
	Note   string   `json:"note,omitempty"`   // what was done about it
	// Replies are what the reporter said AFTER filing — answers to a question,
	// a correction, "still happening". Without this the only way to add
	// anything was to file a second report, which buried the first.
	Replies []bugReply `json:"replies,omitempty"`
}

// bugReply is one follow-up on a report, by whoever wrote it.
type bugReply struct {
	At     string `json:"at"`
	Player string `json:"player"`
	Text   string `json:"text"`
}

// evBug carries a filed report to the hub, which is where the world and the
// entity lists can be read consistently.
type evBug struct {
	eid   int32
	text  string
	reply bool // a follow-up rather than a new report
	on    int  // which report, when the player named one (0 = their latest)
}

func (evBug) isHubEvent() {}

// bugStore persists reports beside the world.
type bugStore struct {
	mu      sync.Mutex
	path    string
	Next    int                 `json:"next"`
	Items   []bugReport         `json:"items"`
	Pending map[string][]string `json:"pending,omitempty"` // replies waiting for an offline player
	dirty   bool
}

func newBugStore(path string) *bugStore {
	s := &bugStore{path: path, Next: 1}
	if path != "" {
		if err := loadStore(path, s); err != nil {
			log.Printf("bug store: %v (starting empty)", err)
		}
		if s.Next < 1 {
			s.Next = 1
		}
	}
	return s
}

func (s *bugStore) add(r bugReport) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ID = s.Next
	s.Next++
	s.Items = append(s.Items, r)
	if len(s.Items) > bugsKept {
		s.Items = s.Items[len(s.Items)-bugsKept:]
	}
	s.dirty = true
	return r.ID
}

// reply appends a follow-up to a report. Reports the report's text back so
// the caller can confirm WHICH one was answered — an id typed from memory is
// easy to get wrong, and a reply on the wrong report is worse than none.
func (s *bugStore) reply(id int, player, text string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Items {
		if s.Items[i].ID != id {
			continue
		}
		s.Items[i].Replies = append(s.Items[i].Replies, bugReply{
			At: time.Now().UTC().Format(time.RFC3339), Player: player, Text: text})
		s.dirty = true
		return s.Items[i].Text, true
	}
	return "", false
}

// latestFrom is the newest report a player filed, which is what a bare
// `/bug re <text>` answers — almost always the one they are talking about.
func (s *bugStore) latestFrom(player string) (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.Items) - 1; i >= 0; i-- {
		if s.Items[i].Player == player {
			return s.Items[i].ID, true
		}
	}
	return 0, false
}

// snapshot renders the store for the /debug/bugs endpoint.
func (s *bugStore) snapshot() []bugReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]bugReport(nil), s.Items...)
}

func (s *bugStore) flushIfDirty() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.dirty || s.path == "" {
		return
	}
	data, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		log.Printf("bug store: %v", err)
		return
	}
	if !writeStore(s.path, data) {
		return // writeStore logs; keep dirty so the next flush retries
	}
	s.dirty = false
}

// fileBugReport captures the world around a player and files their report.
// Runs on the hub, which is the only place the world and the entity lists can
// be read consistently.
func (h *hub) fileBugReport(players map[int32]*tracked, t *tracked, text string) int {
	if len(text) > bugTextMax {
		text = text[:bugTextMax]
	}
	r := bugReport{
		At: time.Now().UTC().Format(time.RFC3339), Player: t.p.name, Text: text,
		Dim: t.dim, X: t.x, Y: t.y, Z: t.z, Yaw: t.yaw, Pitch: t.pitch,
		Gamemode: t.gamemode,
		Held:     itemRegistryName(heldStack(t).item),
		Offhand:  itemRegistryName(t.offhand.item),
	}
	ox, oy, oz := floorInt(t.x)-bugRegionXZ, floorInt(t.y)-bugRegionDown, floorInt(t.z)-bugRegionXZ
	sx, sy, sz := bugRegionXZ*2+1, bugRegionDown+bugRegionUp+1, bugRegionXZ*2+1
	r.Origin, r.Size = [3]int{ox, oy, oz}, [3]int{sx, sy, sz}
	r.Blocks = encodeRegion(h.worldFor(t.dim), r.Origin, r.Size)

	for _, m := range h.mobs {
		if m.dim != t.dim || m.dying > 0 {
			continue
		}
		if int(m.x) < ox || int(m.x) >= ox+sx || int(m.z) < oz || int(m.z) >= oz+sz {
			continue
		}
		r.Nearby = append(r.Nearby, fmt.Sprintf("%s at %.1f,%.1f,%.1f", entityRegistryName(m.etype), m.x, m.y, m.z))
	}
	id := h.bugs.add(r)
	h.bugs.flushIfDirty()
	log.Printf("bug #%d from %q at dim %d (%.0f,%.0f,%.0f): %s", id, t.p.name, t.dim, t.x, t.y, t.z, text)
	return id
}

// encodeRegion run-length encodes a cuboid by block NAME (plus the properties
// that are not the block's default), in YZX order — the same order the chunk
// sections use. Names rather than state ids so a report stays readable and
// survives a canonical-version move.
func encodeRegion(w interface {
	At(x, y, z int) uint32
}, origin, size [3]int) []string {
	if w == nil {
		return nil
	}
	var out []string
	run, count := "", 0
	flush := func() {
		if count > 0 {
			out = append(out, fmt.Sprintf("%dx%s", count, run))
		}
	}
	for dy := 0; dy < size[1]; dy++ {
		for dz := 0; dz < size[2]; dz++ {
			for dx := 0; dx < size[0]; dx++ {
				d := describeState(w.At(origin[0]+dx, origin[1]+dy, origin[2]+dz))
				if d == run {
					count++
					continue
				}
				flush()
				run, count = d, 1
			}
		}
	}
	flush()
	return out
}

// describeState is "name" or "name[prop=val,…]" with only the properties that
// differ from the block's own default — enough to rebuild it, short enough to
// read.
func describeState(state uint32) string {
	name, ok := worldgen.StateName(state)
	if !ok {
		return fmt.Sprintf("state:%d", state)
	}
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return name
	}
	def := worldgen.BlockID(name)
	var diff []string
	for _, p := range info.Props {
		v := worldgen.GetProperty(info, state, p.Name)
		if v != worldgen.GetProperty(info, def, p.Name) {
			diff = append(diff, p.Name+"="+v)
		}
	}
	if len(diff) == 0 {
		return name
	}
	return name + "[" + strings.Join(diff, ",") + "]"
}

// itemRegistryName is an item id's registry name ("" for an empty slot) — the
// reverse of itemByName, used only for reports so the cost does not matter.
func itemRegistryName(id int32) string {
	if id == 0 {
		return ""
	}
	for name, v := range itemByName {
		if v == id {
			return name
		}
	}
	return fmt.Sprintf("item:%d", id)
}

// cmdBug is `/bug <what happened>` to file a report with the world around the
// reporter attached, and `/bug re [#n] <what you want to add>` to say more
// about one already filed — an answer to a question, a correction, or "still
// happening after the fix". Without the second form the only way to add
// anything was to file a fresh report, which buried the thread it belonged to.
// Anybody may use either: a report is not a privilege.
func (s *Server) cmdBug(p *player, args []string) {
	if len(args) > 0 && strings.EqualFold(args[0], "re") {
		rest := args[1:]
		on := 0
		if len(rest) > 0 {
			if n, err := strconv.Atoi(strings.TrimPrefix(rest[0], "#")); err == nil && n > 0 {
				on, rest = n, rest[1:]
			}
		}
		text := strings.TrimSpace(strings.Join(rest, " "))
		if text == "" {
			p.tell("Usage: /bug re <what you want to add> — or /bug re #3 <…> to answer a particular one.")
			return
		}
		s.hub.post(evBug{eid: p.eid, text: text, reply: true, on: on})
		return
	}
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		p.tell("Usage: /bug <what went wrong>, or /bug re <more about the last one you filed>.")
		return
	}
	s.hub.post(evBug{eid: p.eid, text: text})
}

// stateFromDescription is describeState's inverse: "name" or
// "name[prop=val,…]" back to a block state. It is what makes a captured
// region replayable — scripts/bugrepro.py emits a fixture that calls it, so a
// reproduction starts from the player's actual build. Unknown names and
// properties are skipped rather than fatal: a report from a newer world should
// still rebuild as much as it can.
func stateFromDescription(d string) uint32 {
	name, props, _ := strings.Cut(d, "[")
	st := worldgen.BlockID(name)
	if st == 0 && name != "air" {
		return worldgen.Air
	}
	props = strings.TrimSuffix(props, "]")
	if props == "" {
		return st
	}
	info, ok := worldgen.InfoForState(st)
	if !ok {
		return st
	}
	for _, kv := range strings.Split(props, ",") {
		k, v, found := strings.Cut(kv, "=")
		if !found || !info.HasProperty(k) {
			continue
		}
		st = worldgen.SetProperty(info, st, k, v)
	}
	return st
}

// ---- replies -------------------------------------------------------------

// Replying to a report is half the loop. A player who reports something and
// hears nothing stops reporting, and the person who can answer is usually not
// at a keyboard when the report lands — so a reply that cannot wait for them
// to be online is not much of a reply. Messages queue against the player name
// and are delivered the moment they next join.

// Every chat line the engine sends passes through sanitizeNBT, which caps at
// 256 characters — so a reply longer than that used to be silently cut, and
// the caller was told it had been delivered. A message worth sending is worth
// sending whole: long replies are split across lines instead.
const (
	chatLineMax  = 256
	replyTotpMax = 1200 // a chat window, not an essay
)

// replyLines splits a reply into chat-sized lines, breaking on spaces so a
// line never ends mid-word. The first carries the full prefix; the rest are
// indented so they read as continuations.
func replyLines(prefix, text string) []string {
	var out []string
	for first := true; text != ""; first = false {
		p := prefix
		if !first {
			p = "  "
		}
		avail := chatLineMax - len(p)
		if len(text) <= avail {
			out = append(out, p+text)
			break
		}
		cut := strings.LastIndex(text[:avail], " ")
		if cut <= 0 {
			cut = avail // one enormous word: hard break rather than loop
		}
		out = append(out, p+strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	return out
}

// queueReply delivers a message to a player now if they are here, and holds it
// for their next join if they are not. bug > 0 also records it against that
// report, so the report list shows what was answered. Returns whether it was
// delivered and how many lines it took.
func (h *hub) queueReply(players map[int32]*tracked, name, text string, bug int) (delivered bool, lines int) {
	if len(text) > replyTotpMax {
		text = text[:replyTotpMax]
	}
	prefix := "[tachyne] "
	if bug > 0 {
		prefix = fmt.Sprintf("[tachyne] re bug #%d: ", bug)
		h.bugs.note(bug, text)
	}
	msgs := replyLines(prefix, text)
	for _, t := range players {
		if strings.EqualFold(t.p.name, name) {
			for _, m := range msgs {
				t.p.tell(m)
			}
			log.Printf("reply to %q (online, %d lines): %s", name, len(msgs), text)
			return true, len(msgs)
		}
	}
	for _, m := range msgs {
		h.bugs.queue(name, m)
	}
	log.Printf("reply to %q queued for their next join (%d lines): %s", name, len(msgs), text)
	return false, len(msgs)
}

// deliverQueuedReplies hands a joining player whatever was left for them.
func (h *hub) deliverQueuedReplies(t *tracked) {
	for _, msg := range h.bugs.takeQueued(t.p.name) {
		t.p.tell(msg)
	}
}

func (s *bugStore) note(id int, note string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.Items {
		if s.Items[i].ID == id {
			s.Items[i].Note = note
			s.dirty = true
			return
		}
	}
}

func (s *bugStore) queue(name, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Pending == nil {
		s.Pending = map[string][]string{}
	}
	key := strings.ToLower(name)
	if len(s.Pending[key]) >= 10 { // never let a queue grow without bound
		s.Pending[key] = s.Pending[key][1:]
	}
	s.Pending[key] = append(s.Pending[key], msg)
	s.dirty = true
}

func (s *bugStore) takeQueued(name string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(name)
	msgs := s.Pending[key]
	if len(msgs) == 0 {
		return nil
	}
	delete(s.Pending, key)
	s.dirty = true
	return msgs
}

// handleBugEvent files a report, or adds a reply to one already filed.
// A reply with no number answers the player's own most recent report, which
// is nearly always the one they mean; a number answers that one exactly.
func (h *hub) handleBugEvent(t *tracked, e evBug) {
	if e.on == 0 && !e.reply {
		id := h.fileBugReport(h.playersRef, t, e.text)
		t.p.tell(fmt.Sprintf("Filed as bug #%d, with the blocks around you. Thank you.", id))
		return
	}
	id := e.on
	if id == 0 {
		var ok bool
		if id, ok = h.bugs.latestFrom(t.p.name); !ok {
			t.p.tell("You have not filed a report yet — use /bug <what went wrong> first.")
			return
		}
	}
	text := e.text
	if len(text) > bugTextMax {
		text = text[:bugTextMax]
	}
	subject, ok := h.bugs.reply(id, t.p.name, text)
	if !ok {
		t.p.tell(fmt.Sprintf("There is no bug #%d.", id))
		return
	}
	h.bugs.flushIfDirty()
	log.Printf("bug #%d reply from %q: %s", id, t.p.name, text)
	if len(subject) > 60 {
		subject = subject[:57] + "…"
	}
	t.p.tell(fmt.Sprintf("Added to bug #%d (%s).", id, subject))
}
