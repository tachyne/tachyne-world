package plugin

// Task is a cancellation handle for a scheduled function.
type Task interface{ Cancel() }

// Scheduler runs functions on future ticks (20 ticks per second). It is
// the one Context API safe to call from any goroutine; the scheduled
// functions themselves always run on the tick goroutine, at the top of
// the tick, in scheduling order.
type Scheduler interface {
	// NextTick runs fn at the start of the next tick.
	NextTick(fn func())
	// After runs fn once, delayTicks from now (minimum 1).
	After(delayTicks int, fn func()) Task
	// Every runs fn repeatedly, first firing one interval from now.
	Every(intervalTicks int, fn func()) Task
}
