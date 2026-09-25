package server

import (
	"strings"
	"testing"
)

// /tellraw sends a component, filled in for each reader: a score of "*" is
// the reader's own, a selector the names it picks; the flattened text is the
// fallback line.
func TestTellraw(t *testing.T) {
	s, h, ps, logs, _ := eventServer(t, "")
	alice := ps["alice"]
	s.handleCommand(alice, "scoreboard objectives add kills dummy")
	s.handleCommand(alice, "scoreboard players set bob kills 5")
	s.handleCommand(alice, `tellraw bob {"text":"hi  ","color":"gold","extra":[{"score":{"name":"*","objective":"kills"}},{"text":" from "},{"selector":"@a[name=alice]"}]}`)
	s.handleCommand(alice, `tellraw bob {"text":`)
	settle(t, h, logs, "R1")
	b := linesBetween(logs["bob"], "", "R1")
	if !hasLine(b, "hi  5 from alice") {
		t.Errorf("bob read %q, want the filled-in line", b)
	}
	a := linesBetween(logs["alice"], "", "R1")
	found := false
	for _, l := range a {
		if strings.HasPrefix(l, "Invalid chat component") {
			found = true
		}
	}
	if !found {
		t.Errorf("a broken component was not refused: %q", a)
	}
}
