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
	// The city spans many chunks (real jigsaw templates); scan its footprint for
	// the signature reinforced-deepslate frame + loot chests.
	reinf := blockBase("reinforced_deepslate")
	lo, hi := BlockRange("chest")
	reinfN, chestN := 0, 0
	pcx, pcz := int32(city.X>>4), int32(city.Z>>4)
	for cx := pcx - 4; cx <= pcx+4; cx++ {
		for cz := pcz - 4; cz <= pcz+4; cz++ {
			ch := g.GenerateChunk(cx, cz)
			for s := range ch.Sections {
				for _, b := range ch.Sections[s] {
					if b == reinf {
						reinfN++
					} else if b >= lo && b <= hi {
						chestN++
					}
				}
			}
		}
	}
	t.Logf("city at %d,%d,%d: reinforced=%d chests=%d (routed=%d)",
		city.X, city.Y, city.Z, reinfN, chestN, len(g.AncientCityChests(city)))
	if reinfN == 0 {
		t.Fatal("ancient city should stamp its reinforced-deepslate frame from the templates")
	}
	if len(g.AncientCityChests(city)) == 0 {
		t.Fatal("ancient city should carry loot chests")
	}
}
