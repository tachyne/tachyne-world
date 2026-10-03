package world

import (
	"strings"
	"sync"
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A vanilla world keys the chunk cache under GV. (and its preset), its
// chunks come from the vanilla generator through the world (blocks, light,
// heightmaps), and a native world of the same seed keeps its keys.
func TestVanillaWorldGenerates(t *testing.T) {
	native := New(9)
	before := native.cacheKey(1, 2)
	if err := native.SetGenerator(worldgen.GeneratorNative, worldgen.PresetNormal); err != nil {
		t.Fatal(err)
	}
	if got := native.cacheKey(1, 2); got != before {
		t.Fatalf("native key %q became %q", before, got)
	}
	w := New(9)
	if err := w.SetGenerator(worldgen.GeneratorVanilla, worldgen.PresetNormal); err != nil {
		t.Fatal(err)
	}
	if key := w.cacheKey(1, 2); !strings.HasPrefix(key, "GV.") || w.GeneratorMode() != worldgen.GeneratorVanilla {
		t.Fatalf("vanilla key %q (mode %v)", key, w.GeneratorMode())
	}
	amp := New(9)
	if err := amp.SetGenerator(worldgen.GeneratorVanilla, worldgen.PresetAmplified); err != nil {
		t.Fatal(err)
	}
	if key := amp.cacheKey(1, 2); !strings.HasPrefix(key, "GV.amplified.") {
		t.Fatalf("amplified key %q", key)
	}
	if w.At(16, worldgen.MinY, 32) != worldgen.Bedrock {
		t.Error("no bedrock at the floor")
	}
	if w.Light(1, 2) == nil {
		t.Error("no light for a vanilla chunk")
	}
	if h := w.HeightAt(MotionBlocking, 20, 40); h < worldgen.MinY || h > 320 {
		t.Errorf("heightmap %d", h)
	}
	// Chunks generate side by side on many goroutines.
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 3; j++ {
				w.Chunk(int32(i*3+j), int32(-i))
			}
		}(i)
	}
	wg.Wait()
}
