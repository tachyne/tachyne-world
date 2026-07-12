package plugin

import "testing"

func TestPriorityOrder(t *testing.T) {
	d := NewDispatcher()
	var order []string
	// Register out of order; execution must follow the ladder, with
	// same-priority handlers in registration order.
	On(d, Monitor, false, func(e *PlayerJoinEvent) { order = append(order, "monitor") })
	On(d, Normal, false, func(e *PlayerJoinEvent) { order = append(order, "normal1") })
	On(d, Lowest, false, func(e *PlayerJoinEvent) { order = append(order, "lowest") })
	On(d, Highest, false, func(e *PlayerJoinEvent) { order = append(order, "highest") })
	On(d, Normal, false, func(e *PlayerJoinEvent) { order = append(order, "normal2") })

	if !d.Fire(&PlayerJoinEvent{Name: "wes"}) {
		t.Fatal("non-cancellable event reported cancelled")
	}
	want := []string{"lowest", "normal1", "normal2", "highest", "monitor"}
	if len(order) != len(want) {
		t.Fatalf("ran %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("ran %v, want %v", order, want)
		}
	}
}

func TestCancelSemantics(t *testing.T) {
	d := NewDispatcher()
	var saw []string
	On(d, Low, false, func(e *BlockBreakEvent) {
		saw = append(saw, "low")
		e.SetCancelled(true)
	})
	// Cancelled events still reach later handlers that didn't opt out.
	On(d, Normal, false, func(e *BlockBreakEvent) {
		saw = append(saw, "normal")
		if !e.Cancelled() {
			t.Error("normal handler should see the event cancelled")
		}
	})
	// ignoreCancelled handlers are skipped while cancelled.
	On(d, High, true, func(e *BlockBreakEvent) { saw = append(saw, "high-skipped") })
	// A later handler may un-cancel.
	On(d, Highest, false, func(e *BlockBreakEvent) { e.SetCancelled(false) })
	On(d, Monitor, true, func(e *BlockBreakEvent) { saw = append(saw, "monitor") })

	if !d.Fire(&BlockBreakEvent{Name: "wes"}) {
		t.Fatal("event was un-cancelled; Fire should report proceed")
	}
	want := []string{"low", "normal", "monitor"}
	if len(saw) != len(want) {
		t.Fatalf("ran %v, want %v", saw, want)
	}
	for i := range want {
		if saw[i] != want[i] {
			t.Fatalf("ran %v, want %v", saw, want)
		}
	}

	// Without the un-canceller, Fire reports cancelled.
	d2 := NewDispatcher()
	On(d2, Normal, false, func(e *BlockBreakEvent) { e.SetCancelled(true) })
	if d2.Fire(&BlockBreakEvent{}) {
		t.Fatal("cancelled event reported proceed")
	}
}

func TestUnsubscribe(t *testing.T) {
	d := NewDispatcher()
	n := 0
	off := On(d, Normal, false, func(e *PlayerQuitEvent) { n++ })
	d.Fire(&PlayerQuitEvent{})
	off()
	off() // double-call is safe
	d.Fire(&PlayerQuitEvent{})
	if n != 1 {
		t.Fatalf("handler ran %d times, want 1", n)
	}
	if Has[*PlayerQuitEvent](d) {
		t.Fatal("Has true after last handler unsubscribed")
	}
}

func TestHasAndIdleFastPath(t *testing.T) {
	d := NewDispatcher()
	if Has[*PlayerMoveEvent](d) || d.HasType(&PlayerMoveEvent{}) {
		t.Fatal("Has true on empty dispatcher")
	}
	if !d.Fire(&PlayerMoveEvent{}) {
		t.Fatal("idle Fire must report proceed")
	}
	On(d, Normal, false, func(e *PlayerMoveEvent) {})
	if !Has[*PlayerMoveEvent](d) || !d.HasType(&PlayerMoveEvent{}) {
		t.Fatal("Has false with a handler registered")
	}
	// Other event types stay cold.
	if Has[*PlayerJoinEvent](d) {
		t.Fatal("Has leaked across event types")
	}
}

func TestRegisterDuringDispatch(t *testing.T) {
	d := NewDispatcher()
	ran := 0
	On(d, Normal, false, func(e *PlayerJoinEvent) {
		// Registering mid-fire must not affect the current snapshot.
		On(d, Monitor, false, func(e *PlayerJoinEvent) { ran += 10 })
		ran++
	})
	d.Fire(&PlayerJoinEvent{})
	if ran != 1 {
		t.Fatalf("mid-fire registration leaked into current fire: ran=%d", ran)
	}
	d.Fire(&PlayerJoinEvent{})
	if ran != 12 {
		t.Fatalf("second fire should run both handlers: ran=%d", ran)
	}
}

func TestRegisterPanics(t *testing.T) {
	mustPanic := func(name string, fn func()) {
		defer func() {
			if recover() == nil {
				t.Errorf("%s: expected panic", name)
			}
		}()
		fn()
	}
	mustPanic("nil", func() { Register(nil) })
	Register(&testPlugin{name: "dup-check"})
	mustPanic("duplicate", func() { Register(&testPlugin{name: "dup-check"}) })
	mustPanic("empty", func() { Register(&testPlugin{name: ""}) })
}

type testPlugin struct{ name string }

func (p *testPlugin) Name() string             { return p.name }
func (p *testPlugin) Enable(ctx Context) error { return nil }
func (p *testPlugin) Disable()                 {}
