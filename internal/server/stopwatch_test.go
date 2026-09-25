package server

import (
	"strings"
	"testing"
	"time"
)

// /stopwatch through the dispatcher: create, the duplicate refusal, a query
// that has counted real time, restart, remove and the missing-id line; and
// the saved form resumes from what was counted rather than from the clock.
func TestStopwatchCommand(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	s.handleCommand(alice, "stopwatch create race")
	s.handleCommand(alice, "stopwatch create minecraft:race")
	settle(t, h, logs, "S1")
	time.Sleep(60 * time.Millisecond)
	s.handleCommand(alice, "stopwatch query race")
	s.handleCommand(alice, "stopwatch restart race")
	settle(t, h, logs, "S2")
	a := linesBetween(logs["alice"], "", "S2")
	for _, want := range []string{"Created stopwatch 'minecraft:race'", "Stopwatch 'minecraft:race' already exists", "Restarted stopwatch 'minecraft:race'"} {
		if !hasLine(a, want) {
			t.Errorf("no %q in %q", want, a)
		}
	}
	if !hasPrefixLine(a, "Stopwatch 'minecraft:race' has run for 0.") || hasLine(a, "Stopwatch 'minecraft:race' has run for 0.0s") {
		t.Errorf("the query did not count the time: %q", a)
	}
	onHub(t, h, func() {
		h.stopwatches["minecraft:race"] = stopwatchRun{created: time.Now().UnixMilli() - 5000, acc: 2000}
		h.packStopwatches()
		if ms := h.rules.Stopwatches["minecraft:race"]; ms < 7000 || ms > 8000 {
			t.Errorf("packed %d ms, want about 7000", ms)
		}
		h.stopwatches = nil // a restart: unpack from the saved form
	})
	s.handleCommand(alice, "stopwatch query race")
	s.handleCommand(alice, "stopwatch remove race")
	s.handleCommand(alice, "stopwatch query race")
	settle(t, h, logs, "S3")
	a = linesBetween(logs["alice"], "S2", "S3")
	if !hasPrefixLine(a, "Stopwatch 'minecraft:race' has run for 7.") {
		t.Errorf("the reloaded stopwatch did not resume at 7s: %q", a)
	}
	if !hasLine(a, "Removed stopwatch 'minecraft:race'") || !hasLine(a, "Stopwatch 'minecraft:race' does not exist") {
		t.Errorf("remove: %q", strings.Join(a, " | "))
	}
}
