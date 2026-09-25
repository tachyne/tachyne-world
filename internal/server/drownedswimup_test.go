package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// DrownedSwimUpGoal: after dark a drowned deep in the water rises toward
// the surface, to just under sea level; by day it stays down.
func TestDrownedSwimsUpAtNight(t *testing.T) {
	for _, night := range []bool{true, false} {
		h := newHub(world.New(5))
		players := map[int32]*tracked{}
		h.playersRef = players
		h.world.ForceLoad(1, 1, 1)
		water := worldgen.BlockBase("water")
		stone := worldgen.BlockBase("stone")
		top := worldgen.SeaLevel - 1
		for x := -1; x <= 3; x++ {
			for z := -1; z <= 3; z++ {
				for y := 39; y <= top+1; y++ {
					s := water
					switch {
					case y == 39 || x == -1 || x == 3 || z == -1 || z == 3:
						s = stone
					case y > top:
						s = worldgen.Air
					}
					if y > top && (x == -1 || x == 3 || z == -1 || z == 3) {
						s = worldgen.Air
					}
					h.world.SetBlock(x, y, z, s)
				}
			}
		}
		h.dayTime.Store(6000)
		if night {
			h.dayTime.Store(18000)
		}
		pl := survPlayer(h)
		pl.gamemode = gmCreative
		pl.x, pl.y, pl.z = 1.5, float64(top+1), 1.5
		players[pl.p.eid] = pl
		m := h.spawnHostileY(players, entityDrowned, 1.5, 41, 1.5)
		for i := 0; i < 220; i++ {
			h.tick.Add(mobMoveInterval)
			h.updateMobs(players)
		}
		switch {
		case night && m.y < float64(worldgen.SeaLevel-3):
			t.Errorf("at night the drowned should rise near the surface, still at y=%.1f", m.y)
		case !night && m.y > 50:
			t.Errorf("by day the drowned has no reason to rise, at y=%.1f", m.y)
		}
	}
}
