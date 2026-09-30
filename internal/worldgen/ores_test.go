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
	skipHeavy(t)
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

// TestEmeraldMountainsOnly: emerald ore only appears within a blob's reach
// of a mountain column (the biome filter asks the origin's column), and it
// does appear where mountains exist.
func TestEmeraldMountainsOnly(t *testing.T) {
	skipHeavy(t)
	g := NewGenerator(1)
	nearMountain := func(x, z int) bool {
		for dx := -3; dx <= 3; dx++ {
			for dz := -3; dz <= 3; dz++ {
				if mountainBiomes[g.resolveBiome(x+dx, z+dz).Name] {
					return true
				}
			}
		}
		return false
	}
	mountainChunks, emeraldTotal, emeraldOffMountain := 0, 0, 0
	for cx := int32(-20); cx <= 20; cx++ {
		for cz := int32(-20); cz <= 20; cz++ {
			if mountainBiomes[g.resolveBiome(int(cx)*16+8, int(cz)*16+8).Name] {
				mountainChunks++
			}
			ch := g.GenerateChunk(cx, cz)
			for sec := range ch.Sections {
				for i, s := range ch.Sections[sec] {
					if s == EmeraldOre || s == DeepslateEmeraldOre {
						emeraldTotal++
						if !nearMountain(int(cx)*16+i&15, int(cz)*16+(i>>4)&15) {
							emeraldOffMountain++
						}
					}
				}
			}
		}
	}
	if emeraldOffMountain != 0 {
		t.Errorf("%d emerald ore blocks generated away from any mountain column", emeraldOffMountain)
	}
	if mountainChunks > 0 && emeraldTotal == 0 {
		t.Errorf("%d mountain chunks but no emerald generated", mountainChunks)
	}
	t.Logf("mountain chunks=%d emerald=%d", mountainChunks, emeraldTotal)
}

