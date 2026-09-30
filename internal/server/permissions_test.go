package server

import (
	"reflect"
	"testing"

	wattach "github.com/tachyne/tachyne-world/internal/attach"
)

// Where a level comes from: the -ops flag (4, or name:N), the access roles
// (op = 4, op1…op4), the console (4); and what each level may run.
func TestOpPermissionLevels(t *testing.T) {
	s := &Server{Ops: map[string]bool{"owner": true, "gm": true}, OpLevels: map[string]int{"gm": 2}}
	s.adoptIdentity(newPlayer(1, "mod", [16]byte{1}), wattach.Identity{Name: "mod", Roles: []string{"op1", "builder"}})
	s.adoptIdentity(newPlayer(2, "admin", [16]byte{2}), wattach.Identity{Name: "admin", Roles: []string{"op3"}})
	s.adoptIdentity(newPlayer(3, "legacy", [16]byte{3}), wattach.Identity{Name: "legacy", Roles: []string{"op"}})
	for name, want := range map[string]int{
		"owner": 4, "gm": 2, "mod": 1, "admin": 3, "legacy": 4, "nobody": 0, consoleName: 4,
	} {
		if got := s.opLevel(name); got != want {
			t.Errorf("opLevel(%s) = %d, want %d", name, got, want)
		}
	}
	if s.isOp("mod") || !s.isAnyOp("mod") || !s.isOp("gm") {
		t.Error("isOp is level 2 and up; isAnyOp any level")
	}
	for _, c := range []struct {
		who, cmd string
		ok       bool
	}{
		{"nobody", "list", true}, {"nobody", "msg", true}, {"nobody", "tp", false},
		{"mod", "time", false}, {"gm", "time", true}, {"gm", "kick", false},
		{"admin", "kick", true}, {"admin", "op", true}, {"admin", "stop", false},
		{"owner", "stop", true}, {consoleName, "save-all", true},
	} {
		if got := s.commandPermitted(newPlayer(9, c.who, [16]byte{9}), c.cmd); got != c.ok {
			t.Errorf("%s running /%s: permitted=%v, want %v", c.who, c.cmd, got, c.ok)
		}
	}
	e, ok := opEntryFor([]string{"op2", "x", "op"})
	if !ok || e.level != 4 || !reflect.DeepEqual(e.roles, []string{"op2", "op"}) {
		t.Errorf("opEntryFor = %+v %v", e, ok)
	}
	if roleForLevel(4) != "op" || roleForLevel(2) != "op2" {
		t.Error("roleForLevel")
	}
	if s.opPermissionLevel() != 4 {
		t.Error("an unset op-permission-level is 4")
	}
	s.OpPermissionLevel = 3
	if s.opPermissionLevel() != 3 {
		t.Error("op-permission-level 3")
	}
}

// Through the dispatcher: a level-2 op may run /time but not /kick or
// /save-off; a level-4 op runs them all.
func TestCommandLevelsThroughDispatcher(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	s.OpLevels = map[string]int{"bob": 2}
	s.handleCommand(ps["bob"], "kick carol")
	s.handleCommand(ps["bob"], "save-off")
	s.handleCommand(ps["bob"], "time set 1000")
	settle(t, h, logs, "P1")
	b := linesBetween(logs["bob"], "", "P1")
	if permissionRefusals(b) != 2 {
		t.Errorf("level-2 bob's refusals: %q", b)
	}
	s.handleCommand(ps["alice"], "save-off")
	s.handleCommand(ps["alice"], "save-on")
	settle(t, h, logs, "P2")
	if a := linesBetween(logs["alice"], "P1", "P2"); permissionRefusals(a) != 0 {
		t.Errorf("level-4 alice was refused: %q", a)
	}
}
