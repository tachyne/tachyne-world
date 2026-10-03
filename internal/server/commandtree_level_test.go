package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tachyne/tachyne-common/access"
	attachproto "github.com/tachyne/tachyne-common/attach"
)

// treeRoots is the root command names of an encoded tree.
func treeRoots(t *testing.T, body []byte) map[string]bool {
	t.Helper()
	nodes, root := decodeCommandTree(t, body)
	out := map[string]bool{}
	for _, k := range nodes[root].kids {
		out[nodes[k].name] = true
	}
	return out
}

// Each level is sent only the commands it may run (Commands.sendCommands):
// everyone has /help and /msg, a level-2 operator /time but not /kick, a
// level-3 one /kick but not /stop; an op-only plugin command needs level 2.
func TestCommandTreeFilteredByLevel(t *testing.T) {
	s := &Server{}
	s.setTreeCommands([]string{"openplug", "opplug"}, map[string]bool{"opplug": true})
	for _, c := range []struct {
		level     int
		have, not []string
	}{
		{permAll, []string{"help", "msg", "list", "trigger", "openplug"}, []string{"time", "gamemode", "kick", "stop", "opplug", "execute"}},
		{permModerators, []string{"help", "openplug"}, []string{"time", "opplug"}},
		{permGamemasters, []string{"time", "gamemode", "execute", "data", "opplug"}, []string{"kick", "op", "stop"}},
		{permAdmins, []string{"time", "kick", "op", "whitelist"}, []string{"stop", "save-all"}},
		{permOwners, []string{"time", "kick", "stop", "save-all", "opplug"}, nil},
	} {
		roots := treeRoots(t, s.commandTreeFor(c.level))
		for _, n := range c.have {
			if !roots[n] {
				t.Errorf("level %d: /%s missing from the tree", c.level, n)
			}
		}
		for _, n := range c.not {
			if roots[n] {
				t.Errorf("level %d: /%s is in the tree", c.level, n)
			}
		}
	}
	if &s.commandTreeFor(2)[0] != &s.commandTreeFor(2)[0] {
		t.Error("a level's tree is built once and cached")
	}
}

// /op and /deop send the player its new level and then the tree that level
// may use (PlayerList.sendPlayerPermissionLevel → sendCommands).
func TestOpResendsCommandTree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	h, s, players, pl := tickCmdHub(t)
	s.Access = access.New(srv.URL, "", 0)
	go func() {
		for ev := range h.events {
			if e, ok := ev.(evHubCmd); ok {
				e.fn(players)
			}
		}
	}()
	mate := cmdSecondPlayer(players, 2, "mate")
	flush := func() { // FIFO: everything posted before has run
		done := make(chan struct{})
		s.onHub(func(map[int32]*tracked) { close(done) })
		<-done
	}
	sent := func() (status []int32, trees [][]byte) {
		for _, ev := range drainEvs(mate.p) {
			switch v := ev.(type) {
			case attachproto.EntityStatus:
				status = append(status, v.Status)
			case attachproto.CommandTree:
				trees = append(trees, v.Data)
			}
		}
		return
	}
	drainEvs(mate.p)
	s.handleCommand(pl.p, "op mate")
	flush()
	status, trees := sent()
	if len(status) != 1 || status[0] != 24+permOwners || len(trees) != 1 {
		t.Fatalf("after /op: statuses %v, %d trees", status, len(trees))
	}
	if roots := treeRoots(t, trees[0]); !roots["time"] || !roots["stop"] {
		t.Errorf("an operator's tree lacks the operator commands: %v", roots)
	}
	s.handleCommand(pl.p, "deop mate")
	flush()
	status, trees = sent()
	if len(status) != 1 || status[0] != 24 || len(trees) != 1 {
		t.Fatalf("after /deop: statuses %v, %d trees", status, len(trees))
	}
	if roots := treeRoots(t, trees[0]); roots["time"] || !roots["help"] {
		t.Errorf("a deopped player's tree: %v", roots)
	}
}
