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
