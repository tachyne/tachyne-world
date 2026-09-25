package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Hurt by something that is not a living thing (a cactus, fire, a fall) an
// enderman blinks away nine times in ten; a player's blow does not move it.
func TestEndermanTeleportsFromNonLivingHurt(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	moved := func(dt dmgType) int {
		n := 0
		for i := 0; i < 20; i++ {
			m := h.spawnHostileY(players, entityEnderman, 0.5, float64(h.world.SurfaceY(0, 0)), 0.5)
			m.health = 1000
			x, z := m.x, m.z
			h.hurtMobOf(players, m, 1, dt)
			if m.x != x || m.z != z {
				n++
			}
			h.removeMob(players, m)
		}
		return n
	}
	// teleport() is one try at a random spot (it can find nowhere to land),
	// so not every roll moves it — but the prick sets it trying.
	if n := moved(dtCactus); n < 3 {
		t.Errorf("a cactus prick moved the enderman %d times in 20", n)
	}
	if n := moved(dtPlayerAttack); n != 0 {
		t.Errorf("a player's blow teleported the enderman %d times", n)
	}
}
