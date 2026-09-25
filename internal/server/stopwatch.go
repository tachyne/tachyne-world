package server

import (
	"fmt"
	"sort"
	"strconv"
	"time"
)

// /stopwatch create|query|restart|remove <id> (StopwatchCommand, 26.2):
// named real-time stopwatches (Stopwatches). Each counts wall-clock
// milliseconds from its creation; the world keeps what each had counted when
// it was saved, and a restart resumes from there, so the time the server was
// down is not counted.

// stopwatchRun is Stopwatch: when this run started and what earlier runs
// (before a restart) had counted.
type stopwatchRun struct {
	created, acc int64 // unix millis; milliseconds
}

func (r stopwatchRun) elapsedMs(now int64) int64 { return r.acc + now - r.created }

type evStopwatch struct {
	by    int32
	op    string // create, query, restart, remove
	id    string
	scale float64
}

func (evStopwatch) isHubEvent() {}

func (s *Server) cmdStopwatch(p *player, args []string) {
	if !s.isOp(p.name) { // LEVEL_GAMEMASTERS
		p.tell("You don't have permission.")
		return
	}
	usage := "Usage: /stopwatch create|query|restart|remove <id> (query takes a [<scale>])"
	if len(args) < 2 || len(args) > 3 {
		p.tell(usage)
		return
	}
	e := evStopwatch{by: p.eid, op: args[0], scale: 1}
	switch e.op {
	case "create", "restart", "remove":
		if len(args) != 2 {
			p.tell(usage)
			return
		}
	case "query":
		if len(args) == 3 {
			v, err := strconv.ParseFloat(args[2], 64)
			if err != nil {
				p.tell(fmt.Sprintf("Invalid double '%s'", args[2]))
				return
			}
			e.scale = v
		}
	default:
		p.tell(usage)
		return
	}
	id, ok := parseResourceID(args[1])
	if !ok {
		p.tell(fmt.Sprintf("Invalid identifier '%s'", args[1]))
		return
	}
	e.id = id
	s.hub.post(e)
}

// applyStopwatch runs /stopwatch on the hub; it returns the command's result.
func (h *hub) applyStopwatch(players map[int32]*tracked, e evStopwatch) int {
	tell := cmdTeller(players, e.by)
	okTell := h.cmdOK(players, e.by) // every form is sendSuccess(…, true)
	sw := h.stopwatchesLive()
	now := time.Now().UnixMilli()
	run, exists := sw[e.id]
	if e.op != "create" && !exists {
		tell(fmt.Sprintf("Stopwatch '%s' does not exist", e.id))
		return 0
	}
	switch e.op {
	case "create":
		if exists {
			tell(fmt.Sprintf("Stopwatch '%s' already exists", e.id))
			return 0
		}
		sw[e.id] = stopwatchRun{created: now}
		h.saveRules()
		okTell(fmt.Sprintf("Created stopwatch '%s'", e.id))
	case "query":
		secs := float64(run.elapsedMs(now)) / 1000
		okTell(fmt.Sprintf("Stopwatch '%s' has run for %ss", e.id, jDouble(secs)))
		return int(secs * e.scale)
	case "restart":
		sw[e.id] = stopwatchRun{created: now}
		h.saveRules()
		okTell(fmt.Sprintf("Restarted stopwatch '%s'", e.id))
	case "remove":
		delete(sw, e.id)
		h.saveRules()
		okTell(fmt.Sprintf("Removed stopwatch '%s'", e.id))
	}
	return 1
}

// stopwatchesLive is the running set, unpacked from the saved one on first
// use (Stopwatches.unpack: every saved stopwatch resumes now).
func (h *hub) stopwatchesLive() map[string]stopwatchRun {
	if h.stopwatches == nil {
		now := time.Now().UnixMilli()
		h.stopwatches = make(map[string]stopwatchRun, len(h.rules.Stopwatches))
		for id, acc := range h.rules.Stopwatches {
			h.stopwatches[id] = stopwatchRun{created: now, acc: acc}
		}
	}
	return h.stopwatches
}

// packStopwatches is Stopwatches.pack, run as the settings are saved.
func (h *hub) packStopwatches() {
	if h.stopwatches == nil {
		return // never unpacked: what was loaded is still current
	}
	now := time.Now().UnixMilli()
	ids := make([]string, 0, len(h.stopwatches))
	for id := range h.stopwatches {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	h.rules.Stopwatches = nil
	if len(ids) > 0 {
		h.rules.Stopwatches = make(map[string]int64, len(ids))
	}
	for _, id := range ids {
		h.rules.Stopwatches[id] = h.stopwatches[id].elapsedMs(now)
	}
}
