package server

import "sort"

// Scheduled functions: vanilla's TimerQueue (the server's scheduled_events
// saved data). /schedule puts a function or a function tag on it for a
// game tick; each tick pops what is due and the function runner calls
// it. The queue rides settings.json.
//
// Vanilla saves each event's absolute trigger_time against a game time it
// also saves. The engine's game time starts again at every boot, so the
// save carries the game time it was written at, and the load moves every
// event by the difference: what was due in 40 ticks is still due in 40.

// schedCallback is a TimerCallback: a function (FunctionCallback) or a
// function tag (FunctionTagCallback).
type schedCallback struct {
	Tag bool
	ID  string
}

// schedEvent is one queued event.
type schedEvent struct {
	at  uint64
	seq uint64
	id  string // the schedule id: the function's id, or "#" and the tag's
	cb  schedCallback
}

// timerQueue is TimerQueue: events ordered by trigger time, then by the
// order they were scheduled in; one event per id and time.
type timerQueue struct {
	events []schedEvent
	seq    uint64
}

func (q *timerQueue) schedule(id string, at uint64, cb schedCallback) {
	for _, e := range q.events {
		if e.id == id && e.at == at {
			return
		}
	}
	q.seq++
	ev := schedEvent{at: at, seq: q.seq, id: id, cb: cb}
	i := sort.Search(len(q.events), func(i int) bool {
		a := q.events[i]
		return a.at > at || (a.at == at && a.seq > ev.seq)
	})
	q.events = append(q.events, schedEvent{})
	copy(q.events[i+1:], q.events[i:])
	q.events[i] = ev
}

// remove drops every event with the id and reports how many there were.
func (q *timerQueue) remove(id string) int {
	kept := q.events[:0]
	n := 0
	for _, e := range q.events {
		if e.id == id {
			n++
			continue
		}
		kept = append(kept, e)
	}
	q.events = kept
	return n
}

// popDue takes every event due at or before now, in order.
func (q *timerQueue) popDue(now uint64) []schedCallback {
	n := 0
	for n < len(q.events) && q.events[n].at <= now {
		n++
	}
	if n == 0 {
		return nil
	}
	out := make([]schedCallback, n)
	for i := 0; i < n; i++ {
		out[i] = q.events[i].cb
	}
	q.events = append(q.events[:0], q.events[n:]...)
	return out
}

// ids is getEventsIds: every id with an event queued.
func (q *timerQueue) ids() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range q.events {
		if !seen[e.id] {
			seen[e.id] = true
			out = append(out, e.id)
		}
	}
	sort.Strings(out)
	return out
}

// scheduleSave is the queue as settings.json keeps it.
type scheduleSave struct {
	GameTime uint64              `json:"gameTime"`
	Events   []scheduleSaveEvent `json:"events"`
}

type scheduleSaveEvent struct {
	TriggerTime uint64               `json:"trigger_time"`
	ID          string               `json:"id"`
	Callback    scheduleSaveCallback `json:"callback"`
}

type scheduleSaveCallback struct {
	Type string `json:"type"` // minecraft:function or minecraft:function_tag
	ID   string `json:"id"`
}

// packSchedule writes the queue into the rules for saving.
func (h *hub) packSchedule() {
	if len(h.sched.events) == 0 {
		h.rules.ScheduledEvents = nil
		return
	}
	sv := &scheduleSave{GameTime: h.tick.Load()}
	for _, e := range h.sched.events {
		typ := "minecraft:function"
		if e.cb.Tag {
			typ = "minecraft:function_tag"
		}
		sv.Events = append(sv.Events, scheduleSaveEvent{TriggerTime: e.at, ID: e.id, Callback: scheduleSaveCallback{Type: typ, ID: e.cb.ID}})
	}
	h.rules.ScheduledEvents = sv
}

// unpackSchedule rebuilds the queue from the loaded rules, moved onto
// this run's game time.
func (h *hub) unpackSchedule() {
	h.sched = timerQueue{}
	sv := h.rules.ScheduledEvents
	if sv == nil {
		return
	}
	now := h.tick.Load()
	for _, e := range sv.Events {
		var cb schedCallback
		switch e.Callback.Type {
		case "minecraft:function", "function":
			cb = schedCallback{ID: e.Callback.ID}
		case "minecraft:function_tag", "function_tag":
			cb = schedCallback{Tag: true, ID: e.Callback.ID}
		default:
			continue
		}
		at := now
		if e.TriggerTime > sv.GameTime {
			at = now + (e.TriggerTime - sv.GameTime)
		}
		h.sched.schedule(e.ID, at, cb)
	}
}
