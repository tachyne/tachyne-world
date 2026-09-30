package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// The bus "run" command runs a line as the console: with operator rights
// no player granted it, answering with the lines the command said, telling
// the online operators "[Server: …]" as vanilla's console does, and leaving
// no trace of itself in the players map.
func TestConsoleRunsCommands(t *testing.T) {
	s, h, _, logs := feedbackServer(t)
	h.runConsole = s.runAsConsole
	chestSt, _ := parseBlockState("chest[type=single,waterlogged=false]")
	cp := simPos{dim: 0, blockPos: blockPos{0, 100, 0}}
	onHub(t, h, func() { h.world.SetBlock(0, 100, 0, chestSt) })

	settle(t, h, logs, "C1")
	args, _ := json.Marshal(map[string]string{"command": "/item replace block 0 100 0 container.0 with white_wool"})
	data, errStr := executeCommand(h, "run", args)
	if errStr != "" {
		t.Fatalf("run: %s", errStr)
	}
	lines := data.(map[string]any)["lines"].([]string)
	if len(lines) != 1 || lines[0] != "Replaced 1 slot(s) at 0, 100, 0 with [White Wool]" {
		t.Errorf("console heard %q", lines)
	}
	settle(t, h, logs, "C2")
	want := adminLine("Server", "Replaced 1 slot(s) at 0, 100, 0 with [White Wool]")
	if !hasLine(linesBetween(logs["alice"], "C1", "C2"), want) {
		t.Errorf("alice (op) missed %q: %q", want, linesBetween(logs["alice"], "C1", "C2"))
	}
	if hasLine(linesBetween(logs["carol"], "C1", "C2"), want) {
		t.Errorf("carol is no operator but heard %q", want)
	}
	onHub(t, h, func() {
		if c := h.chests[cp]; c == nil || c.slots[0] != (invStack{item: itemByName["white_wool"], count: 1}) {
			t.Errorf("chest slot 0: %+v", c)
		}
		for _, tr := range h.playersRef {
			if tr.p.name == consoleName {
				t.Errorf("the console stayed in the players map")
			}
		}
	})

	// A failure comes back as the command's own line.
	args, _ = json.Marshal(map[string]string{"command": "item replace block 0 100 0 nowhere.1 with diamond"})
	data, errStr = executeCommand(h, "run", args)
	if errStr != "" {
		t.Fatalf("run: %s", errStr)
	}
	if lines := data.(map[string]any)["lines"].([]string); len(lines) != 1 || !strings.Contains(lines[0], "nowhere.1") {
		t.Errorf("failure heard as %q", lines)
	}
	if _, errStr := executeCommand(h, "run", []byte(`{}`)); errStr == "" {
		t.Error("an empty command ran")
	}
}

// /forceload's hub half is an event of its own (evForceLoadCmd), not an
// evHubCmd closure, and it used to answer nobody from the console. Every
// ForceLoadCommand reply now reaches it: added single and multiple, the
// query both ways, the list, removed, the failures and the too-big area
// (the first line is the one that came back empty from the bus).
func TestConsoleForceloadAnswers(t *testing.T) {
	s, h, _, logs := feedbackServer(t)
	h.runConsole = s.runAsConsole
	settle(t, h, logs, "F1")
	run := func(cmd string) []string {
		t.Helper()
		args, _ := json.Marshal(map[string]string{"command": cmd})
		data, errStr := executeCommand(h, "run", args)
		if errStr != "" {
			t.Fatalf("run %s: %s", cmd, errStr)
		}
		return data.(map[string]any)["lines"].([]string)
	}
	for _, c := range []struct{ cmd, want string }{
		{"/forceload add 94 -93 94 -84", "Marked chunk [5, -6] in minecraft:overworld to be force loaded"},
		{"forceload add 0 0", "Marked chunk [0, 0] in minecraft:overworld to be force loaded"},
		{"forceload add 0 0", "No chunks were marked for force loading"},
		{"forceload add 0 16 16 16", "Marked 2 chunks in minecraft:overworld from [0, 1] to [1, 1] to be force loaded"},
		{"forceload query 5 5", "Chunk at [0, 0] in minecraft:overworld is marked for force loading"},
		{"forceload query 100 100", "Chunk at [6, 6] in minecraft:overworld is not marked for force loading"},
		{"forceload query", "4 force loaded chunks were found in minecraft:overworld at: [5, -6], [0, 0], [0, 1], [1, 1]"},
		{"forceload remove 0 0", "Unmarked chunk [0, 0] in minecraft:overworld for force loading"},
		{"forceload remove 0 0", "No chunks were removed from force loading"},
		{"forceload add 0 0 1000 1000", "Too many chunks in the specified area (maximum 256, but specified 3969)"},
		{"forceload remove all", "Unmarked all force loaded chunks in minecraft:overworld"},
		{"forceload query", "No force loaded chunks were found in minecraft:overworld"},
	} {
		if got := run(c.cmd); len(got) != 1 || got[0] != c.want {
			t.Errorf("/%s: console heard %q, want %q", strings.TrimPrefix(c.cmd, "/"), got, c.want)
		}
	}
	onHub(t, h, func() {
		for _, tr := range h.playersRef {
			if tr.p.name == consoleName {
				t.Errorf("the console stayed in the players map")
			}
		}
	})
}
