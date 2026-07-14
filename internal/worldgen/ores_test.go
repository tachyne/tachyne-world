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

// TestRedstoneLapisGeneration confirms the newly added ores appear in their
// vanilla bands and that redstone generates in its UNLIT state.
func TestRedstoneLapisGeneration(t *testing.T) {
	g := NewGenerator(1)
	litRedstone := blockBase("redstone_ore")
	litDeepRedstone := blockBase("deepslate_redstone_ore")
	var redstone, redstoneDeep, lapis, lapisMid, lit int
	for cx := int32(0); cx < 24; cx++ {
		for cz := int32(0); cz < 24; cz++ {
			ch := g.GenerateChunk(cx, cz)
			for sec := range ch.Sections {
				baseY := MinY + sec*16
				for idx, s := range ch.Sections[sec] {
					y := baseY + idx/256
					switch s {
					case RedstoneOre, DeepslateRedstoneOre:
						redstone++
						if y < -32 {
							redstoneDeep++
						}
					case litRedstone, litDeepRedstone:
						lit++ // the lit state must never be generated
					case LapisOre, DeepslateLapisOre:
						lapis++
						if y >= -32 && y <= 32 {
							lapisMid++
						}
					}
				}
			}
		}
	}
	if redstone == 0 || redstoneDeep == 0 {
		t.Errorf("redstone ore missing/shallow: total %d, deep %d", redstone, redstoneDeep)
	}
	if lit != 0 {
		t.Errorf("%d LIT redstone ore blocks generated (should all be unlit)", lit)
	}
	if lapis == 0 || lapisMid == 0 {
		t.Errorf("lapis ore missing: total %d, in core band %d", lapis, lapisMid)
	}
	t.Logf("redstone=%d (deep %d) lapis=%d (mid %d)", redstone, redstoneDeep, lapis, lapisMid)
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
