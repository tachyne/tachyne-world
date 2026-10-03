package world

import (
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A vanilla-mode Nether or End world generates the vanilla dimension
// through the world's own chunk path, at its true heights, and keys its
// cache apart from a native world of the same seed and from each other.
func TestVanillaDimensionWorlds(t *testing.T) {
	const seed = 1
	nn, _ := NewNether(seed, nil)
	vn, _ := NewNether(seed, nil)
	ve, _ := NewEnd(seed, nil)
	if !vn.UseVanillaDimension() || !ve.UseVanillaDimension() {
		t.Fatal("vanilla dimension not installed")
	}
	if ow := New(seed); ow.UseVanillaDimension() {
		t.Error("the overworld took a vanilla dimension")
	}
	keys := map[string]bool{nn.cacheKey(3, 4): true, vn.cacheKey(3, 4): true, ve.cacheKey(3, 4): true}
	if len(keys) != 3 || !strings.HasPrefix(vn.cacheKey(3, 4), "GV.n.") || !strings.HasPrefix(ve.cacheKey(3, 4), "GV.e.") {
		t.Errorf("cache keys collide or lack the vanilla tag: %v", keys)
	}
	ch := vn.Chunk(0, 0)
	if ch == nil {
		t.Fatal("no chunk")
	}
	floor, under := 0, 0
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			if vn.Block(x, 0, z) == worldgen.Bedrock {
				floor++
			}
			if vn.Block(x, -10, z) != worldgen.Air {
				under++
			}
		}
	}
	if floor < 250 || under != 0 {
		t.Errorf("vanilla Nether: %d bedrock cells at y=0, %d blocks under it", floor, under)
	}
	if b := vn.BiomeAt(8, 8); !strings.HasPrefix(b, "minecraft:") {
		t.Errorf("biome %q", b)
	}
	if y := ve.SurfaceY(0, 0); y < 40 || y > 100 {
		t.Errorf("vanilla End surface at the origin y=%v", y)
	}
}
