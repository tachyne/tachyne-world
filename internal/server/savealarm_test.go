package server

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// A save that keeps failing reaches the operators after saveAlarmAfter, then
// once a minute, and a recovery is announced; a single failed save is not.
func TestSaveAlarm(t *testing.T) {
	clock := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	a := newSaveAlarm(func() time.Time { return clock })
	fail := errors.New("disk full")
	step := func(d time.Duration, err error) string { clock = clock.Add(d); return a.observe("world", err) }

	if msg := step(0, fail); msg != "" {
		t.Fatalf("the first failure alone must not alarm: %q", msg)
	}
	if msg := step(30*time.Second, nil); msg != "" {
		t.Fatalf("a recovery nobody was warned about is not announced: %q", msg)
	}
	step(0, fail)
	for i := 0; i < 3; i++ {
		if msg := step(30*time.Second, fail); msg != "" {
			t.Fatalf("under saveAlarmAfter there is no alarm yet (%d): %q", i, msg)
		}
	}
	msg := step(30*time.Second, fail)
	if !strings.Contains(msg, "world has not saved for 2m0s") || !strings.Contains(msg, "disk full") {
		t.Fatalf("after two minutes of failures the operators are warned: %q", msg)
	}
	if msg := step(30*time.Second, fail); msg != "" {
		t.Fatalf("the warning repeats at most once a minute: %q", msg)
	}
	if msg := step(30*time.Second, fail); msg == "" {
		t.Fatal("…and does repeat after a minute")
	}
	if msg := step(30*time.Second, nil); !strings.Contains(msg, "saving again") {
		t.Fatalf("the recovery is announced: %q", msg)
	}
}
