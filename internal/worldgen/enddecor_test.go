package worldgen

import "testing"

// The outer End: highlands chunks carry their biome and grow chorus; the
// small islands' biome floats end stone in the void; a chorus plant's cells
// join up and end in flowers.
func TestOuterEndGrows(t *testing.T) {
	g := NewEndGenerator(3)
	// a highlands chunk with land under it
	var hx, hz int32
	found := false
	for r := 64; r < 100 && !found; r++ {
		for cx := -r; cx <= r && !found; cx++ {
			for _, cz := range []int{-r, r} {
				x, z := cx*16+8, cz*16+8
				if _, _, ok := g.EndOuterColumn(x, z); ok && g.endBiome(x, z) == "minecraft:end_highlands" {
					hx, hz, found = int32(cx), int32(cz), true
					break
				}
			}
		}
	}
	if !found {
		t.Skip("no highlands land within reach for this seed")
	}
	plants, flowers := 0, 0
	for dcx := int32(-2); dcx <= 2; dcx++ {
		for dcz := int32(-2); dcz <= 2; dcz++ {
			ch := g.generateEndChunk(hx+dcx, hz+dcz)
			if dcx == 0 && dcz == 0 && ch.Biomes[4] != "minecraft:end_highlands" {
				t.Errorf("highlands chunk labelled %q", ch.Biomes[4])
			}
			for s := range ch.Sections {
				for _, b := range ch.Sections[s] {
					switch {
					case isChorusPlantState(b):
						plants++
					case isChorusFlowerState(b):
						flowers++
					}
				}
			}
		}
	}
	if plants == 0 || flowers == 0 {
		t.Errorf("chorus plants %d flowers %d in twenty-five highlands chunks", plants, flowers)
	}
	// a plant stamped directly joins up
	ch := NewChunk(SectionCount)
	reg := &endRegion{g: g, ch: ch, baseX: 100000, baseZ: 100000}
	for dx := 0; dx < 16; dx++ {
		for dz := 0; dz < 16; dz++ {
			setSectionBlock(ch, dx, 60, dz, EndStone, true)
		}
	}
	g.chorusPlant(newTreeRNG(3, 1, 1), reg, 100008, 61, 100008)
	base := sectionBlockAt(ch, 8, 61, 8)
	info, _ := InfoForState(base)
	if !isChorusPlantState(base) || GetProperty(info, base, "down") != "true" || GetProperty(info, base, "up") != "true" {
		t.Errorf("the plant's base is not joined down and up: %d", base)
	}
	// a small island in the void
	ch2 := NewChunk(SectionCount)
	reg2 := &endRegion{g: g, ch: ch2, baseX: 200000, baseZ: 200000}
	g.endIsland(newTreeRNG(3, 2, 2), reg2, 200008, 60, 200008)
	stone := 0
	for s := range ch2.Sections {
		for _, b := range ch2.Sections[s] {
			if b == EndStone {
				stone++
			}
		}
	}
	if stone < 40 {
		t.Errorf("small island stone %d", stone)
	}
}
