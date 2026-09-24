package server

import (
	"testing"
	"time"
)

// /version prints vanilla's ten lines for the engine's canonical version,
// even with send_command_feedback off; /stop announces itself and runs the
// graceful-shutdown hook; both are refused to a non-operator.
func TestCommandVersionAndStop(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	stopped := make(chan struct{}, 1)
	s.Stop = func() { stopped <- struct{}{} }

	s.handleCommand(alice, "gamerule send_command_feedback false")
	s.handleCommand(alice, "version")
	settle(t, h, logs, "V1")
	a := linesBetween(logs["alice"], "", "V1")
	if len(a) != 10 || a[0] != "Server version info:" || a[1] != "id = 26.3" ||
		a[3] != "data = 5023" || a[5] != "protocol = 777 (0x309)" || a[9] != "stable = yes" {
		t.Fatalf("version lines: %q", a)
	}
	s.handleCommand(alice, "gamerule send_command_feedback true")

	s.handleCommand(ps["carol"], "stop")
	s.handleCommand(ps["carol"], "version")
	s.handleCommand(alice, "stop")
	settle(t, h, logs, "V2")
	if a := linesBetween(logs["alice"], "V1", "V2"); !hasLine(a, "Stopping the server") {
		t.Errorf("stop line: %q", a)
	}
	if b := linesBetween(logs["bob"], "V1", "V2"); !hasLine(b, "§7§o[alice: Stopping the server]") {
		t.Errorf("other operators are told: %q", b)
	}
	c := linesBetween(logs["carol"], "V1", "V2")
	if permissionRefusals(c) != 2 {
		t.Errorf("non-op: %q", c)
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("/stop never ran the shutdown")
	}
	select {
	case <-stopped:
		t.Error("the shutdown ran twice (carol's /stop must not)")
	case <-time.After(stopDelay + 200*time.Millisecond):
	}
}
