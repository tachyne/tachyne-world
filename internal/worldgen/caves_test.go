package worldgen

import "testing"

// TestCavesCarved: the deep stone band should be partly hollowed by caves —
// present (not zero) and not total swiss cheese.
func TestCavesCarved(t *testing.T) {
	g := NewGenerator(1)
	air, total := 0, 0
	for cx := int32(-3); cx <= 3; cx++ {
		for cz := int32(-3); cz <= 3; cz++ {
			ch := g.GenerateChunk(cx, cz)
			for y := 0; y <= 40; y++ { // well below any surface
				sec := (y - MinY) / 16
				ly := (y - MinY) % 16
				for lx := 0; lx < 16; lx++ {
					for lz := 0; lz < 16; lz++ {
						total++
						if ch.Sections[sec][(ly*16+lz)*16+lx] == Air {
							air++
						}
					}
				}
			}
		}
	}
	pct := 100 * float64(air) / float64(total)
	if pct < 2 || pct > 30 {
		t.Errorf("cave air in y[0..40] = %.1f%%, want a sane 2..30%%", pct)
	}
}
