package worldgen

import "testing"

// TestFeaturesAppear: a generated region should contain trees (logs + leaves)
// and ground cover, confirming the decoration pass runs and produces them.
func TestFeaturesAppear(t *testing.T) {
	g := NewGenerator(1)
	seen := map[uint32]bool{}
	for cx := int32(-5); cx <= 5; cx++ {
		for cz := int32(-5); cz <= 5; cz++ {
			ch := g.GenerateChunk(cx, cz)
			for _, sec := range ch.Sections {
				for _, b := range sec {
					seen[b] = true
				}
			}
		}
	}
	for name, id := range map[string]uint32{
		"oak_log":     OakLog,
		"oak_leaves":  OakLeaves,
		"short_grass": ShortGrass,
	} {
		if !seen[id] {
			t.Errorf("expected %s somewhere in the region", name)
		}
	}
}

// TestFeaturesDeterministic: trees/grass are a pure function of seed+coords, so
// a chunk regenerates identically (this is what makes re-streaming stable).
func TestFeaturesDeterministic(t *testing.T) {
	a := NewGenerator(7).GenerateChunk(2, 3)
	b := NewGenerator(7).GenerateChunk(2, 3)
	if *a != *b {
		t.Fatal("decorated chunk is not reproducible")
	}
}
