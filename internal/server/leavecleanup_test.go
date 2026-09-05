package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Per-player side tables keyed by eid must be released on leave. Eids are
// unique per join, so anything left behind is a permanent entry — the exact
// "map keyed by session id with no delete on disconnect" shape.
func TestLeaveReleasesPerPlayerSculkState(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[pl.p.eid] = pl
	h.sculkStep[pl.p.eid] = 42
	h.sculkLastX[pl.p.eid] = 1.5
	h.sculkLastZ[pl.p.eid] = -2.5

	h.onLeave(players, pl.p)

	if _, ok := h.sculkStep[pl.p.eid]; ok {
		t.Error("sculkStep entry survived leave")
	}
	if _, ok := h.sculkLastX[pl.p.eid]; ok {
		t.Error("sculkLastX entry survived leave")
	}
	if _, ok := h.sculkLastZ[pl.p.eid]; ok {
		t.Error("sculkLastZ entry survived leave")
	}
}
