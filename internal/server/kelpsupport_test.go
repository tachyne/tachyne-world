package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestGrowingPlantAnchors: GrowingPlantBlock.canSurvive holds a plant on its
// OWN head or body or a sturdy face; kelp never on magma
// (#cannot_support_kelp); and one plant does not hold another.
func TestGrowingPlantAnchors(t *testing.T) {
	w := world.New(1)
	x, y, z := 3, 150, 3
	kelp := worldgen.BlockBase("kelp")
	for _, c := range []struct {
		below string
		ok    bool
	}{{"stone", true}, {"magma_block", false}, {"kelp_plant", true}, {"twisting_vines_plant", false}} {
		w.SetBlock(x, y-1, z, worldgen.BlockBase(c.below))
		w.SetBlock(x, y, z, kelp)
		w.SetBlock(x, y+1, z, worldgen.WaterBase)
		if got := supported(w, blockPos{x, y, z}, kelp); got != c.ok {
			t.Errorf("kelp on %s: survives=%v, want %v", c.below, got, c.ok)
		}
	}
}
