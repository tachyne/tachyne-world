package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A sprinting player within reach rolls an armadillo up: rolling, then
// scared (blows lose a point and halve), then unrolling once the danger
// memory lapses; a blow from a living thing is danger in itself.
func TestArmadilloRollsUp(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.tick.Store(80) // on the scare check
	a := h.spawnSpecies(players, entityArmadillo, 0, 0.5, 70, 0.5)
	if a == nil {
		t.Fatal("no armadillo")
	}
	a.x, a.y, a.z = 0.5, 70, 0.5
	pl.x, pl.y, pl.z, pl.sprinting = 3.5, 70, 0.5, true
	h.armadilloTick(players, a)
	if a.armState != armRolling {
		t.Fatalf("a sprinting player beside it should start the roll: state=%d", a.armState)
	}
	h.tick.Store(80 + armRollingTicks)
	h.armadilloTick(players, a)
	if a.armState != armScared {
		t.Fatalf("after rolling it is scared: state=%d", a.armState)
	}
	hp := a.health
	a.hurt(5)
	if got := hp - a.health; got != 2 {
		t.Fatalf("a rolled-up armadillo takes (5-1)/2 = 2, took %d", got)
	}
	// The danger lapses (the player walks off): unrolling, then idle.
	pl.sprinting = false
	h.tick.Store(80 + armDangerTicks + 1)
	h.armadilloTick(players, a)
	if a.armState != armUnrolling {
		t.Fatalf("once the danger memory runs low it unrolls: state=%d", a.armState)
	}
	h.tick.Store(80 + armDangerTicks + 1 + armUnrollingTicks)
	h.armadilloTick(players, a)
	if a.armState != armIdle {
		t.Fatalf("and stands up: state=%d", a.armState)
	}
	// A blow is danger too.
	h.armadilloHurtByLiving(players, a)
	if a.armState != armRolling {
		t.Fatal("a blow rolls it up")
	}
	// A cub never rolls.
	a.armState, a.baby = armIdle, true
	h.armadilloHurtByLiving(players, a)
	if a.armState != armIdle {
		t.Fatal("a cub does not roll up")
	}
}

// ArmadilloBallUp: rolled up with danger still about, the armadillo peeks
// out (entity event 64) — not at once, but 150 to 450 ticks on, and again
// after as long.
func TestArmadilloPeeks(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	for x := -2; x <= 5; x++ {
		for z := -2; z <= 2; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl := survPlayer(h)
	pl.p.eid = 500
	players := map[int32]*tracked{pl.p.eid: pl}
	a := h.spawnSpecies(players, entityArmadillo, 0, 0.5, 180, 0.5)
	pl.x, pl.y, pl.z, pl.sprinting = 3.5, 180, 0.5, true
	pl.tracked = map[int32]bool{a.eid: true}
	h.armadilloHurtByLiving(players, a)
	start := h.tick.Load()
	var peeks []uint64
	for i := 0; i < 600; i++ {
		h.tick.Add(mobMoveInterval)
		a.armDangerUntil = h.tick.Load() + armDangerTicks // the danger stays
		h.armadilloTick(players, a)
		for _, ev := range drainEvs(pl.p) {
			if st, ok := ev.(attachproto.EntityStatus); ok && st.EID == a.eid && st.Status == entityStatusArmadilloPeek {
				peeks = append(peeks, h.tick.Load()-start)
			}
		}
	}
	if len(peeks) < 2 {
		t.Fatalf("a scared armadillo should peek now and then, peeked at %v", peeks)
	}
	if peeks[0] < 150 || peeks[1]-peeks[0] < 150 || peeks[1]-peeks[0] > 452 {
		t.Fatalf("peeks come 150-450 ticks apart: %v", peeks)
	}
}
