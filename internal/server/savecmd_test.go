package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /save-all, /save-off and /save-on through the dispatcher: save-all
// writes the edits now with vanilla's two lines; the switch flips once
// each way and refuses a repeat; a non-operator is refused.
func TestCommandSave(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	path := filepath.Join(t.TempDir(), "nether.gob")
	nw, err := world.NewWithStore(1, world.NewFileStore(path))
	if err != nil {
		t.Fatal(err)
	}
	nw.SetBlock(0, 70, 0, worldgen.BlockID("stone"))
	s.nether = nw // saveEverything writes every dimension's edits

	s.handleCommand(alice, "save-all flush")
	settle(t, h, logs, "S1")
	a := linesBetween(logs["alice"], "", "S1")
	if len(a) != 2 || a[0] != "Saving the game (this may take a moment!)" || a[1] != "Saved the game" {
		t.Fatalf("save-all lines: %q", a)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("save-all did not write the edits: %v", err)
	}

	s.handleCommand(alice, "save-off")
	s.handleCommand(alice, "save-off")
	settle(t, h, logs, "S2")
	if !h.saveOff.Load() {
		t.Error("save-off did not pause saving")
	}
	s.handleCommand(alice, "save-on")
	s.handleCommand(alice, "save-on")
	s.handleCommand(alice, "save-all sideways")
	s.handleCommand(ps["carol"], "save-off")
	settle(t, h, logs, "S3")
	if h.saveOff.Load() {
		t.Error("save-on did not resume saving")
	}
	a = linesBetween(logs["alice"], "S1", "S3")
	for _, want := range []string{"Automatic saving is now disabled", "Saving is already turned off",
		"Automatic saving is now enabled", "Saving is already turned on", "Usage: /save-all [flush]"} {
		if !hasLine(a, want) {
			t.Errorf("missing %q in %q", want, a)
		}
	}
	if c := linesBetween(logs["carol"], "S2", "S3"); len(c) != 1 || !strings.Contains(c[0], "permission") {
		t.Errorf("non-op: %q", c)
	}
}
