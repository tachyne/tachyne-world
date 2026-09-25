package server

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// /tick (TickCommand) and the server's TickRateManager: the target rate,
// freezing the game, stepping a frozen game, and sprinting. A frozen game
// runs no simulation — no time, no weather, no mobs, no block ticks —
// while players still move and act; every client is told the state
// (ticking_state) so its own prediction runs at the same pace.

const defaultTickRate = 20

// tickState is the hub's TickRateManager.
type tickState struct {
	rate         float32 // target ticks per second
	frozen       bool
	stepLeft     int
	sprintLeft   int
	sprintTicks  int
	sprintStart  time.Time
	sprintCaller *player
}

func (s *tickState) interval() time.Duration {
	if s.sprintLeft > 0 {
		return time.Millisecond // as fast as the loop can run
	}
	r := s.rate
	if r <= 0 {
		r = defaultTickRate
	}
	return time.Duration(float64(time.Second) / float64(r))
}

// runsNormally is TickRateManager.runsNormally: not frozen, or stepping.
func (s *tickState) runsNormally() bool { return !s.frozen || s.stepLeft > 0 }

// tickGate runs at the top of every loop tick; false means this tick is
// skipped (the game is frozen and not stepping). It advances a step or a
// sprint and tells everyone when a step count changes.
func (h *hub) tickGate(players map[int32]*tracked) bool {
	ts := &h.ticks
	if !ts.runsNormally() {
		h.lastTick.Store(time.Now().UnixNano()) // frozen is not wedged
		return false
	}
	if ts.stepLeft > 0 {
		ts.stepLeft--
		h.toAll(players, attachproto.TickingStep{Steps: int32(ts.stepLeft)})
	}
	if ts.sprintLeft > 0 {
		ts.sprintTicks++
		if ts.sprintLeft--; ts.sprintLeft == 0 {
			h.finishSprint(players)
		}
	}
	return true
}

// tickingStateEv is what a client is told on join and on every change.
func (h *hub) tickingStateEv() attachproto.TickingState {
	r := h.ticks.rate
	if r <= 0 {
		r = defaultTickRate
	}
	return attachproto.TickingState{Rate: r, Frozen: h.ticks.frozen}
}

func (h *hub) resetTicker() {
	if h.ticker != nil {
		h.ticker.Reset(h.ticks.interval())
	}
}

func (h *hub) finishSprint(players map[int32]*tracked) {
	ts := &h.ticks
	d := time.Since(ts.sprintStart)
	n := max(ts.sprintTicks, 1)
	tps := float64(n) / math.Max(d.Seconds(), 1e-9)
	mspt := float64(d.Milliseconds()) / float64(n)
	if ts.sprintCaller != nil {
		h.cmdSuccess(players, ts.sprintCaller, fmt.Sprintf("Sprint completed with %s ticks per second, or %s ms per tick",
			strconv.FormatFloat(math.Round(tps), 'f', -1, 64), fmt.Sprintf("%.2f", mspt)), true)
	}
	ts.sprintTicks, ts.sprintCaller = 0, nil
	h.resetTicker()
}

func (s *Server) cmdTick(p *player, args []string) {
	if !s.isOp(p.name) { // TickCommand: LEVEL_ADMINS
		p.tell("You don't have permission.")
		return
	}
	s.onHub(func(players map[int32]*tracked) {
		if msg := s.hub.tickCommand(players, p, args); msg != "" {
			cmdFail(p, msg)
		}
	})
}

const tickUsage = "Usage: /tick query | rate <rate> | freeze | unfreeze | step [<time>|stop] | sprint <time>|stop"

