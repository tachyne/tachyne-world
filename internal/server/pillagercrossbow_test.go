package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestPillagerCrossbow: within eight blocks a pillager draws (flag up)
// for twenty-five ticks, aims for twenty to forty, fires and draws again.
func TestPillagerCrossbow(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 6.5, 180, 0.5
	p := h.spawnMob(players, entityPillager, 0.5, 180, 0.5)
	h.pillagerTick(players, p)
	if p.cbState != cbCharging {
		t.Fatalf("it draws first: state %d", p.cbState)
	}
	for i := 0; i < 13 && p.cbState == cbCharging; i++ {
		h.pillagerTick(players, p)
	}
	if p.cbState != cbCharged || p.cbTicks < crossbowAimMin || p.cbTicks >= crossbowAimMin+crossbowAimRandom {
		t.Fatalf("loaded after twenty-five ticks, aiming: state %d ticks %d", p.cbState, p.cbTicks)
	}
	before := len(h.arrows)
	for i := 0; i < 30 && len(h.arrows) == before; i++ {
		h.pillagerTick(players, p)
	}
	if len(h.arrows) != before+1 || p.cbState != cbUncharged {
		t.Fatalf("one bolt, then it draws again: bolts %d state %d", len(h.arrows)-before, p.cbState)
	}
}
