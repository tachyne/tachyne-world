package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func stat(t *tracked, name string) int32 {
	if t.stats == nil {
		return 0
	}
	return t.stats[statKey{attachproto.StatCustom, customStatID[name]}]
}

// Walking, sneaking, sprinting, jumping and falling each land in their own
// vanilla counter, in centimetres.
func TestMovementStatisticsFamily(t *testing.T) {
	h := newHub(world.New(1))
	pl := riderAt(1, 100.5, 70, 100.5)
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	pl.onGround = true
	pl.stats = map[statKey]int32{}
	// A ground move of one block: walk.
	h.moveStats(pl, evMove{x: 101.5, y: 70, z: 100.5, onGround: true})
	if got := stat(pl, "walk_one_cm"); got != 100 {
		t.Errorf("walk_one_cm %d, want 100", got)
	}
	pl.p.sneaking = true
	h.moveStats(pl, evMove{x: 101.5, y: 70, z: 100.5, onGround: true})
	if got := stat(pl, "crouch_one_cm"); got != 100 {
		t.Errorf("crouch_one_cm %d, want 100", got)
	}
	pl.p.sneaking = false
	h.moveStats(pl, evMove{x: 101.5, y: 70, z: 100.5, onGround: true, sprinting: true})
	if got := stat(pl, "sprint_one_cm"); got != 100 {
		t.Errorf("sprint_one_cm %d, want 100", got)
	}
	// Leaving the ground upward is a jump; dropping two blocks is a fall of 200 cm.
	h.moveStats(pl, evMove{x: 100.5, y: 70.4, z: 100.5, onGround: false})
	if got := stat(pl, "jump"); got != 1 {
		t.Errorf("jump %d, want 1", got)
	}
	pl.airborne, pl.onGround = true, false
	pl.y = 72
	h.moveStats(pl, evMove{x: 100.5, y: 70, z: 100.5, onGround: false})
	if got := stat(pl, "fall_one_cm"); got != 200 {
		t.Errorf("fall_one_cm %d, want 200", got)
	}
	// A teleport is not a walk.
	pl.onGround = true
	h.moveStats(pl, evMove{x: 200.5, y: 70, z: 100.5, onGround: true})
	if got := stat(pl, "walk_one_cm"); got != 100 {
		t.Errorf("a 100-block move must not count as walking: %d", got)
	}
}

// Damage taken is recorded in tenths after mitigation, the mitigated part as
// resisted, and a shield's share as blocked.
func TestDamageStatistics(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.stats = map[statKey]int32{}
	h.hurtBy(nil, pl, 4, dtGeneric, deathCause{})
	if got := stat(pl, "damage_taken"); got != 40 {
		t.Errorf("damage_taken %d, want 40 (4 hearts × 10)", got)
	}
	pl.absorption = 2
	h.hurtBy(nil, pl, 3, dtGeneric, deathCause{})
	if got := stat(pl, "damage_absorbed"); got != 20 {
		t.Errorf("damage_absorbed %d, want 20", got)
	}
	if got := stat(pl, "damage_taken"); got != 50 {
		t.Errorf("damage_taken after absorption %d, want 50", got)
	}
}

// Jump Boost raises the safe fall distance by one block per level.
func TestJumpBoostRaisesSafeFallDistance(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	h.rules.FallDamage = true
	drop := func(blocks float64) float32 {
		pl.health, pl.dead = 20, false
		pl.airborne, pl.peakY = true, 100
		pl.x, pl.y, pl.z = 0.5, 100, 0.5
		h.world.SetBlock(0, int(100-blocks)-1, 0, worldgen.Stone)
		h.onFallAndExhaust(nil, pl, evMove{x: 0.5, y: 100 - blocks, z: 0.5, onGround: true})
		return 20 - pl.health
	}
	if d := drop(4); d != 1 {
		t.Fatalf("a four-block fall deals %v, want 1", d)
	}
	pl.effects[effJumpBoost] = &activeEffect{amp: 1, left: 100} // Jump Boost II: grace 3 + 2
	if d := drop(4); d != 0 {
		t.Errorf("with Jump Boost II a four-block fall should be free, dealt %v", d)
	}
	if d := drop(7); d != 2 {
		t.Errorf("with Jump Boost II a seven-block fall deals %v, want 2", d)
	}
}
