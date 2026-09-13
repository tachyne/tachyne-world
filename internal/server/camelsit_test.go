package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestCamelSits: an idle camel sits after twenty seconds of standing, is
// held still while sat, stands for a rider pushing forward, and stands
// instantly when hit; the pose metadata carries the change tick.
func TestCamelSits(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	h.tick.Store(1000)
	c := h.spawnMob(players, entityCamel, 0.5, 180, 0.5)
	c.baby = false
	if c.camelSitting() || c.camelRefusesToMove(1000) {
		t.Fatal("a fresh camel stands and may walk (pose tick 0 is long past)")
	}
	sat := false
	for i := 0; i < 2000 && !sat; i++ {
		h.camelSitStep(players, c)
		sat = c.camelSitting()
	}
	if !sat || c.poseTick != -1000 {
		t.Fatalf("the camel should sit with the tick recorded: %v %d", sat, c.poseTick)
	}
	if !h.camelSitStep(players, c) {
		t.Fatal("a sat camel refuses to move")
	}
	want := []byte{byte(c.eid), metaIndexPose, metaTypePose, poseSitting, metaIndexCamelPoseTick, metaTypeLong}
	if got := camelPoseMeta(c); string(got[:6]) != string(want) {
		t.Fatalf("pose meta %v", got)
	}
	// A rider pushing forward: not while the fold is still playing.
	pl.ridingEID, c.rider = c.eid, pl.p.eid
	h.camelRiderForward(players, pl)
	if !c.camelSitting() {
		t.Fatal("mid-fold the camel stays down")
	}
	h.tick.Store(1100)
	h.camelRiderForward(players, pl)
	if c.camelSitting() || c.poseTick != 1100 {
		t.Fatalf("forward stands it up: %d", c.poseTick)
	}
	// Hit while sat: up at once, no rise animation.
	pl.ridingEID, c.rider = 0, 0
	h.tick.Store(2000)
	h.camelSitDown(players, c)
	c.kb = 3
	h.camelSitStep(players, c)
	if c.camelSitting() || c.camelInTransition(2000) {
		t.Fatalf("a hit stands the camel up instantly: %d", c.poseTick)
	}
}
