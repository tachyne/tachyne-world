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
	if !seen[OakLog] {
		t.Error("expected oak_log somewhere in the region")
	}
	if !seen[ShortGrass] {
		t.Error("expected short_grass somewhere in the region")
	}
	// Oak leaves appear — at ANY distance state. The old assertion demanded
	// the exact distance-7 state, which is now correctly absent: placement
	// seeds every leaf with its true distance from the trunk, and a healthy
	// oak's canopy sits entirely within 6. A distance-7 leaf is one about to
	// decay, so a fresh region must contain leaves and none of them at 7.
	leafLo, leafHi := BlockRange("oak_leaves")
	anyLeaf, decaying := false, 0
	for s := range seen {
		if s < leafLo || s > leafHi {
			continue
		}
		anyLeaf = true
		idx := s - leafLo // distance x persistent x waterlogged, waterlogged innermost
		if idx/4 == 6 && (idx/2)%2 == 1 {
			decaying++
		}
	}
	if !anyLeaf {
		t.Error("expected oak_leaves somewhere in the region")
	}
	if decaying > 0 {
		t.Errorf("%d oak-leaf state(s) at distance 7 non-persistent — freshly grown canopies would rot", decaying)
	}
}

// TestFeaturesDeterministic: trees/grass are a pure function of seed+coords, so
// a chunk regenerates identically (this is what makes re-streaming stable).
func TestFeaturesDeterministic(t *testing.T) {
	a := NewGenerator(7).GenerateChunk(2, 3)
	b := NewGenerator(7).GenerateChunk(2, 3)
	if !a.Equal(b) {
		t.Fatal("decorated chunk is not reproducible")
	}
}
