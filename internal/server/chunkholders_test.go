package server

import (
	"testing"
	"time"
)

// spectators_generate_chunks through the dispatcher: with the rule off a
// spectator is gated to what others hold loaded — alice's view window and a
// forced chunk — and with it on (the default) nobody is gated.
func TestSpectatorsGenerateChunksGate(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice, carol := ps["alice"], ps["carol"]
	s.handleCommand(alice, "gamemode spectator carol")
	s.handleCommand(alice, "forceload add 800 800")
	settle(t, h, logs, "G0")
	time.Sleep(400 * time.Millisecond) // a few ticks for the mirror
	if carol.loadedOnly.Load() {
		t.Fatal("a spectator was gated with spectators_generate_chunks on")
	}
	s.handleCommand(alice, "gamerule spectators_generate_chunks false")
	settle(t, h, logs, "G1")
	deadline := time.Now().Add(5 * time.Second)
	for !carol.loadedOnly.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the spectator was never gated")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(400 * time.Millisecond)
	if alice.loadedOnly.Load() {
		t.Error("a creative player was gated")
	}
	if !h.heldLoaded(0, 1, 1) {
		t.Error("a chunk in alice's view is not held")
	}
	if !h.heldLoaded(0, 50, 50) {
		t.Error("the forced chunk is not held")
	}
	if h.heldLoaded(0, 30, -30) || h.heldLoaded(1, 0, 0) {
		t.Error("a chunk nobody holds counts as held")
	}
}