func (h *hub) tickCommand(players map[int32]*tracked, p *player, args []string) string {
	ts := &h.ticks
	ok := func(msg string) { h.cmdSuccess(players, p, msg, true) }
	if len(args) == 0 {
		return tickUsage
	}
	switch strings.ToLower(args[0]) {
	case "query":
		status := "The game is running normally"
		switch {
		case ts.sprintLeft > 0:
			status = "The game is sprinting"
		case ts.frozen:
			status = "The game is frozen"
		case h.tickStats.percentile(0.5) > ts.interval():
			status = "The game is running, but can't keep up with the target tick rate"
		}
		ok(status)
		avg := float64(h.tickStats.percentile(0.5).Microseconds()) / 1000
		target := 1000 / float64(max(ts.rate, 1))
		if ts.sprintLeft > 0 {
			ok(fmt.Sprintf("Target tick rate: %s per second (ignored, reference only).\nAverage time per tick: %.1fms", fmtRate(ts.rate), avg))
		} else {
			ok(fmt.Sprintf("Target tick rate: %s per second.\nAverage time per tick: %.1fms (Target: %.1fms)", fmtRate(ts.rate), avg, target))
		}
		ms := func(q float64) string {
			return fmt.Sprintf("%.1f", float64(h.tickStats.percentile(q).Microseconds())/1000)
		}
		ok(fmt.Sprintf("Percentiles: P50: %sms P95: %sms P99: %sms. Sample: %d", ms(0.5), ms(0.95), ms(0.99), min(h.tickStats.n, len(h.tickStats.ring))))
	case "rate":
		if len(args) != 2 {
			return tickUsage
		}
		r, err := strconv.ParseFloat(args[1], 64)
		if err != nil || r < 1 || r > 10000 { // FloatArgumentType.floatArg(1.0F, 10000.0F)
			return "Float must be between 1.0 and 10000.0, found " + args[1]
		}
		ts.rate = float32(r)
		h.resetTicker()
		h.toAll(players, h.tickingStateEv())
		ok("Set the target tick rate to " + fmtRate(ts.rate) + " per second")
	case "freeze", "unfreeze":
		freeze := strings.EqualFold(args[0], "freeze")
		if freeze && ts.sprintLeft > 0 {
			ts.sprintLeft = 0 // stopSprinting
			h.finishSprint(players)
		}
		ts.frozen, ts.stepLeft = freeze, 0
		h.toAll(players, h.tickingStateEv())
		h.toAll(players, attachproto.TickingStep{Steps: 0})
		if freeze {
			ok("The game is frozen")
		} else {
			ok("The game is running normally")
		}
	case "step":
		if len(args) == 2 && strings.EqualFold(args[1], "stop") {
			if ts.stepLeft == 0 {
				return "No tick step in progress"
			}
			ts.stepLeft = 0
			h.toAll(players, attachproto.TickingStep{Steps: 0})
			ok("Interrupted the current tick step")
			return ""
		}
		n := 1
		if len(args) == 2 {
			v, good := parseTickTime(args[1])
			if !good || v < 1 {
				return "Invalid time: " + args[1]
			}
			n = v
		}
		if !ts.frozen {
			return "Unable to step the game - the game must be frozen first"
		}
		ts.stepLeft = n
		h.toAll(players, attachproto.TickingStep{Steps: int32(n)})
		ok(fmt.Sprintf("Stepping %d tick(s)", n))
	case "sprint":
		if len(args) != 2 {
			return tickUsage
		}
		if strings.EqualFold(args[1], "stop") {
			if ts.sprintLeft == 0 {
				return "No tick sprint in progress"
			}
			ts.sprintLeft = 0
			h.finishSprint(players)
			ok("Interrupted the current tick sprint")
			return ""
		}
		v, good := parseTickTime(args[1])
		if !good || v < 1 {
			return "Invalid time: " + args[1]
		}
		if ts.sprintLeft > 0 {
			ok("Interrupted the current tick sprint")
		}
		ts.sprintLeft, ts.sprintTicks, ts.sprintStart, ts.sprintCaller = v, 0, time.Now(), p
		h.resetTicker()
	default:
		return tickUsage
	}
	return ""
}

// parseTickTime is TimeArgument: a number of ticks, or with a unit
// (t, s, d).
func parseTickTime(s string) (int, bool) {
	mul := 1.0
	switch {
	case strings.HasSuffix(s, "t"):
		s = strings.TrimSuffix(s, "t")
	case strings.HasSuffix(s, "s"):
		s, mul = strings.TrimSuffix(s, "s"), 20
	case strings.HasSuffix(s, "d"):
		s, mul = strings.TrimSuffix(s, "d"), 24000
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0, false
	}
	return int(math.Round(f * mul)), true
}

// fmtRate prints a rate as vanilla's %s of a float does ("20.0").
func fmtRate(r float32) string {
	if r <= 0 {
		r = defaultTickRate
	}
	return jDouble(float64(r))
}

// toAll sends one event to every player.
func (h *hub) toAll(players map[int32]*tracked, ev any) {
	for _, t := range players {
		t.p.trySendEv(ev)
	}
}
