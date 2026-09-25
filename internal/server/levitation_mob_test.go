package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestLevitatingMobRises: a mob under Levitation floats up (LivingEntity.
// travel: dy eases toward 0.05 × level), and comes down when it ends.
func TestLevitatingMobRises(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	w.ForceLoad(0, 0, 2)
	for x := -3; x <= 3; x++ {
		for z := -3; z <= 3; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 200; y++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	cow := h.spawnMob(players, entityCow, 0.5, 180, 0.5)
	cow.statik = false
	h.applyMobEffectTicks(players, cow, effLevitation, 0, 200)
	for i := 0; i < 40; i++ {
		cow.vx, cow.vz = 0, 0
		h.updateMobs(players)
	}
	if cow.y < 181 {
		t.Fatalf("a levitating cow should rise, at y=%.2f", cow.y)
	}
}
