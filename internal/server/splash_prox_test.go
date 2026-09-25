package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestSplashHarmingScalesWithDistance: HealOrHarmMobEffect's instant effect
// is (int)(scale × (6 << amp)): a player at the edge of the splash takes
// less than one at its centre, and a scale that rounds to nothing does
// nothing.
func TestSplashHarmingScalesWithDistance(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	harm := []potEffect{{id: effInstantDamage, amp: 0}}
	pl.health = 20
	h.applyPotionAoE(players, pl, harm, 1, 1)
	full := 20 - pl.health
	h.tick.Add(20)
	pl.health = 20
	h.applyPotionAoE(players, pl, harm, 0.5, 1)
	half := 20 - pl.health
	if full != 6 || half != 3 {
		t.Fatalf("harming at the centre dealt %v and at half distance %v, want 6 and 3", full, half)
	}
	h.tick.Add(20)
	pl.health = 20
	h.applyPotionAoE(players, pl, harm, 0.1, 1) // (int)(0.6) = 0
	if pl.health != 20 {
		t.Fatalf("a glancing splash that rounds to nothing still hurt: %v", 20-pl.health)
	}
}
