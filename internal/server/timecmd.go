package server

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// /time (TimeCommand, 26.3): the world's clocks. Each clock (WorldClock:
// minecraft:overworld, minecraft:the_end) counts total ticks, advancing at
// its own rate while it is not paused and the advance_time rule holds
// (ServerClockManager). A dimension names its default clock — the Nether
// has none — and the bare verbs act on the source's; `of <clock>` names one.
//
//	/time [of <clock>] set <time>|<timemarker>
//	/time [of <clock>] add <time>
//	/time [of <clock>] pause|resume
//	/time [of <clock>] rate <rate>
//	/time [of <clock>] query time|<timeline> [repetition]
//	/time query gametime
//
// A time marker (the day timeline's day, noon, night, midnight, …) moves the
// clock FORWARD to its next occurrence; a number sets the total outright. A
// timeline reads its clock modulo its period (the moon's 192000, the day's
// 24000), and its repetitions are the periods passed.
//
// The overworld clock's total is the hub's dayTime, which everything that
// asks the time of day reads; the rest of each clock's state is clockRun,
// saved beside it in the settings.

const (
	clockOverworld = iota
	clockTheEnd
	numClocks
)

// clockKeys are the WorldClock registry keys, by clock index.
var clockKeys = [numClocks]string{"minecraft:overworld", "minecraft:the_end"}

// TimeCommand's rate bounds: FloatArgumentType.floatArg(1.0E-5F, 1000.0F).
const (
	timeRateMin = 1e-5
	timeRateMax = 1000
)

// clockRun is ServerClockInstance's state: the total (kept here for every
// clock but the overworld's, whose total is the hub's dayTime), the partial
// tick a fractional rate carries, the rate and whether it is paused.
type clockRun struct {
	Total   uint64  `json:"total_ticks"`
	Partial float32 `json:"partial_tick,omitempty"`
	Rate    float32 `json:"rate,omitempty"` // 0 = the default 1 (ClockState's optional rate)
	Paused  bool    `json:"paused,omitempty"`
}

func (r *clockRun) rate() float32 {
	if r.Rate <= 0 {
		return 1
	}
	return r.Rate
}

// clockMarker is a ClockTimeMarker: a tick within its timeline's period.
type clockMarker struct{ ticks, period int64 }

// occursAt is ClockTimeMarker.occursAt.
func (k clockMarker) occursAt(total int64) bool {
	if k.period == 0 {
		return k.ticks == total
	}
	return k.ticks == total%k.period
}

// moveTo is ClockTimeMarker.resolveTimeToMoveTo: the next time it comes round.
func (k clockMarker) moveTo(total int64) int64 {
	if k.period == 0 {
		return k.ticks
	}
	d := k.ticks - total%k.period
	if d <= 0 {
		d += k.period
	}
	return total + d
}

// clockMarkers are each clock's time markers, as the timelines register
// them (the day timeline's six, all on the overworld clock).
var clockMarkers = [numClocks]map[string]clockMarker{
	clockOverworld: {
		"minecraft:day": {1000, 24000}, "minecraft:noon": {6000, 24000}, "minecraft:night": {13000, 24000},
		"minecraft:midnight": {18000, 24000}, "minecraft:roll_village_siege": {18000, 24000},
		"minecraft:wake_up_from_sleep": {0, 24000},
	},
}

// timeline is the clock and period of a Timeline (0 = no period).
type timeline struct {
	clock  int
	period int64
}

// timelines is the timeline registry (data/minecraft/timeline).
var timelines = map[string]timeline{
	"minecraft:day":               {clockOverworld, 24000},
	"minecraft:early_game":        {clockOverworld, 0},
	"minecraft:moon":              {clockOverworld, 192000},
	"minecraft:villager_schedule": {clockOverworld, 24000},
}

// defaultClock is DimensionType.defaultClock: the overworld's and the End's
// own clocks; the Nether has none.
func defaultClock(dim int) (int, bool) {
	switch dim {
	case dimOverworld:
		return clockOverworld, true
	case dimEnd:
		return clockTheEnd, true
	}
	return 0, false
}

func clockIndex(key string) int {
	for i, k := range clockKeys {
		if k == key {
			return i
		}
	}
	return -1
}

