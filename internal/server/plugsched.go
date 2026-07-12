package server

import (
	"sync"
	"sync/atomic"

	"tachyne/plugin"
)

// pluginSched runs plugin-scheduled functions on the hub goroutine, bucketed
// by due tick like hub.pending. Insertion is mutex-guarded so plugins may
// schedule from their own goroutines; execution happens only in run().
type pluginSched struct {
	mu     sync.Mutex
	h      *hub
	byTick map[uint64][]*ptask
}

type ptask struct {
	fn    func()
	every uint64 // 0 = one-shot
	dead  atomic.Bool
}

func (t *ptask) Cancel() { t.dead.Store(true) }

func newPluginSched(h *hub) *pluginSched {
	return &pluginSched{h: h, byTick: map[uint64][]*ptask{}}
}

func (ps *pluginSched) schedule(delay, every int, fn func()) *ptask {
	if delay < 1 {
		delay = 1
	}
	t := &ptask{fn: fn, every: uint64(every)}
	due := ps.h.tick.Load() + uint64(delay)
	ps.mu.Lock()
	ps.byTick[due] = append(ps.byTick[due], t)
	ps.mu.Unlock()
	return t
}

// run executes every task due at or before age. The <= sweep (not just the
// exact bucket) absorbs the race where another goroutine reads the tick
// counter just before the hub increments it.
func (ps *pluginSched) run(age uint64) {
	ps.mu.Lock()
	var tasks []*ptask
	for due, l := range ps.byTick {
		if due <= age {
			tasks = append(tasks, l...)
			delete(ps.byTick, due)
		}
	}
	ps.mu.Unlock()
	for _, t := range tasks {
		if t.dead.Load() {
			continue
		}
		t.fn()
		if t.every > 0 && !t.dead.Load() {
			ps.mu.Lock()
			ps.byTick[age+t.every] = append(ps.byTick[age+t.every], t)
			ps.mu.Unlock()
		}
	}
}

// schedFacade adapts pluginSched to the plugin.Scheduler interface.
type schedFacade struct{ ps *pluginSched }

func (s schedFacade) NextTick(fn func())                     { s.ps.schedule(1, 0, fn) }
func (s schedFacade) After(delay int, fn func()) plugin.Task { return s.ps.schedule(delay, 0, fn) }
func (s schedFacade) Every(interval int, fn func()) plugin.Task {
	if interval < 1 {
		interval = 1
	}
	return s.ps.schedule(interval, interval, fn)
}
