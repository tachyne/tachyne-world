package worldgen

import "testing"

// TestDeterministic: the same seed must reproduce the same chunk exactly.
func TestDeterministic(t *testing.T) {
	a := NewGenerator(42).GenerateChunk(3, -7)
	b := NewGenerator(42).GenerateChunk(3, -7)
	if *a != *b {
		t.Fatal("same seed produced different chunks")
	}
	if c := NewGenerator(43).GenerateChunk(3, -7); *a == *c {
		t.Error("different seeds produced identical chunks")
	}
}

// TestColumnStructure: every column is bedrock at the bottom, solid up to the
// surface, then water or air above — never floating or hollow at the base.
func TestColumnStructure(t *testing.T) {
	g := NewGenerator(1)
	for _, wx := range []int{0, 100, -250, 999} {
		col := g.columnAt(wx, wx/2)
		if col.block(MinY) != Bedrock {
			t.Errorf("x=%d: bottom is %d, want bedrock", wx, col.block(MinY))
		}
		if got := col.block(col.h - 1); got == Air {
			t.Errorf("x=%d: surface block is air", wx)
		}
		above := col.block(col.h)
		if above != Air && above != Water {
			t.Errorf("x=%d: block above surface is %d, want air or water", wx, above)
		}
		if col.h < 5 || col.h > 250 {
			t.Errorf("x=%d: height %d out of range", wx, col.h)
		}
	}
}

// TestVariety: a generated region should contain a mix of materials, not a
// single block — a smoke test that the terrain is actually varied.
func TestVariety(t *testing.T) {
	g := NewGenerator(1)
	seen := map[uint32]bool{}
	for cx := int32(-2); cx <= 2; cx++ {
		for cz := int32(-2); cz <= 2; cz++ {
			ch := g.GenerateChunk(cx, cz)
			for _, sec := range ch.Sections {
				for _, s := range sec {
					seen[s] = true
				}
			}
		}
	}
	for _, want := range []uint32{Air, Stone, Bedrock} {
		if !seen[want] {
			t.Errorf("expected block %d somewhere in the region", want)
		}
	}
	if len(seen) < 4 {
		t.Errorf("only %d distinct blocks generated; terrain looks too uniform", len(seen))
	}
}
