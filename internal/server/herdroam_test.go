package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A herd's roaming goal drifts by a random walk. A random walk has no
// restoring force, so without a bound it diffuses away from where it started
// without limit — and because a herd's cows steer toward the goal, the whole
// group migrates with it.
//
// On an EMPTY server that is unbounded work: the boot-seeded herds are spawned
// straight into h.mobs and never enter activeChunks, and the only unload path
// (reconcileMobChunks) is reached solely from naturalSpawn, which returns early
// when there are no players. So the herds tick forever with nobody watching,
// and each step reads the world at fresh coordinates — generating and caching
// terrain no one will ever see, until the chunk cache has eaten the pod.

func roamHub(t *testing.T) (*hub, float64, float64) {
	t.Helper()
	h := newHub(world.New(1))
	h.playersRef = map[int32]*tracked{}
	// Root the herd on land the way the boot seeding does — the drift reverses
	// off water, so a herd started in the ocean never moves at all.
	lx, lz := h.findLand(0, 0)
	return h, float64(lx), float64(lz)
}

// The goal stays inside its home range no matter how long it roams.
func TestHerdGoalStaysNearHome(t *testing.T) {
	h, lx, lz := roamHub(t)
	hd := newHerd(lx, lz)
	h.herds = append(h.herds, hd)

	worst := 0.0
	// Far more steps than the drift needs to escape: at 0.05 blocks a step a
	// straight run would cover 5000 blocks.
	for i := 0; i < 100000; i++ {
		h.updateHerdTargets()
		if d := math.Hypot(hd.x-hd.hx, hd.z-hd.hz); d > worst {
			worst = d
		}
	}
	if worst > herdRoamRadius+1 { // +1: a single step may cross the line before it turns
		t.Errorf("herd goal reached %.1f blocks from home, want no more than %d",
			worst, herdRoamRadius)
	}
	if worst == 0 {
		t.Error("herd goal never moved — the test is not exercising the drift")
	}
}

// The bound must not pin the herd in place: it still roams a real area.
func TestHerdGoalStillRoams(t *testing.T) {
	h, lx, lz := roamHub(t)
	hd := newHerd(lx, lz)
	h.herds = append(h.herds, hd)
	for i := 0; i < 20000; i++ {
		h.updateHerdTargets()
	}
	if d := math.Hypot(hd.x-hd.hx, hd.z-hd.hz); d < 1 {
		t.Errorf("herd goal only reached %.2f blocks from home — it is stuck", d)
	}
}

// Every herd is rooted where it is created, so the bound applies to herds that
// appear later (a bus-spawned one) and not only to the boot-seeded three.
func TestHerdsAreRootedWhereCreated(t *testing.T) {
	h, _, _ := roamHub(t)
	i := h.herdNear(300, -400)
	hd := h.herds[i]
	if hd.hx != 300 || hd.hz != -400 {
		t.Errorf("herd home is (%.0f,%.0f), want the (300,-400) it was created at", hd.hx, hd.hz)
	}
}
