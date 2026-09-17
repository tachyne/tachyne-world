package worldgen

import "testing"

// findLushColumn scans outward for a column whose caves at y=-10 are lush.
func findLushChunk(g *Generator) (int32, int32, bool) {
	for r := 0; r < 96; r++ {
		for cx := -r; cx <= r; cx++ {
			for _, cz := range []int{-r, r} {
				x, z := cx*16+8, cz*16+8
				if g.caveBiome(x, z, -10) == "minecraft:lush_caves" && g.Height(x, z)-24 > -10 {
					return int32(cx), int32(cz), true
				}
			}
		}
	}
	return 0, 0, false
}

// A lush cave grows: cave vines, moss, and something on the floor or
// ceiling across a stretch of lush chunks; glow lichen shows up underground.
func TestLushCavesGrow(t *testing.T) {
	g := NewGenerator(5)
	cx, cz, ok := findLushChunk(g)
	if !ok {
		t.Skip("no lush caves near the origin for this seed")
	}
	vines, moss, lichen := 0, 0, 0
	for dcx := int32(-3); dcx <= 3; dcx++ {
		for dcz := int32(-3); dcz <= 3; dcz++ {
			ch := g.GenerateChunk(cx+dcx, cz+dcz)
			for s := range ch.Sections {
				for _, b := range ch.Sections[s] {
					switch {
					case (b >= caveVinesLo && b <= caveVinesHi) || b == caveVinesBody || b == caveVinesBody-1:
						vines++
					case b == MossBlock || b == MossCarpet:
						moss++
					case b >= glowLichenLo && b <= glowLichenHi:
						lichen++
					}
				}
			}
		}
	}
	if vines == 0 {
		t.Error("no cave vines in forty-nine lush chunks")
	}
	if moss == 0 {
		t.Error("no moss in forty-nine lush chunks")
	}
	if lichen == 0 {
		t.Error("no glow lichen underground")
	}
}
