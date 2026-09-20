package server

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// One background write at a time, and requests that arrive mid-write collapse
// into a single follow-up — a slow disk must not pile up goroutines.
func TestBgWriterRunsOneAtATimeAndCoalesces(t *testing.T) {
	var b bgWriter
	var running, runs int32
	release := make(chan struct{})
	write := func() {
		if atomic.AddInt32(&running, 1) != 1 {
			t.Error("two background writes ran at once")
		}
		atomic.AddInt32(&runs, 1)
		<-release
		atomic.AddInt32(&running, -1)
	}
	b.run(write)             // starts
	for i := 0; i < 5; i++ { // all five collapse into one follow-up
		b.run(write)
	}
	close(release)
	b.wait()
	if n := atomic.LoadInt32(&runs); n != 2 {
		t.Fatalf("five requests during one write should produce 2 runs, got %d", n)
	}
}

// wait blocks until the write has finished, which is what makes an explicit
// save safe to issue while a periodic one is in flight.
func TestBgWriterWaitBlocksUntilDone(t *testing.T) {
	var b bgWriter
	var done atomic.Bool
	b.run(func() {
		time.Sleep(30 * time.Millisecond)
		done.Store(true)
	})
	b.wait()
	if !done.Load() {
		t.Fatal("wait returned before the write finished")
	}
	b.wait() // idle: returns at once
}

// The whole point: the caller is not the one paying for the write.
func TestBgWriterReturnsImmediately(t *testing.T) {
	var b bgWriter
	var wg sync.WaitGroup
	wg.Add(1)
	start := time.Now()
	b.run(func() {
		defer wg.Done()
		time.Sleep(60 * time.Millisecond)
	})
	if d := time.Since(start); d > 20*time.Millisecond {
		t.Fatalf("run should hand off at once, took %v", d)
	}
	wg.Wait()
	b.wait()
}
