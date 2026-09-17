package worldgen

import "testing"

func findDripstoneChunk(g *Generator) (int32, int32, bool) {
	for r := 0; r < 96; r++ {
		for cx := -r; cx <= r; cx++ {
			for _, cz := range []int{-r, r} {
				x, z := cx*16+8, cz*16+8
				if g.caveBiome(x, z, -10) == "minecraft:dripstone_caves" && g.Height(x, z)-24 > -10 {
					return int32(cx), int32(cz), true
				}
			}
		}
	}
	return 0, 0, false
}

// Dripstone caves grow dripstone: blocks and pointed spikes, the spikes
// pointing the right way from the rock they hang from or stand on.
func TestDripstoneCavesGrow(t *testing.T) {
	g := NewGenerator(5)
	cx, cz, ok := findDripstoneChunk(g)
	if !ok {
		t.Skip("no dripstone caves near the origin for this seed")
	}
	lo, hi := BlockRange("pointed_dripstone")
	blocks, points, wrong := 0, 0, 0
	for dcx := int32(-2); dcx <= 2; dcx++ {
		for dcz := int32(-2); dcz <= 2; dcz++ {
			ch := g.GenerateChunk(cx+dcx, cz+dcz)
			for s := range ch.Sections {
				for i, b := range ch.Sections[s] {
					switch {
					case b == DripstoneBlock:
						blocks++
					case b >= lo && b <= hi:
						points++
						info, _ := InfoForState(b)
						lx, ly, lz := i%16, MinY+s*16+i/256, (i/16)%16
						if GetProperty(info, b, "thickness") == "base" {
							dir := GetProperty(info, b, "vertical_direction")
							root := sectionBlockAt(ch, lx, ly+1, lz)
							if dir == "up" {
								root = sectionBlockAt(ch, lx, ly-1, lz)
							}
							if root == Air || root == Water {
								wrong++
							}
						}
					}
				}
			}
		}
	}
	if blocks == 0 || points == 0 {
		t.Errorf("dripstone blocks %d, pointed %d in twenty-five dripstone chunks", blocks, points)
	}
	if wrong > 0 {
		t.Errorf("%d pointed dripstone bases hang in the air", wrong)
	}
}
