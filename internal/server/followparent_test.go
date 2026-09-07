package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A calf six blocks from a cow walks toward it; within three it stops; a
// baby with no adult of its kind about does nothing.
func TestBabyFollowsParent(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	cow := h.spawnAnimal(players, entityCow, 0, 0)
	calf := h.spawnAnimal(players, entityCow, 6, 0)
	if cow == nil || calf == nil {
		t.Fatal("no cows")
	}
	cow.x, cow.y, cow.z = 0.5, 70, 0.5
	calf.x, calf.y, calf.z = 6.5, 70, 0.5
	calf.baby = true
	if !h.followParentStep(calf) {
		t.Fatal("the calf should follow the cow")
	}
	if calf.vx >= 0 || math.Abs(calf.vz) > 1e-9 {
		t.Fatalf("the calf should head west toward the cow: vx=%.3f vz=%.3f", calf.vx, calf.vz)
	}
	if calf.parent != cow.eid {
		t.Fatal("the cow should be remembered as the parent")
	}
	calf.x = 2.5 // close enough: the goal ends
	if h.followParentStep(calf) {
		t.Fatal("a calf beside its parent should stop following")
	}
	// No adult nearby: nothing to follow (the search runs only every 10 ticks).
	calf.x, calf.parentRecalc = 40.5, 0
	if h.followParentStep(calf) {
		t.Fatal("a calf far from any adult has no parent to follow")
	}
	// A pig's piglet does not follow a cow.
	piglet := h.spawnAnimal(players, entityPig, 6, 0)
	piglet.x, piglet.y, piglet.z, piglet.baby, piglet.parentRecalc = 6.5, 70, 0.5, true, 0
	if h.followParentStep(piglet) {
		t.Fatal("a piglet must not follow a cow")
	}
}
