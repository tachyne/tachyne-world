package server

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The hub's tick is timed by phase, so a slow tick says what it spent its
// time on rather than only that it was slow. A lap is one time.Now at a
// phase boundary — about a microsecond a tick in all.
const (
	phaseClock = iota
	phaseScheduled
	phaseBlockUpdates
	phaseMachines
	phaseRandomTicks
	phaseMobs
	phaseEntities
	phaseSecondly
	phaseSpawning
	phaseSprings
	phaseWorld
	phaseSaving
	phaseFlush
	phaseCount
)

var phaseNames = [phaseCount]string{
	"clock", "scheduled tasks", "block updates", "machines", "random ticks",
	"mobs", "entities & players", "once-a-second", "spawning", "springs",
	"world effects", "saving", "hud & flush",
}

// tickPhases is one tick's lap clock.
type tickPhases struct {
	last time.Time
	d    [phaseCount]time.Duration
}

func (p *tickPhases) start(t time.Time) {
	p.last = t
	p.d = [phaseCount]time.Duration{}
}

// lap charges the time since the last lap to phase i.
func (p *tickPhases) lap(i int) {
	now := time.Now()
	p.d[i] += now.Sub(p.last)
	p.last = now
}

// top names the phases that took the most time, longest first — the three
// over a millisecond.
func (p *tickPhases) top() string {
	idx := make([]int, 0, phaseCount)
	for i, d := range p.d {
		if d >= time.Millisecond {
			idx = append(idx, i)
		}
	}
	sort.Slice(idx, func(a, b int) bool { return p.d[idx[a]] > p.d[idx[b]] })
	if len(idx) > 3 {
		idx = idx[:3]
	}
	parts := make([]string, len(idx))
	for k, i := range idx {
		parts[k] = fmt.Sprintf("%s %s", phaseNames[i], p.d[i].Round(time.Millisecond))
	}
	return strings.Join(parts, ", ")
}
