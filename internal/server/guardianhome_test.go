package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// ElderGuardian: its first update makes where it is its home, sixteen
// blocks around; carried out of it, it swims back
// (MoveTowardsRestrictionGoal).
func TestElderGuardianSwimsHome(t *testing.T) {
	h := newHub(world.New(5))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.world.ForceLoad(16, 0, 2)
	water := worldgen.BlockBase("water")
	for x := -2; x <= 34; x++ {
		for z := -1; z <= 1; z++ {
			for y := 150; y <= 152; y++ {
				h.world.SetBlock(x, y, z, water)
			}
		}
	}
	pl := survPlayer(h)
	pl.gamemode = gmCreative
	pl.x, pl.y, pl.z = 16.5, 160, 0.5
	players[pl.p.eid] = pl
	m := h.spawnHostileY(players, entityElderGuardian, 1.5, 151, 0.5)
	h.tick.Add(mobMoveInterval)
	h.updateMobs(players)
	if m.homeR != elderHomeRadius || abs(m.homePos.x-floorInt(m.x)) > 1 {
		t.Fatalf("the elder takes where it is as home: %+v r=%d", m.homePos, m.homeR)
	}
	m.x, m.sx = 30.5, 30.5 // carried off
	for i := 0; i < 300 && m.x > float64(m.homePos.x+elderHomeRadius); i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
	}
	if m.x > float64(m.homePos.x+elderHomeRadius) {
		t.Errorf("an elder guardian outside its home swims back, still at x=%.1f", m.x)
	}
}
