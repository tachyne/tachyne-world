package server

import (
	"encoding/json"
	"testing"

	"tachyne/internal/world"
)

// TestExecuteCommand checks the shared plugin-command router (backend-agnostic).
func TestExecuteCommand(t *testing.T) {
	h := newHub(world.New(1))
	go h.run()

	if e := executeCommand(h, "settime", json.RawMessage(`{"time":13000}`)); e != "" {
		t.Fatalf("settime: %s", e)
	}
	if got := h.dayTime.Load(); got != 13000 {
		t.Errorf("settime: dayTime=%d, want 13000", got)
	}
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