// TestOreDepthVariants: ore below the deepslate transition must use the
// deepslate variant (it replaced deepslate, not stone), and vice versa. The
// lower granite, diorite and andesite blobs start at y=0 and reach seven
// below it, into the deepslate, and an ore in them takes the stone variant.
func TestOreDepthVariants(t *testing.T) {
	g := NewGenerator(1)
	for cx := int32(-2); cx <= 2; cx++ {
		ch := g.GenerateChunk(cx, 0)
		for s := range ch.Sections {
			for i, st := range ch.Sections[s] {
				y := MinY + s*16 + i/256
				switch st {
				case CoalOre, IronOre, CopperOre, GoldOre, DiamondOre:
					if y < -7 {
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

// A soil blob with a build near it is not placed; the others still are.
// Through placeOres, on a chunk of solid stone.
func TestSoilBlobBuildGuard(t *testing.T) {
	stoneChunk := func() *Chunk {
		ch := NewChunk(SectionCount)
		for s := range ch.Sections {
			for i := range ch.Sections[s] {
				ch.Sections[s][i] = Stone
			}
		}
		return ch
	}
	count := func(ch *Chunk, b uint32) (n int, at [3]int) {
		for s := range ch.Sections {
			for i, v := range ch.Sections[s] {
				if v == b {
					n++
					at = [3]int{i & 15, MinY + s*16 + i>>8, (i >> 4) & 15}
				}
			}
		}
		return
	}
	g := NewGenerator(7)
	ch := stoneChunk()
	g.placeOres(ch, 3, 5)
	dirt, at := count(ch, Dirt)
	gravel, _ := count(ch, Gravel)
	if dirt == 0 || gravel == 0 {
		t.Fatalf("no blobs in solid stone: dirt %d gravel %d", dirt, gravel)
	}
	g2 := NewGenerator(7)
	setTestEdits(g2, map[[3]int]uint32{{3*16 + at[0], at[1], 5*16 + at[2]}: BlockBase("stone_bricks")})
	ch2 := stoneChunk()
	g2.placeOres(ch2, 3, 5)
	if s := sectionBlockAt(ch2, at[0], at[1], at[2]); s != Stone {
		t.Errorf("blob cell beside a build is %d, want stone", s)
	}
	if dirt2, _ := count(ch2, Dirt); dirt2 == 0 || dirt2 >= dirt {
		t.Errorf("dirt %d → %d: want only the guarded blob gone", dirt, dirt2)
	}
}

// 26.3's ore step: every placement the old eleven specs lacked is there,
// with vanilla's size, count and discard chance.
func TestOrePlacements(t *testing.T) {
	want := map[string]struct {
		size, count, rarity int
		discard             float64
	}{
		"ore_coal_upper": {17, 30, 0, 0}, "ore_coal_lower": {17, 20, 0, 0.5},
		"ore_iron_upper": {9, 90, 0, 0}, "ore_iron_middle": {9, 10, 0, 0}, "ore_iron_small": {4, 10, 0, 0},
		"ore_copper": {10, 16, 0, 0}, "ore_copper_large": {20, 16, 0, 0},
		"ore_gold": {9, 4, 0, 0.5}, "ore_gold_extra": {9, 50, 0, 0}, "ore_gold_lower": {9, 0, 0, 0.5},
		"ore_diamond": {4, 7, 0, 0.5}, "ore_diamond_medium": {8, 2, 0, 0.5},
		"ore_diamond_large": {12, 0, 9, 0.7}, "ore_diamond_buried": {8, 4, 0, 1},
		"ore_lapis_buried": {7, 4, 0, 1}, "ore_granite_upper": {64, 0, 6, 0},
	}
	seen := map[string]bool{}
	salts := map[int64]bool{}
	for _, p := range orePlacements {
		if salts[p.salt] {
			t.Errorf("%s shares a salt", p.name)
		}
		salts[p.salt] = true
		seen[p.name] = true
		if w, ok := want[p.name]; ok && (w.size != p.cfg.size || w.count != p.count || w.rarity != p.rarity || w.discard != p.cfg.discard) {
			t.Errorf("%s: size %d count %d rarity %d discard %v, want %+v", p.name, p.cfg.size, p.count, p.rarity, p.cfg.discard, w)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("no %s placement", name)
		}
	}
	if len(orePlacements) != 30 {
		t.Errorf("%d ore placements, want 30", len(orePlacements))
	}
}

// OreFeature's shape: one blob is a single lump of cells near its origin,
// and with discard_chance_on_air_exposure 1 no ore cell touches air while
// at 0 the same blob fills its exposed cells too.
func TestOreBlobShapeAndDiscard(t *testing.T) {
	g := NewGenerator(3)
	porous := func() *owRegion { // stone with every third layer open
		ch := NewChunk(SectionCount)
		for s := range ch.Sections {
			for i := range ch.Sections[s] {
				if (MinY+s*16+i>>8)%3 != 0 {
					ch.Sections[s][i] = Stone
				}
			}
		}
		return &owRegion{g: g, ch: ch, baseX: 0, baseZ: 0, cols: map[[2]int]column{}}
	}
	top := MinY + SectionCount*16
	place := func(discard float64) (*owRegion, int) {
		reg := porous()
		cfg := oreOf(CoalOre, DeepslateCoalOre, 17, discard)
		oreBlob(reg, nil, cfg, newTreeRNG(11, 0, 0), 8, 40, 8, top, 11, 1)
		n := 0
		for s := range reg.ch.Sections {
			for i, v := range reg.ch.Sections[s] {
				if v != CoalOre {
					continue
				}
				n++
				x, y, z := i&15, MinY+s*16+i>>8, (i>>4)&15
				if abs(x-8) > 6 || abs(z-8) > 6 || y < 40-6 || y > 40+4 {
					t.Errorf("discard %v: ore at (%d,%d,%d), far from its origin", discard, x, y, z)
				}
			}
		}
		return reg, n
	}
	reg, n := place(1)
	for s := range reg.ch.Sections {
		for i, v := range reg.ch.Sections[s] {
			if v == CoalOre && oreNextToAir(reg, i&15, MinY+s*16+i>>8, (i>>4)&15) {
				t.Fatalf("discard 1: an ore cell touches air")
			}
		}
	}
	_, all := place(0)
	if all <= n || all == 0 {
		t.Errorf("discard 0 placed %d, discard 1 %d: want more with no discard", all, n)
	}
}
