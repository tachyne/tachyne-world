package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
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
