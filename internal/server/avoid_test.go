package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestSkeletonAvoidsWolf: a skeleton with a wolf four blocks off heads the
// other way at sprint pace and forgets its target; a creeper ignores the
// wolf but not a cat.
func TestSkeletonAvoidsWolf(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -24; x <= 24; x++ {
		for z := -24; z <= 24; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	sk := h.spawnMob(players, entitySkeleton, 0.5, 180, 0.5)
	wolf := h.spawnMob(players, entityWolf, 4.5, 180, 0.5)
	sk.hasTarget = true
	h.gridDirty()
	h.avoidScan(players, sk)
	if sk.avoidLeft == 0 || sk.avoidEID != wolf.eid || sk.hasTarget {
		t.Fatalf("the skeleton should be avoiding the wolf: left %d eid %d target %v", sk.avoidLeft, sk.avoidEID, sk.hasTarget)
	}
	if sk.avoidX > sk.x {
		t.Fatalf("the spot is on the far side from the wolf: %.1f", sk.avoidX)
	}
	if !h.avoidStep(players, sk) || sk.vx >= 0 {
		t.Fatalf("walking away: vx %.3f", sk.vx)
	}
	if got := math.Hypot(sk.vx, sk.vz); math.Abs(got-sk.moveSpeed()*1.2) > 1e-9 {
		t.Fatalf("sprint pace inside seven blocks: %.4f", got)
	}
	cr := h.spawnMob(players, entityCreeper, 0.5, 180, -3.5)
	h.gridDirty()
	h.avoidScan(players, cr)
	if cr.avoidLeft != 0 {
		t.Fatal("a creeper does not mind a wolf")
	}
	h.spawnMob(players, entityCat, 2.5, 180, -3.5)
	h.gridDirty()
	h.avoidScan(players, cr)
	if cr.avoidLeft == 0 {
		t.Fatal("but it does mind a cat")
	}
}

// TestRabbitAvoidsPlayer: a survival player eight blocks off sends a rabbit
// the other way at vanilla's 2.2; a creative one does not; a tamed cat is
// not shy but a wild one is.
func TestRabbitAvoidsPlayer(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -24; x <= 24; x++ {
		for z := -24; z <= 24; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl.x, pl.y, pl.z = 6.5, 180, 0.5
	r := h.spawnMob(players, entityRabbit, 0.5, 180, 0.5)
	h.avoidScan(players, r)
	if r.avoidLeft == 0 || !r.avoidPlayer || r.avoidEID != pl.p.eid || r.avoidX > r.x {
		t.Fatalf("the rabbit should flee the player: left %d player %v x %.1f", r.avoidLeft, r.avoidPlayer, r.avoidX)
	}
	h.avoidStep(players, r)
	if got := math.Hypot(r.vx, r.vz); math.Abs(got-r.moveSpeed()*2.2) > 1e-9 {
		t.Fatalf("sprint pace 2.2 inside seven: %.4f", got)
	}
	pl.gamemode = gmCreative
	r.avoidLeft = 0
	h.avoidScan(players, r)
	if r.avoidLeft != 0 {
		t.Fatal("creative players are not fled")
	}
	pl.gamemode = gmSurvival
	c := h.spawnMob(players, entityCat, 0.5, 180, 2.5)
	c.tamed = true
	h.avoidScan(players, c)
	if c.avoidLeft != 0 {
		t.Fatal("a tamed cat is not shy")
	}
	c.tamed = false
	h.avoidScan(players, c)
	if c.avoidLeft == 0 {
		t.Fatal("a wild cat is")
	}
}
