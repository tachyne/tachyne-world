package server

import (
	"encoding/json"
	"fmt"
	"log"
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
}

// evBug carries a filed report to the hub, which is where the world and the
// entity lists can be read consistently.
type evBug struct {
	eid  int32
	text string
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

// cmdBug is `/bug <what happened>`: file a report with the world around the
// reporter attached. Anybody may use it — a report is not a privilege.
func (s *Server) cmdBug(p *player, args []string) {
	text := strings.TrimSpace(strings.Join(args, " "))
	if text == "" {
		p.tell("Usage: /bug <what went wrong> — say what you expected and what happened.")
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

const bugReplyMax = 300

// queueReply delivers a message to a player now if they are here, and holds it
// for their next join if they are not. bug > 0 also records it against that
// report, so the report list shows what was answered.
func (h *hub) queueReply(players map[int32]*tracked, name, text string, bug int) (delivered bool) {
	if len(text) > bugReplyMax {
		text = text[:bugReplyMax]
	}
	msg := "[tachyne] " + text
	if bug > 0 {
		msg = fmt.Sprintf("[tachyne] re bug #%d: %s", bug, text)
		h.bugs.note(bug, text)
	}
	for _, t := range players {
		if strings.EqualFold(t.p.name, name) {
			t.p.tell(msg)
			log.Printf("reply to %q (online): %s", name, text)
			return true
		}
	}
	h.bugs.queue(name, msg)
	log.Printf("reply to %q queued for their next join: %s", name, text)
	return false
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
