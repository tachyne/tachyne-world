package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
)

// /setworldspawn in the Nether (26.3 stores RespawnData with its level):
// it is taken, the compass is told the Nether, a death with no bed respawns
// there, and a restart keeps the overworld's own join point.
func TestSetWorldSpawnInTheNether(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	alice := ps["alice"]
	onHub(t, h, func() {
		h.nether, _ = world.NewNether(1, nil)
		for _, tr := range h.playersRef {
			if tr.p.name == "alice" {
				tr.dim = dimNether
			}
		}
	})
	alice.dim = dimNether
	s.handleCommand(alice, "setworldspawn 10 70 20")
	settle(t, h, logs, "W1")
	if got := linesBetween(logs["alice"], "", "W1"); !hasLine(got, "Set the world spawn point to 10, 70, 20 [0.0]") {
		t.Fatalf("replies %q", got)
	}
	onHub(t, h, func() {
		if h.spawnDim() != dimNether || h.worldSpawnX != 10.5 {
			t.Errorf("spawn dim %d x %v", h.spawnDim(), h.worldSpawnX)
		}
		var bob *tracked
		for _, tr := range h.playersRef {
			if tr.p.name == "bob" {
				bob = tr
			}
		}
		if _, _, _, dim := h.respawnPoint(h.playersRef, bob); dim != dimNether {
			t.Errorf("a bedless respawn goes to dim %d, want the Nether", dim)
		}
		drainEvs(bob.p)
		h.sendDefaultSpawn(bob)
		for _, ev := range drainEvs(bob.p) {
			if d, ok := ev.(attachproto.DefaultSpawn); ok && d.Dim != "minecraft:the_nether" {
				t.Errorf("the compass was told %q", d.Dim)
			}
		}
	})
}
