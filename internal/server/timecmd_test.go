package server

import (
	"encoding/json"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// /time's 26.3 verbs through the dispatcher: a time or a time marker on the
// source's clock, add, pause/resume, rate, the queries, and `of <clock>`,
// with TimeCommand's refusals word for word.
func TestTimeClockVerbs(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	onHub(t, h, func() { h.rules.DoDaylight = false; h.dayTime.Store(0) }) // the clocks hold still
	for _, cmd := range []string{
		"time set 1000", "time set 1000", "time set day", "time set noon", "time set day",
		"time query time", "time query day", "time query minecraft:day repetition", "time query moon",
		"time add -30000", "time add 1d",
		"time pause", "time pause", "time resume", "time resume",
		"time rate 2", "time rate 2", "time rate 0", "time rate 1001",
		"time of the_end set 50", "time of minecraft:the_end query time", "time of the_end query day",
		"time of the_end set day", "time of nowhere query time", "time query nothing",
		"time of the_end query gametime", "time set -5", "time set 5x", "time query daytime",
	} {
		s.handleCommand(alice, cmd)
	}
	settle(t, h, logs, "T1")
	a := linesBetween(logs["alice"], "", "T1")
	for _, want := range []string{
		"Set minecraft:overworld to 1000 tick(s)",
		"Clock minecraft:overworld is already set to 1000 tick(s)",
		"Clock minecraft:overworld is already at time marker minecraft:day",
		"Set minecraft:overworld to time marker minecraft:noon",
		"Set minecraft:overworld to time marker minecraft:day", // forward: the next day, 25000
		"Clock minecraft:overworld is at 25000 tick(s)",
		"Timeline minecraft:day is at 1000 tick(s)",
		"Timeline minecraft:day has passed 1 repetition(s)",
		"Timeline minecraft:moon is at 25000 tick(s)",
		"Set minecraft:overworld to 0 tick(s)", // addTicks never goes below zero
		"Set minecraft:overworld to 24000 tick(s)",
		"Paused clock minecraft:overworld", "Clock minecraft:overworld is already paused",
		"Resumed clock minecraft:overworld", "Clock minecraft:overworld is already running",
		"Clock minecraft:overworld will now advance at 2.0x normal rate",
		"Clock minecraft:overworld is already advancing at 2.0x normal rate",
		"Float must not be less than 1.0E-5: found 0.0",
		"Float must not be more than 1000.0: found 1001.0",
		"Set minecraft:the_end to 50 tick(s)",
		"Clock minecraft:the_end is at 50 tick(s)",
		"Timeline minecraft:day is not valid for clock minecraft:the_end",
		"Time marker minecraft:day does not exist for clock minecraft:the_end",
		"Can't find element 'minecraft:nowhere' of type 'minecraft:world_clock'",
		"Can't find element 'minecraft:nothing' of type 'minecraft:timeline'",
		"Can't find element 'minecraft:gametime' of type 'minecraft:timeline'",
		"The tick count must not be less than 0: found -5",
		"Invalid unit",
		"Can't find element 'minecraft:daytime' of type 'minecraft:timeline'", // 1.21's query daytime is gone
	} {
		if !hasLine(a, want) {
			t.Errorf("no %q in %q", want, a)
		}
	}
	onHub(t, h, func() {
		if h.dayTime.Load() != 24000 || h.clocks[clockTheEnd].Total != 50 || h.clocks[clockOverworld].rate() != 2 {
			t.Errorf("clocks: overworld %d rate %v, end %d", h.dayTime.Load(), h.clocks[clockOverworld].rate(), h.clocks[clockTheEnd].Total)
		}
	})

	// The Nether has no default clock; query gametime needs none.
	onHub(t, h, func() { h.playersRef[alice.eid].dim = dimNether })
	s.handleCommand(alice, "time set day")
	s.handleCommand(alice, "time query gametime")
	s.handleCommand(alice, "time of overworld query time")
	settle(t, h, logs, "T2")
	a = linesBetween(logs["alice"], "T1", "T2")
	if !hasLine(a, "There is no default clock in dimension minecraft:the_nether") ||
		!hasPrefixLine(a, "The game time is ") || !hasLine(a, "Clock minecraft:overworld is at 24000 tick(s)") {
		t.Errorf("in the Nether: %q", a)
	}
}

// ServerClockManager.tick: each clock gains its rate in partial ticks while
// it runs and advance_time holds, a paused one stands still, and the clocks
// survive a save.
func TestClockRateAndPause(t *testing.T) {
	h := newTestHub(world.New(1))
	h.rules.DoDaylight = true
	h.dayTime.Store(100)
	h.clocks[clockOverworld].Rate = 0.5
	for range 5 {
		h.tickClocks()
	}
	if got := h.dayTime.Load(); got != 102 || h.clocks[clockOverworld].Partial != 0.5 {
		t.Fatalf("half rate: %d + %v after five ticks, want 102 + 0.5", got, h.clocks[clockOverworld].Partial)
	}
	if h.clocks[clockTheEnd].Total != 5 {
		t.Fatalf("the End's clock runs too: %d", h.clocks[clockTheEnd].Total)
	}
	h.clocks[clockTheEnd].Paused = true
	h.rules.DoDaylight = false
	h.tickClocks()
	if h.dayTime.Load() != 102 {
		t.Fatal("advance_time off: no clock moves")
	}
	h.rules.DoDaylight = true
	h.tickClocks()
	if h.clocks[clockTheEnd].Total != 5 || h.dayTime.Load() != 103 {
		t.Fatalf("paused End %d, overworld %d", h.clocks[clockTheEnd].Total, h.dayTime.Load())
	}

	h.packClocks()
	data, _ := json.Marshal(h.rules)
	h2 := newTestHub(world.New(1))
	if err := json.Unmarshal(data, &h2.rules); err != nil {
		t.Fatal(err)
	}
	dt := uint64(103)
	h2.rules.DayTime = &dt
	h2.dayTime.Store(dt)
	h2.unpackClocks()
	if h2.clocks[clockOverworld].rate() != 0.5 || !h2.clocks[clockTheEnd].Paused || h2.clocks[clockTheEnd].Total != 5 || h2.dayTime.Load() != 103 {
		t.Fatalf("restored clocks %+v, day %d", h2.clocks, h2.dayTime.Load())
	}
}

// The time argument: a float, then a unit, rounded.
func TestTimeArg(t *testing.T) {
	for _, tc := range []struct {
		in   string
		min  int64
		want int64
		msg  string
	}{
		{"100", 0, 100, ""}, {"1d", 0, 24000, ""}, {"2s", 0, 40, ""}, {"0.5d", 0, 12000, ""},
		{"3t", 0, 3, ""}, {"-20", -100, -20, ""}, {"-20", 0, 0, "The tick count must not be less than 0: found -20"},
		{"5x", 0, 0, "Invalid unit"}, {"x", 0, 0, "Expected float"},
	} {
		got, msg := timeArg(tc.in, tc.min)
		if got != tc.want || msg != tc.msg {
			t.Errorf("timeArg(%q) = %d, %q; want %d, %q", tc.in, got, msg, tc.want, tc.msg)
		}
	}
}
