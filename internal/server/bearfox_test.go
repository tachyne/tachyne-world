package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// PolarBear's NearestAttackableTarget<Fox>: an adult bear goes after a fox
// it can see and bites it; a cub leaves foxes alone.
func TestPolarBearHuntsFoxes(t *testing.T) {
	for _, cub := range []bool{false, true} {
		h := newHub(world.New(1))
		players := map[int32]*tracked{}
		h.world.ForceLoad(0, 0, 2)
		for x := -4; x <= 8; x++ {
			for z := -3; z <= 3; z++ {
				h.world.SetBlock(x, 179, z, worldgen.Stone)
			}
		}
		bear := h.spawnMob(players, entityPolarBear, 0.5, 180, 0.5)
		bear.baby = cub
		fox := h.spawnMob(players, entityFox, 2.5, 180, 0.5)
		fox.frozen = true // hold it where it stands
		fox.health = 1000
		h.gridDirty()
		bitten := false
		for i := 0; i < 80 && !bitten; i++ {
			h.updateMobs(players)
			bitten = fox.health < 1000
		}
		if bitten == cub {
			t.Errorf("cub=%v: fox bitten=%v", cub, bitten)
		}
	}
}
