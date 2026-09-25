package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func panicPad(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.world.ForceLoad(0, 0, 2)
	for x := -12; x <= 12; x++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl.x, pl.y, pl.z = 2.5, 180, 0.5
	return h, players, pl
}

// PanicGoal keeps re-picking spots only while the hurt is under forty ticks
// old, then finishes the leg it is on: a struck cow is calm again within a
// few seconds rather than running on for four.
func TestPanicGoalWearsOffAfterFortyTicks(t *testing.T) {
	h, players, pl := panicPad(t)
	cow := h.spawnMob(players, entityCow, 0.5, 180, 0.5)
	cow.health = 1000
	h.attackMob(players, pl.p.eid, cow.eid)
	pl.x = 50 // out of the way
	if cow.panic != 40/mobMoveInterval {
		t.Fatalf("a hurt is remembered forty ticks: panic %d updates", cow.panic)
	}
	for i := 0; i < 40/mobMoveInterval+panicLegMax+1; i++ {
		h.updateMobs(players)
	}
	if cow.panic != 0 || cow.panicHasT {
		t.Fatalf("the cow should have stopped panicking: panic %d, leg %v", cow.panic, cow.panicHasT)
	}
}

// AnimalPanic runs 100-120 ticks from its start, and a second blow while it
// runs does not stretch it.
func TestAnimalPanicRunsItsOwnClock(t *testing.T) {
	h, players, pl := panicPad(t)
	goat := h.spawnMob(players, entityGoat, 0.5, 180, 0.5)
	goat.health = 1000
	h.attackMob(players, pl.p.eid, goat.eid)
	first := goat.panic
	if first < 100/mobMoveInterval || first > 120/mobMoveInterval {
		t.Fatalf("AnimalPanic lasts 100-120 ticks: %d updates", first)
	}
	goat.panic -= 10
	goat.invulnTicks = 0
	h.attackMob(players, pl.p.eid, goat.eid)
	if goat.panic != first-10 {
		t.Fatalf("a second blow restarted the panic: %d, want %d", goat.panic, first-10)
	}
}

// PanicGoal outranks AvoidEntityGoal: a rabbit backing off from a wolf
// that is struck drops the retreat and panics.
func TestPanicOutranksAvoid(t *testing.T) {
	h, players, pl := panicPad(t)
	r := h.spawnMob(players, entityRabbit, 0.5, 180, 0.5)
	r.health = 1000
	r.avoidLeft, r.avoidX, r.avoidZ = avoidGiveUp, -10.5, 0.5
	h.attackMob(players, pl.p.eid, r.eid)
	pl.x = 50
	if h.avoidStep(players, r) || r.avoidLeft != 0 {
		t.Fatal("a panicking rabbit should give up its retreat")
	}
}
