package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// movement_efficiency on a mob (set here through /attribute) lifts the soul
// sand slow-down toward none, as LivingEntity.getBlockSpeedFactor lerps it.
func TestMobMovementEfficiency(t *testing.T) {
	s, h, ps, logs := feedbackServer(t)
	var z *mob
	onHub(t, h, func() {
		h.world.SetBlock(4, 150, 4, worldgen.SoulSand)
		z = h.spawnMob(h.playersRef, entityZombie, 4.5, 151, 4.5)
		if f := h.mobSpeedFactor(z); f != 0.4 {
			t.Errorf("soul sand factor %v, want 0.4", f)
		}
	})
	s.handleCommand(ps["alice"], "attribute @e[type=zombie,limit=1] minecraft:movement_efficiency base set 0.5")
	settle(t, h, logs, "E1")
	onHub(t, h, func() {
		if f := h.mobSpeedFactor(z); math.Abs(f-0.7) > 1e-9 {
			t.Errorf("factor with movement_efficiency 0.5: %v, want 0.7", f)
		}
	})
}