// clockTotal is a clock's total ticks.
func (h *hub) clockTotal(c int) uint64 {
	if c == clockOverworld {
		return h.dayTime.Load()
	}
	return h.clocks[c].Total
}

// setClockTotal is setTotalTicks: the total, and no partial tick carried.
// The overworld's goes through setDayTime (the plugin TimeSetEvent).
func (h *hub) setClockTotal(players map[int32]*tracked, c int, v uint64) {
	h.clocks[c].Partial = 0
	if c == clockOverworld {
		h.setDayTime(v)
		h.broadcastTime(players)
		return
	}
	h.clocks[c].Total = v
	h.broadcastTime(players)
}

// broadcastTime is modifyClock's ClientboundSetTimePacket: everyone is told
// at once rather than at the next once-a-second send.
func (h *hub) broadcastTime(players map[int32]*tracked) {
	body := h.timeFrame()
	for _, t := range players {
		t.p.trySendEv(body)
	}
}

// timeFrame is ServerClockManager.createFullSyncPacket: the game time and
// every clock's packNetworkState — its full total (the moon's phase is the
// total over a 192000-tick period), partial tick and rate, which is 0 while
// the clock is paused or the advance_time rule is off. It is also kept for
// the sessions that join between syncs.
func (h *hub) timeFrame() attachproto.Time {
	day := h.dayTime.Load()
	f := attachproto.Time{Age: int64(h.tick.Load()), Time: int64(day % dayLengthTicks)}
	f.Clocks = make([]attachproto.Clock, numClocks)
	for c := range h.clocks {
		r := &h.clocks[c]
		rate := r.rate()
		if r.Paused || !h.rules.DoDaylight {
			rate = 0
		}
		f.Clocks[c] = attachproto.Clock{ID: clockNetID[c], Total: int64(h.clockTotal(c)), Partial: r.Partial, Rate: rate}
	}
	snap := f
	h.lastTime.Store(&snap)
	return f
}

// clockNetID is each clock's id in the world_clock registry the gateways send.
var clockNetID = [numClocks]int32{clockOverworld: attachproto.ClockOverworld, clockTheEnd: attachproto.ClockTheEnd}

// joinTime is the clock sync a joining session starts from (PlayerList
// sendLevelInfo's full sync): the last one the hub built, or the bare day
// time before the first.
func (h *hub) joinTime() attachproto.Time {
	if f := h.lastTime.Load(); f != nil {
		return *f
	}
	return attachproto.Time{Time: int64(h.dayTime.Load())}
}

// tickClocks is ServerClockManager.tick: while advance_time holds, every
// clock that is not paused gains its rate in partial ticks and takes the
// whole ones. It returns the overworld total.
func (h *hub) tickClocks() uint64 {
	if h.rules.DoDaylight {
		for c := range h.clocks {
			r := &h.clocks[c]
			if r.Paused {
				continue
			}
			r.Partial += r.rate()
			full := float32(math.Floor(float64(r.Partial)))
			r.Partial -= full
			if full <= 0 {
				continue
			}
			if c == clockOverworld {
				h.dayTime.Add(uint64(full))
			} else {
				r.Total += uint64(full)
			}
		}
	}
	return h.dayTime.Load()
}

// packClocks writes the clocks into the saved settings.
func (h *hub) packClocks() {
	h.rules.Clocks = make(map[string]*clockRun, numClocks)
	for c := range h.clocks {
		r := h.clocks[c]
		r.Total = h.clockTotal(c)
		h.rules.Clocks[clockKeys[c]] = &r
	}
}

// unpackClocks restores them; the overworld's total stays the saved
// dayTime's when there is one.
func (h *hub) unpackClocks() {
	for c, key := range clockKeys {
		r := h.rules.Clocks[key]
		if r == nil {
			continue
		}
		h.clocks[c] = *r
		if c == clockOverworld {
			if h.rules.DayTime == nil {
				h.dayTime.Store(r.Total)
			}
			h.clocks[c].Total = 0
		}
	}
}

