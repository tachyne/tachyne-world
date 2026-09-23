package server

import (
	"testing"
	"time"
)

// A slow tick names the phases that took its time, longest first, and
// leaves out the ones that took next to nothing.
func TestTickPhasesNameTheSlowOnes(t *testing.T) {
	var p tickPhases
	p.start(time.Now())
	p.d[phaseBlockUpdates] = 812 * time.Millisecond
	p.d[phaseMobs] = 90 * time.Millisecond
	p.d[phaseSpawning] = 40 * time.Millisecond
	p.d[phaseSaving] = 20 * time.Millisecond
	p.d[phaseClock] = 200 * time.Microsecond
	if got, want := p.top(), "block updates 812ms, mobs 90ms, spawning 40ms"; got != want {
		t.Fatalf("top = %q, want %q", got, want)
	}
	p.start(time.Now())
	if got := p.top(); got != "" {
		t.Fatalf("a fresh tick names %q", got)
	}
	// A lap charges the time since the last one to its phase.
	p.start(time.Now().Add(-5 * time.Millisecond))
	p.lap(phaseMobs)
	if p.d[phaseMobs] < 5*time.Millisecond {
		t.Fatalf("lap charged %s, want at least 5ms", p.d[phaseMobs])
	}
}
