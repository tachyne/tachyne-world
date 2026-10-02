package world

import (
	"strings"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A native world keeps the chunk-cache keys it always had; a vanilla-caves
// world keys its own, so a shared cache never serves one the other's chunks.
func TestSetCavesKeysTheChunkCache(t *testing.T) {
	native := New(42)
	before := native.cacheKey(3, -4)
	native.SetCaves(worldgen.CavesNative)
	if got := native.cacheKey(3, -4); got != before || native.Caves() != worldgen.CavesNative {
		t.Fatalf("native key %q became %q", before, got)
	}
	vanilla := New(42)
	vanilla.SetCaves(worldgen.CavesVanilla)
	vanilla.SetCaves(worldgen.CavesVanilla) // twice: still one tag
	key := vanilla.cacheKey(3, -4)
	if vanilla.Caves() != worldgen.CavesVanilla || !strings.HasPrefix(key, "CV.") || strings.Count(key, "CV.") != 1 {
		t.Fatalf("vanilla key %q (mode %v)", key, vanilla.Caves())
	}
	if key == before {
		t.Fatal("vanilla and native worlds share a cache key")
	}
	// Through the world's generated chunks: the two worlds differ underground.
	differ := false
	nch, vch := native.generated(3, -4), vanilla.generated(3, -4)
	for s := 0; s < 6 && !differ; s++ { // y -64..31
		differ = nch.Sections[s] != vch.Sections[s]
	}
	if !differ {
		t.Fatal("a vanilla-caves world generated the native underground")
	}
}
