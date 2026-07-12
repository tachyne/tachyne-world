package plugin

import (
	"reflect"
	"sync"
	"sync/atomic"
)

// Priority orders handlers for one event type. Lowest runs first, so
// later (higher-priority) handlers get the final say over mutable fields
// and cancellation. Monitor runs last and must observe only — never
// mutate or cancel from a Monitor handler.
type Priority int8

const (
	Lowest Priority = iota
	Low
	Normal
	High
	Highest
	Monitor
)

// Event is implemented by every event struct. Events are always fired
// and received as pointers (e.g. On[*BlockBreakEvent]). EventName is the
// stable wire name, doubling as the bus topic suffix once events are
// published out-of-process.
type Event interface{ EventName() string }

// Cancellable is implemented by events whose default action a handler
// may veto. Cancelled events still reach later handlers (which may
// un-cancel), matching Bukkit semantics.
type Cancellable interface {
	Cancelled() bool
	SetCancelled(bool)
}

// Cancel is embedded by cancellable event structs.
type Cancel struct{ cancelled bool }

func (c *Cancel) Cancelled() bool     { return c.cancelled }
func (c *Cancel) SetCancelled(v bool) { c.cancelled = v }

type entry struct {
	prio Priority
	ign  bool // skip while event is cancelled
	seq  int64
	fn   func(Event)
}

// Dispatcher routes fired events to registered handlers. The zero value
// is not usable; call NewDispatcher.
type Dispatcher struct {
	mu    sync.RWMutex
	lists map[reflect.Type][]entry
	seq   int64
	count atomic.Int64
}

func NewDispatcher() *Dispatcher {
	return &Dispatcher{lists: map[reflect.Type][]entry{}}
}

// On registers fn for events of concrete type T at the given priority.
// If ignoreCancelled is true, fn is skipped while the event is
// cancelled. The returned func unregisters the handler; calling it more
// than once is safe. Handlers registered during a Fire take effect from
// the next Fire.
func On[T Event](d *Dispatcher, pr Priority, ignoreCancelled bool, fn func(T)) (cancel func()) {
	t := reflect.TypeFor[T]()
	d.mu.Lock()
	d.seq++
	e := entry{prio: pr, ign: ignoreCancelled, seq: d.seq, fn: func(ev Event) { fn(ev.(T)) }}
	old := d.lists[t]
	// Copy-on-write insert ordered by (priority, registration seq), so a
	// Fire snapshot in flight is never mutated under it.
	nl := make([]entry, 0, len(old)+1)
	i := 0
	for ; i < len(old) && old[i].prio <= pr; i++ {
		nl = append(nl, old[i])
	}
	nl = append(nl, e)
	nl = append(nl, old[i:]...)
	d.lists[t] = nl
	d.mu.Unlock()
	d.count.Add(1)

	var once sync.Once
	return func() {
		once.Do(func() {
			d.mu.Lock()
			old := d.lists[t]
			nl := make([]entry, 0, len(old))
			for _, oe := range old {
				if oe.seq != e.seq {
					nl = append(nl, oe)
				}
			}
			if len(nl) == 0 {
				delete(d.lists, t)
			} else {
				d.lists[t] = nl
			}
			d.mu.Unlock()
			d.count.Add(-1)
		})
	}
}

// Has reports whether any handler is registered for event type T. Use it
// to skip building expensive events entirely.
func Has[T Event](d *Dispatcher) bool {
	if d.count.Load() == 0 {
		return false
	}
	t := reflect.TypeFor[T]()
	d.mu.RLock()
	n := len(d.lists[t])
	d.mu.RUnlock()
	return n > 0
}

// HasType is the non-generic twin of Has for callers holding an Event
// value.
func (d *Dispatcher) HasType(ev Event) bool {
	if d.count.Load() == 0 {
		return false
	}
	t := reflect.TypeOf(ev)
	d.mu.RLock()
	n := len(d.lists[t])
	d.mu.RUnlock()
	return n > 0
}

// Fire runs the handler ladder for ev and reports whether the default
// action should proceed: it returns false iff ev is Cancellable and
// ended the ladder cancelled. With no handlers registered the cost is
// one atomic load.
func (d *Dispatcher) Fire(ev Event) bool {
	c, cancellable := ev.(Cancellable)
	if d.count.Load() == 0 {
		return !cancellable || !c.Cancelled()
	}
	t := reflect.TypeOf(ev)
	d.mu.RLock()
	l := d.lists[t]
	d.mu.RUnlock()
	for i := range l {
		if cancellable && l[i].ign && c.Cancelled() {
			continue
		}
		l[i].fn(ev)
	}
	return !cancellable || !c.Cancelled()
}
