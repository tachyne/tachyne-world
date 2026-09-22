package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Hit one zombified piglin and the pack comes — every one inside a box
// thirty-five across and ten high, and they come for YOU rather than just
// seething where they stand.
func TestHittingOneZombifiedPiglinAlertsThePack(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.x, pl.y, pl.z = 0.5, 70, 0.5

	hit := h.spawnMobIn(players, entityZombifiedPiglin, 0, 2.5, 70, 0.5)
	near := h.spawnMobIn(players, entityZombifiedPiglin, 0, 25.5, 70, 0.5) // inside 35
	high := h.spawnMobIn(players, entityZombifiedPiglin, 0, 4.5, 85, 0.5)  // 15 up: outside 10
	far := h.spawnMobIn(players, entityZombifiedPiglin, 0, 60.5, 70, 0.5)  // outside 35
	for _, m := range []*mob{hit, near, high, far} {
		if m == nil {
			t.Fatal("the piglins should have spawned")
		}
	}
	h.alertZombifiedPiglins(hit, pl)

	if near.targetEID != pl.p.eid {
		t.Fatalf("a piglin inside the box should hunt the attacker, target=%d", near.targetEID)
	}
	if high.targetEID == pl.p.eid {
		t.Fatal("one fifteen blocks up is outside the ten-high box")
	}
	if far.targetEID == pl.p.eid {
		t.Fatal("one sixty blocks away is outside the box")
	}
}
