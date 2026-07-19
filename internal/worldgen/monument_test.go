package worldgen

import "testing"

// TestMonumentFootprint: the ported MonumentBuilding shell fills its 58×58
// footprint with the vanilla prismarine materials, keeps its floating decorative
// sea lanterns (the removeFloatingFragments ordering fix), and hides the 2×2×2
// gold treasure.
func TestMonumentFootprint(t *testing.T) {
	g := NewGenerator(1)
	var mon Monument
	for i := 0; i < 90 && !mon.Exists; i++ {
		for j := 0; j < 90; j++ {
			if m := g.MonumentIn(i*monumentCell+224, j*monumentCell+224); m.Exists {
				mon = m
				break
			}
		}
	}
	if !mon.Exists {
		t.Skip("no monument rolled for this seed")
	}
	gray, light, dark := blockBase("prismarine"), blockBase("prismarine_bricks"), blockBase("dark_prismarine")
	lamp, gold := blockBase("sea_lantern"), blockBase("gold_block")
	cnt := map[uint32]int{}
	for cx := (mon.X - 30) >> 4; cx <= (mon.X+30)>>4; cx++ {
		for cz := (mon.Z - 30) >> 4; cz <= (mon.Z+30)>>4; cz++ {
			ch := g.GenerateChunk(int32(cx), int32(cz))
			for s := range ch.Sections {
				for _, b := range ch.Sections[s] {
					cnt[b]++
				}
			}
		}
	}
	t.Logf("monument %d,%d,%d: prismarine=%d bricks=%d dark=%d lantern=%d gold=%d",
		mon.X, mon.Y, mon.Z, cnt[gray], cnt[light], cnt[dark], cnt[lamp], cnt[gold])
	if cnt[gray] < 2000 {
		t.Errorf("too little prismarine (%d) — shell missing", cnt[gray])
	}
	if cnt[light] < 1000 {
		t.Errorf("too few prismarine bricks (%d)", cnt[light])
	}
	if cnt[lamp] < 8 {
		t.Errorf("sea lanterns culled (%d) — floating-fragment ordering regressed", cnt[lamp])
	}
	if cnt[gold] != 8 {
		t.Errorf("gold treasure = %d, want 8", cnt[gold])
	}
}