func (s *Server) cmdTime(p *player, args []string) {
	// TimeCommand requires LEVEL_GAMEMASTERS.
	if !s.isOp(p.name) {
		p.tell("You don't have permission to change the time.")
		return
	}
	s.onHub(func(players map[int32]*tracked) { s.hub.runTime(players, p, args) })
}

const timeUsage = "Usage: /time [of <clock>] set <time|timemarker> | add <time> | pause | resume | rate <rate> | " +
	"query time | query <timeline> [repetition]; /time query gametime"

// runTime is TimeCommand on the hub, which owns the clocks.
func (h *hub) runTime(players map[int32]*tracked, p *player, args []string) {
	fail := func(msg string) { cmdFail(p, msg) }
	okTell := func(msg string) { h.cmdSuccess(players, p, msg, true) }
	info := func(msg string) { h.cmdSuccess(players, p, msg, false) }
	clock, named := -1, false
	if len(args) > 0 && args[0] == "of" {
		if len(args) < 3 {
			fail(timeUsage)
			return
		}
		id, ok := parseResourceID(args[1])
		if !ok {
			fail("Invalid ID")
			return
		}
		if clock = clockIndex(id); clock < 0 {
			fail(fmt.Sprintf("Can't find element '%s' of type 'minecraft:world_clock'", id))
			return
		}
		named, args = true, args[2:]
	}
	if len(args) == 0 {
		fail(timeUsage)
		return
	}
	verb, rest := args[0], args[1:]
	if verb == "query" && !named && len(rest) == 1 && rest[0] == "gametime" {
		info(fmt.Sprintf("The game time is %d tick(s)", h.tick.Load())) // needs no clock
		return
	}
	switch verb { // the grammar first: a malformed line never reaches the clock
	case "set", "add", "rate":
		if len(rest) != 1 {
			fail(timeUsage)
			return
		}
	case "pause", "resume":
		if len(rest) != 0 {
			fail(timeUsage)
			return
		}
	case "query":
		if len(rest) == 0 || len(rest) > 2 || (len(rest) == 2 && rest[1] != "repetition") || (rest[0] == "time" && len(rest) != 1) {
			fail(timeUsage)
			return
		}
	default:
		fail(timeUsage)
		return
	}
	if !named {
		dim := dimOverworld
		if t := players[p.eid]; t != nil {
			dim = t.dim
		}
		c, ok := defaultClock(dim)
		if !ok {
			fail(fmt.Sprintf("There is no default clock in dimension %s", dimType(dim).Key))
			return
		}
		clock = c
	}
	key := clockKeys[clock]
	run := &h.clocks[clock]
	switch verb {
	case "set":
		if timeWordIsNumber(rest[0]) {
			n, msg := timeArg(rest[0], 0)
			if msg != "" {
				fail(msg)
				return
			}
			if n > math.MaxInt32 {
				fail(fmt.Sprintf("Integer must not be more than %d, found %d", math.MaxInt32, n))
				return
			}
			if h.clockTotal(clock) == uint64(n) {
				fail(fmt.Sprintf("Clock %s is already set to %d tick(s)", key, n))
				return
			}
			h.setClockTotal(players, clock, uint64(n))
			h.saveRules()
			okTell(fmt.Sprintf("Set %s to %d tick(s)", key, n))
			return
		}
		id, ok := parseResourceID(rest[0])
		if !ok {
			fail("Invalid ID")
			return
		}
		mk, ok := clockMarkers[clock][id]
		if !ok {
			fail(fmt.Sprintf("Time marker %s does not exist for clock %s", id, key))
			return
		}
		total := int64(h.clockTotal(clock))
		if mk.occursAt(total) {
			fail(fmt.Sprintf("Clock %s is already at time marker %s", key, id))
			return
		}
		h.setClockTotal(players, clock, uint64(mk.moveTo(total)))
		h.saveRules()
		okTell(fmt.Sprintf("Set %s to time marker %s", key, id))
	case "add":
		n, msg := timeArg(rest[0], math.MinInt32)
		if msg != "" {
			fail(msg)
			return
		}
		if n > math.MaxInt32 {
			fail(fmt.Sprintf("Integer must not be more than %d, found %d", math.MaxInt32, n))
			return
		}
		// addTicks: never below zero, and the partial tick carries on.
		total := max(int64(h.clockTotal(clock))+n, 0)
		partial := run.Partial
		h.setClockTotal(players, clock, uint64(total))
		run.Partial = partial
		h.saveRules()
		okTell(fmt.Sprintf("Set %s to %d tick(s)", key, total))
	case "pause", "resume":
		paused := verb == "pause"
		if run.Paused == paused {
			if paused {
				fail(fmt.Sprintf("Clock %s is already paused", key))
			} else {
				fail(fmt.Sprintf("Clock %s is already running", key))
			}
			return
		}
		run.Paused = paused
		h.saveRules()
		h.broadcastTime(players)
		if paused {
			okTell(fmt.Sprintf("Paused clock %s", key))
		} else {
			okTell(fmt.Sprintf("Resumed clock %s", key))
		}
	case "rate":
		f, err := strconv.ParseFloat(rest[0], 32)
		if err != nil {
			fail(fmt.Sprintf("Invalid float '%s'", rest[0]))
			return
		}
		rate := float32(f)
		switch {
		case rate < timeRateMin:
			fail(fmt.Sprintf("Float must not be less than %s: found %s", jFloat(timeRateMin), jFloat(rate)))
			return
		case rate > timeRateMax:
			fail(fmt.Sprintf("Float must not be more than %s: found %s", jFloat(timeRateMax), jFloat(rate)))
			return
		}
		if run.rate() == rate {
			fail(fmt.Sprintf("Clock %s is already advancing at %sx normal rate", key, jFloat(rate)))
			return
		}
		run.Rate = rate
		h.saveRules()
		h.broadcastTime(players)
		okTell(fmt.Sprintf("Clock %s will now advance at %sx normal rate", key, jFloat(rate)))
	case "query":
		if rest[0] == "time" {
			info(fmt.Sprintf("Clock %s is at %d tick(s)", key, h.clockTotal(clock)))
			return
		}
		id, ok := parseResourceID(rest[0])
		if !ok {
			fail("Invalid ID")
			return
		}
		tl, ok := timelines[id]
		if !ok {
			fail(fmt.Sprintf("Can't find element '%s' of type 'minecraft:timeline'", id))
			return
		}
		if tl.clock != clock {
			fail(fmt.Sprintf("Timeline %s is not valid for clock %s", id, key))
			return
		}
		total := int64(h.clockTotal(clock))
		if len(rest) == 2 { // Timeline.getPeriodCount
			reps := int64(0)
			if tl.period > 0 {
				reps = total / tl.period
			}
			info(fmt.Sprintf("Timeline %s has passed %d repetition(s)", id, reps))
			return
		}
		cur := total // Timeline.getCurrentTicks
		if tl.period > 0 {
			cur = total % tl.period
		}
		info(fmt.Sprintf("Timeline %s is at %d tick(s)", id, cur))
	}
}

