package worldgen

import "testing"

// oreCounts scans a generated chunk and tallies ore cells by state.
func oreCounts(ch *Chunk) map[uint32]int {
	counts := map[uint32]int{}
	ores := map[uint32]bool{
		CoalOre: true, DeepslateCoalOre: true, IronOre: true, DeepslateIronOre: true,
		CopperOre: true, DeepslateCopperOre: true, GoldOre: true, DeepslateGoldOre: true,
		DiamondOre: true, DeepslateDiamondOre: true,
	}
	for s := range ch.Sections {
		for _, st := range ch.Sections[s] {
			if ores[st] {
				counts[st]++
			}
		}
	}
	return counts
}

func TestOresGenerate(t *testing.T) {
	g := NewGenerator(1)
	total := map[uint32]int{}
	for cx := int32(-3); cx <= 3; cx++ {
		for cz := int32(-3); cz <= 3; cz++ {
			for st, n := range oreCounts(g.GenerateChunk(cx, cz)) {
				total[st] += n
			}
		}
	}
	// Over 49 chunks every ore family should appear (surface + deepslate pooled).
	families := map[string]int{
		"coal":    total[CoalOre] + total[DeepslateCoalOre],
		"copper":  total[CopperOre] + total[DeepslateCopperOre],
		"iron":    total[IronOre] + total[DeepslateIronOre],
		"gold":    total[GoldOre] + total[DeepslateGoldOre],
		"diamond": total[DiamondOre] + total[DeepslateDiamondOre],
	}
	for name, n := range families {
		if n == 0 {
			t.Errorf("no %s ore in 49 chunks", name)
		}
	}
	if families["coal"] <= families["diamond"] {
		t.Errorf("coal (%d) should be more common than diamond (%d)", families["coal"], families["diamond"])
	}
}

func TestOresDeterministic(t *testing.T) {
	a := oreCounts(NewGenerator(7).GenerateChunk(2, -5))
	b := oreCounts(NewGenerator(7).GenerateChunk(2, -5))
	for st, n := range a {
		if b[st] != n {
			t.Fatalf("ore counts differ across generations: state %d %d vs %d", st, n, b[st])
		}
	}
}

// TestOreDepthVariants: ore below the deepslate transition must use the
// deepslate variant (it replaced deepslate, not stone), and vice versa.
func TestOreDepthVariants(t *testing.T) {
	g := NewGenerator(1)
	for cx := int32(-2); cx <= 2; cx++ {
		ch := g.GenerateChunk(cx, 0)
		for s := range ch.Sections {
			for i, st := range ch.Sections[s] {
				y := MinY + s*16 + i/256
				switch st {
				case CoalOre, IronOre, CopperOre, GoldOre, DiamondOre:
					if y < 0 {
						t.Fatalf("stone-variant ore %d at y=%d (deepslate zone)", st, y)
					}
				case DeepslateCoalOre, DeepslateIronOre, DeepslateCopperOre, DeepslateGoldOre, DeepslateDiamondOre:
					if y >= 4 {
						t.Fatalf("deepslate-variant ore %d at y=%d (stone zone)", st, y)
					}
				}
			}
		}
	}
}
