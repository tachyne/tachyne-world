package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestBreakExperienceAndInfested: a spawner and the sculk blocks pay
// experience when mined, and the infested blocks are recognised.
func TestBreakExperienceAndInfested(t *testing.T) {
	rng := func(n int) int { return n - 1 }
	if got := xpForBlock(spawnerBlock, rng); got != 15+14+14 {
		t.Fatalf("spawner xp %d", got)
	}
	for _, n := range []string{"sculk_sensor", "calibrated_sculk_sensor", "sculk_shrieker", "sculk_catalyst"} {
		if got := xpForBlock(worldgen.BlockBase(n), rng); got != 5 {
			t.Fatalf("%s xp %d, want 5", n, got)
		}
	}
	if xpForBlock(worldgen.Stone, rng) != 0 {
		t.Fatal("stone pays nothing")
	}
	if !isInfested(worldgen.BlockBase("infested_stone_bricks")) || isInfested(worldgen.BlockBase("stone_bricks")) {
		t.Fatal("infested detection")
	}
}
