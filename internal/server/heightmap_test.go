package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// motionBlockingTop reads the stored MOTION_BLOCKING heightmap, which the
// hub's own block writes keep current: a roof placed and taken away moves
// the column's top, and a snow layer (not in #blocks_motion_in_heightmap)
// does not.
func TestMotionBlockingTopFollowsBuilds(t *testing.T) {
	h := newTestHub(world.New(1))
	players := map[int32]*tracked{}
	x, z := 40, 40
	h.world.ForceLoad(x, z, 1)
	base := h.motionBlockingTop(dimOverworld, x, z)
	h.setBlockAt(players, dimOverworld, blockPos{x, 300, z}, worldgen.Stone)
	if got := h.motionBlockingTop(dimOverworld, x, z); got != 301 {
		t.Fatalf("a roof at 300: top %d, want 301", got)
	}
	h.setBlockAt(players, dimOverworld, blockPos{x, 301, z}, snowLayer1)
	if got := h.motionBlockingTop(dimOverworld, x, z); got != 301 {
		t.Errorf("a snow layer raised MOTION_BLOCKING to %d", got)
	}
	if got := h.worldSurface(x, z); got != 302 {
		t.Errorf("WORLD_SURFACE over the snow: %d, want 302", got)
	}
	h.setBlockAt(players, dimOverworld, blockPos{x, 301, z}, worldgen.Air)
	h.setBlockAt(players, dimOverworld, blockPos{x, 300, z}, worldgen.Air)
	if got := h.motionBlockingTop(dimOverworld, x, z); got != base {
		t.Errorf("the roof is gone: top %d, want the ground's %d", got, base)
	}
}
