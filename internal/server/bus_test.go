package server

import (
	"encoding/json"
	"testing"
	"time"

	"tachyne/internal/world"
)

// waitDayTime polls for an async day-time set to land (settime goes through
// the hub event queue since the plugin TimeSetEvent).
func waitDayTime(t *testing.T, h *hub, want uint64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for h.dayTime.Load() != want {
		if time.Now().After(deadline) {
			t.Fatalf("dayTime=%d, want %d", h.dayTime.Load(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestExecuteCommand checks the shared plugin-command router (backend-agnostic).
func TestExecuteCommand(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.DoMobSpawning = false
	h.rules.DoDaylight = false // hold the clock still so the settime poll target is exact
	go h.run()

	if e := executeCommand(h, "settime", json.RawMessage(`{"time":13000}`)); e != "" {
		t.Fatalf("settime: %s", e)
	}
	// settime routes through the hub now (the plugin TimeSetEvent fires on
	// the tick goroutine), so the write is asynchronous.
	waitDayTime(t, h, 13000)
	if e := executeCommand(h, "say", json.RawMessage(`{"text":"hi"}`)); e != "" {
		t.Errorf("say: %s", e)
	}
	if executeCommand(h, "say", json.RawMessage(`{}`)) == "" {
		t.Error("say without text should error")
	}
	if executeCommand(h, "nope", nil) == "" {
		t.Error("unknown command should error")
	}
}
