package worldgen

import "testing"

// The Nether has vanilla's bedrock roof at y=127 with netherrack under it and
// air over it — except over a player's build on the old ceiling, which
// keeps its open void.
func TestNetherRoof(t *testing.T) {
	g := NewNetherGenerator(7)
	ch := g.GenerateChunk(0, 0)
	gradient := 0
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			if s := sectionBlockAt(ch, lx, NetherRoof, lz); s != Bedrock {
				t.Fatalf("roof at %d,%d is %d", lx, lz, s)
			}
			if s := sectionBlockAt(ch, lx, NetherRoof+1, lz); s != Air {
				t.Fatalf("above the roof at %d,%d is %d", lx, lz, s)
			}
			for y := NetherCeiling + 1; y < NetherRoof; y++ {
				switch sectionBlockAt(ch, lx, y, lz) {
				case Air, Lava:
					t.Fatalf("hole under the roof at %d,%d,%d", lx, y, lz)
				case Bedrock:
					gradient++
					if y <= NetherRoof-5 {
						t.Fatalf("bedrock below the gradient at y=%d", y)
					}
				}
			}
		}
	}
	if gradient < 256 || gradient > 4*256 {
		t.Errorf("%d gradient bedrock in 4×256 cells (want about half)", gradient)
	}
	g2 := NewNetherGenerator(7)
	setEditOverlay(g2, map[[3]int]uint32{{5, NetherCeiling + 1, 5}: BlockBase("cobblestone")})
	ch = g2.GenerateChunk(0, 0)
	for _, c := range []struct {
		x, z int
		open bool
	}{{5, 5, true}, {7, 3, true}, {8, 5, false}, {5, 12, false}} {
		got := sectionBlockAt(ch, c.x, NetherRoof, c.z) == Air
		if got != c.open {
			t.Errorf("column %d,%d open %v, want %v", c.x, c.z, got, c.open)
		}
	}
}
