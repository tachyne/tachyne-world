package worldgen

import "testing"

func findBiomeChunk(g *Generator, want func(string) bool) (int32, int32, bool) {
	for r := 0; r < 96; r++ {
		for cx := -r; cx <= r; cx++ {
			for _, cz := range []int{-r, r} {
				if want(g.resolveBiome(cx*16+8, cz*16+8).Name) {
					return int32(cx), int32(cz), true
				}
			}
		}
	}
	return 0, 0, false
}

// Cold ground gets a snow layer over snowy grass; cold water freezes.
func TestFreezeTopLayer(t *testing.T) {
	g := NewGenerator(5)
	cx, cz, ok := findBiomeChunk(g, func(n string) bool { return n == "minecraft:snowy_plains" || n == "minecraft:snowy_taiga" })
	if !ok {
		t.Skip("no snowy plains within 96 chunks for this seed")
	}
	ch := g.GenerateChunk(cx, cz)
	snow := 0
	for s := range ch.Sections {
		for _, b := range ch.Sections[s] {
			if b == Snow {
				snow++
			}
		}
	}
	if snow == 0 {
		t.Error("no snow layers in a snowy chunk")
	}
	// Grass under the pass turns snowy under its layer (the engine's snowy
	// biomes top out in snow blocks, so a grass cell is planted for the check).
	y := MinY + len(ch.Sections)*16 - 1
	for y > MinY && sectionBlockAt(ch, 3, y, 3) == Air {
		y--
	}
	setSectionBlock(ch, 3, y, 3, GrassBlock, true)
	setSectionBlock(ch, 3, y+1, 3, Air, true)
	g.freezeTopLayer(ch, cx, cz)
	if sectionBlockAt(ch, 3, y+1, 3) != Snow {
		t.Errorf("no snow layer over cold grass: %d", sectionBlockAt(ch, 3, y+1, 3))
	}
	if info, ok := InfoForState(sectionBlockAt(ch, 3, y, 3)); !ok || GetProperty(info, sectionBlockAt(ch, 3, y, 3), "snowy") != "true" {
		t.Errorf("grass under the layer is not snowy: %d", sectionBlockAt(ch, 3, y, 3))
	}
	if cx, cz, ok := findBiomeChunk(g, isFrozenOcean); ok {
		ch := g.GenerateChunk(cx, cz)
		ice := 0
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				if sectionBlockAt(ch, lx, SeaLevel-1, lz) == Ice {
					ice++
				}
			}
		}
		if ice == 0 {
			t.Error("a frozen ocean's surface did not freeze")
		}
	}
	// A warm biome stays bare.
	cx, cz, ok = findBiomeChunk(g, func(n string) bool { return n == "minecraft:desert" })
	if ok {
		ch := g.GenerateChunk(cx, cz)
		for s := range ch.Sections {
			for _, b := range ch.Sections[s] {
				if b == Snow || b == Ice {
					t.Fatal("snow or ice in a desert")
				}
			}
		}
	}
}

// Frozen oceans raise icebergs: packed ice appears across a stretch of them.
func TestIcebergsInFrozenOceans(t *testing.T) {
	g := NewGenerator(5)
	cx, cz, ok := findBiomeChunk(g, isFrozenOcean)
	if !ok {
		t.Skip("no frozen ocean within 96 chunks for this seed")
	}
	packed := 0
	for dcx := int32(-6); dcx <= 6 && packed == 0; dcx++ {
		for dcz := int32(-6); dcz <= 6 && packed == 0; dcz++ {
			if !isFrozenOcean(g.resolveBiome(int(cx+dcx)*16+8, int(cz+dcz)*16+8).Name) {
				continue
			}
			ch := g.GenerateChunk(cx+dcx, cz+dcz)
			for s := range ch.Sections {
				for _, b := range ch.Sections[s] {
					if b == PackedIce {
						packed++
					}
				}
			}
		}
	}
	if packed == 0 {
		t.Error("no packed ice in 169 frozen-ocean chunks")
	}
}

// A fossil stamps its bones (and ore) into the rock under a desert.
func TestFossilStamps(t *testing.T) {
	g := NewGenerator(5)
	if TemplateByName("fossil/spine_1") == nil {
		t.Fatal("fossil templates are not baked")
	}
	cx, cz, ok := findBiomeChunk(g, func(n string) bool { return n == "minecraft:desert" })
	if !ok {
		t.Skip("no desert within 96 chunks for this seed")
	}
	ch := g.GenerateChunk(cx, cz)
	reg := &owRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{}}
	r := newTreeRNG(g.seed, int(cx)*16, int(cz)*16)
	g.fossil(r, reg, int(cx)*16+8, 40, int(cz)*16+8, false)
	boneLo, boneHi := BlockRange("bone_block")
	bones := 0
	for s := range ch.Sections {
		for _, b := range ch.Sections[s] {
			if b >= boneLo && b <= boneHi {
				bones++
			}
		}
	}
	if bones == 0 {
		t.Error("no bone blocks after stamping a fossil")
	}
}

// Infested stone shows up in the mountains and nowhere else.
func TestInfestedStoneInMountains(t *testing.T) {
	g := NewGenerator(5)
	cx, cz, ok := findBiomeChunk(g, func(n string) bool { return infestedBiomes[n] })
	if !ok {
		t.Skip("no infested biome within 96 chunks for this seed")
	}
	n := 0
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			ch := g.GenerateChunk(cx+dcx, cz+dcz)
			for s := range ch.Sections {
				for _, b := range ch.Sections[s] {
					if b == InfestedStone || b == InfestedDeepslate {
						n++
					}
				}
			}
		}
	}
	if n == 0 {
		t.Error("no infested stone in nine mountain chunks")
	}
}
