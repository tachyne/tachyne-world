package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestCactusSurvivesBesideNonSolidBlocks: CactusBlock.canSurvive refuses a
// neighbour that isSolid (the shape-size threshold), not one that merely
// collides — a carpet or a candle beside a cactus leaves it standing, a
// stone does not.
func TestCactusSurvivesBesideNonSolidBlocks(t *testing.T) {
	w := world.New(1)
	cactus := worldgen.BlockBase("cactus")
	for _, c := range []struct {
		name string
		ok   bool
	}{{"white_carpet", true}, {"candle", true}, {"stone", false}, {"oak_planks", false}} {
		x, y, z := 3, 180, 3
		w.SetBlock(x, y-1, z, worldgen.BlockBase("sand"))
		w.SetBlock(x, y+1, z, worldgen.Air)
		w.SetBlock(x, y, z, cactus)
		w.SetBlock(x+1, y, z, worldgen.BlockBase(c.name))
		if got := supported(w, blockPos{x, y, z}, cactus); got != c.ok {
			t.Errorf("cactus beside %s: survives=%v, want %v", c.name, got, c.ok)
		}
	}
}
