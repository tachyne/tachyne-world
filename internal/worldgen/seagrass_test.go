package worldgen

import "testing"

// 26.3's eight seagrass placements: their counts and the tall weight of
// the selector each one places, and every seagrass biome naming one.
func TestSeagrassPlacements(t *testing.T) {
	want := map[string][2]int{
		"seagrass_warm": {80, 30}, "seagrass_normal": {48, 30}, "seagrass_cold": {32, 30},
		"seagrass_river": {48, 40}, "seagrass_swamp": {64, 60}, "seagrass_deep_warm": {80, 80},
		"seagrass_deep": {48, 80}, "seagrass_deep_cold": {40, 80},
	}
	if len(seagrassPlacements) != len(want) {
		t.Fatalf("%d seagrass placements, want %d", len(seagrassPlacements), len(want))
	}
	salts := map[int64]bool{}
	for _, p := range seagrassPlacements {
		if w, ok := want[p.name]; !ok || w != [2]int{p.count, p.tall} {
			t.Errorf("%s: count %d tall %d, want %v", p.name, p.count, p.tall, w)
		}
		if salts[p.salt] {
			t.Errorf("%s shares a salt", p.name)
		}
		salts[p.salt] = true
	}
	for b, name := range seagrassByBiome {
		if _, ok := want[name]; !ok {
			t.Errorf("%s lists unknown placement %s", b, name)
		}
	}
	if seagrassByBiome["minecraft:warm_ocean"] != "seagrass_warm" || seagrassByBiome["minecraft:mangrove_swamp"] != "seagrass_swamp" {
		t.Error("warm ocean and mangrove swamp share the lukewarm and swamp placements")
	}
}

// One placement's attempts all fall about one column (in_square before
// count), so a chunk that lists only the deep ocean's placement grows its
// seagrass inside one 15×15 square, and only where the biome at the cell
// lists that same placement.
func TestSeagrassPatchesAboutOneColumn(t *testing.T) {
	g := NewGenerator(1)
	for _, c := range oceanChunks(g, map[string]bool{"minecraft:deep_ocean": true}, 24) {
		ox, oz := int(c[0])*16, int(c[1])*16
		only := true
		for _, d := range [5][2]int{{0, 0}, {15, 0}, {0, 15}, {15, 15}, {8, 8}} {
			if seagrassByBiome[g.resolveBiome(ox+d[0], oz+d[1]).Name] != "seagrass_deep" {
				only = false
			}
		}
		if !only {
			continue
		}
		var cells [][2]int
		put := func(x, y, z int, s uint32) {
			if s != TallSeagrassUpper {
				cells = append(cells, [2]int{x, z})
			}
		}
		g.decorateSeagrass(ox, oz, func(x, z int) bool { return true },
			func(x, z int) (int, bool) { return 40, true },
			func(x, y, z int) bool { return true }, put, nil)
		if len(cells) == 0 {
			t.Fatalf("chunk %v: no seagrass placed", c)
		}
		x0, x1, z0, z1 := cells[0][0], cells[0][0], cells[0][1], cells[0][1]
		for _, p := range cells {
			x0, x1, z0, z1 = min(x0, p[0]), max(x1, p[0]), min(z0, p[1]), max(z1, p[1])
			if got := seagrassByBiome[g.resolveBiome(p[0], p[1]).Name]; got != "seagrass_deep" {
				t.Errorf("seagrass at %v in a biome listing %q", p, got)
			}
		}
		if x1-x0 > 14 || z1-z0 > 14 {
			t.Errorf("chunk %v: seagrass spans %dx%d, want one patch within 15x15", c, x1-x0+1, z1-z0+1)
		}
		return
	}
	t.Skip("no chunk wholly in the deep ocean near the origin")
}

// Through GenerateChunk: ocean chunks grow seagrass, and none grows under a
// player's build standing in the water over the floor.
func TestSeagrassBuildGuard(t *testing.T) {
	g := NewGenerator(1)
	chunks := oceanChunks(g, map[string]bool{"minecraft:ocean": true, "minecraft:deep_ocean": true, "minecraft:lukewarm_ocean": true}, 8)
	count := func(c [2]int32) int {
		n := 0
		ch := g.GenerateChunk(c[0], c[1])
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				for y := MinY; y < SeaLevel; y++ {
					if s := sectionBlockAt(ch, lx, y, lz); s == Seagrass || s == TallSeagrassLower {
						n++
					}
				}
			}
		}
		return n
	}
	var hit [2]int32
	found := false
	for _, c := range chunks {
		if count(c) > 0 {
			hit, found = c, true
			break
		}
	}
	if !found {
		t.Fatal("no seagrass in eight ocean chunks")
	}
	// A plank two cells over the floor of every column of the chunk.
	edits := map[[3]int]uint32{}
	planks := BlockBase("oak_planks")
	for x := int(hit[0]) * 16; x < int(hit[0])*16+16; x++ {
		for z := int(hit[1]) * 16; z < int(hit[1])*16+16; z++ {
			if y, ok := g.seafloorCol(x, z); ok {
				edits[[3]int{x, y + 2, z}] = planks
			}
		}
	}
	g.SetEditRegion(func(ecx, ecz int32, fn func(x, y, z int, s uint32)) {
		for p, s := range edits {
			if int32(floorDiv16(p[0])) == ecx && int32(floorDiv16(p[2])) == ecz {
				fn(p[0], p[1], p[2], s)
			}
		}
	})
	if n := count(hit); n != 0 {
		t.Errorf("chunk %v: %d seagrass under a build", hit, n)
	}
}
