package worldgen

import "testing"

func TestAncientCityGenerates(t *testing.T) {
	g := NewGenerator(1)
	// Find a placed city by sampling cell centres over a wide area.
	var city AncientCity
	for i := 0; i < 60 && !city.Exists; i++ {
		for j := 0; j < 60 && !city.Exists; j++ {
			c := g.AncientCityIn(i*ancientCityCell+256, j*ancientCityCell+256)
			if c.Exists {
				city = c
			}
		}
	}
	if !city.Exists {
		t.Skip("no ancient city in the sampled region (deep_dark gating)")
	}
	// Generate the chunk holding the city centre and check for its signature blocks.
	cx, cz := int32(city.X>>4), int32(city.Z>>4)
	ch := g.GenerateChunk(cx, cz)
	reinf := blockBase("reinforced_deepslate")
	sculk, reinfN, chests := 0, 0, 0
	for s := range ch.Sections {
		for _, b := range ch.Sections[s] {
			switch {
			case b == wgSculk:
				sculk++
			case b == reinf:
				reinfN++
			case b == ChestNorth:
				chests++
			}
		}
	}
	t.Logf("city at %d,%d,%d: sculk=%d reinforced=%d chests=%d", city.X, city.Y, city.Z, sculk, reinfN, chests)
	if reinfN == 0 || sculk == 0 {
		t.Fatal("ancient city chunk should contain reinforced deepslate + sculk")
	}
}