// timeArg is TimeArgument: a float, then a unit — d (a day, 24000 ticks),
// s (a second, 20), t or nothing (a tick) — rounded to whole ticks and held
// to a minimum. The message is the parse error, "" when it parsed.
func timeArg(s string, minTicks int64) (int64, string) {
	i := 0
	for i < len(s) && (s[i] >= '0' && s[i] <= '9' || s[i] == '.' || s[i] == '-') {
		i++
	}
	num, unit := s[:i], s[i:]
	if num == "" {
		return 0, "Expected float"
	}
	f, err := strconv.ParseFloat(num, 32)
	if err != nil {
		return 0, fmt.Sprintf("Invalid float '%s'", num)
	}
	factor, ok := map[string]float32{"d": dayLengthTicks, "s": 20, "t": 1, "": 1}[unit]
	if !ok {
		return 0, "Invalid unit"
	}
	ticks := int64(math.Floor(float64(float32(f)*factor) + 0.5)) // Math.round(float)
	if ticks < minTicks {
		return 0, fmt.Sprintf("The tick count must not be less than %d: found %d", minTicks, ticks)
	}
	return ticks, ""
}

// timeWordIsNumber tells set's two arguments apart: a time starts as a
// number does; anything else is a time marker's id.
func timeWordIsNumber(s string) bool {
	return s != "" && strings.ContainsRune("-.0123456789", rune(s[0]))
}
