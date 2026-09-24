package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// MobEffectInstance's hidden stack: a stronger, shorter effect over a weaker,
// longer one runs first, and when it ends the weaker one is back with the
// time it had left.
func TestHiddenEffectComesBack(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.applyEffect(players, pl, effSpeed, 0, 60) // Speed I, a minute
	h.applyEffect(players, pl, effSpeed, 1, 10) // Speed II, ten seconds
	if e := pl.effects[effSpeed]; e == nil || e.amp != 1 || e.hidden == nil || e.hidden.amp != 0 {
		t.Fatalf("after Speed II: %+v, want II showing with I hidden", e)
	}
	for i := 0; i < 10*20; i++ {
		h.updateEffects(players)
	}
	e := pl.effects[effSpeed]
	if e == nil || e.amp != 0 {
		t.Fatalf("after Speed II ran out: %+v, want Speed I back", e)
	}
	if e.left < 49*20 || e.left > 50*20 {
		t.Errorf("Speed I came back with %d ticks, want about fifty seconds", e.left)
	}
	// A weaker, longer one goes under a stronger one without showing.
	h.applyEffect(players, pl, effStrength, 1, 10)
	h.applyEffect(players, pl, effStrength, 0, 60)
	if s := pl.effects[effStrength]; s.amp != 1 || s.hidden == nil || s.hidden.amp != 0 {
		t.Errorf("Strength I did not wait under Strength II: %+v", s)
	}
}
