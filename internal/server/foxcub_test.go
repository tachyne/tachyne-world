package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// FoxFollowParentGoal(1.25): a fox kit trots after the nearest adult fox.
func TestFoxKitFollowsAnAdult(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.world.ForceLoad(0, 0, 2)
	for x := -2; x <= 10; x++ {
		for z := -2; z <= 2; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	h.dayTime.Store(18000) // night: no fox is asleep
	kit := h.spawnMob(players, entityFox, 0.5, 180, 0.5)
	kit.baby = true
	adult := h.spawnMob(players, entityFox, 7.5, 180, 0.5)
	adult.frozen = true
	h.gridDirty()
	d0 := math.Hypot(adult.x-kit.x, adult.z-kit.z)
	for i := 0; i < 20; i++ {
		h.updateMobs(players)
		h.gridDirty()
	}
	if d := math.Hypot(adult.x-kit.x, adult.z-kit.z); d > d0-2 {
		t.Fatalf("the kit did not follow: %.1f → %.1f blocks", d0, d)
	}
}
