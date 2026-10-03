package worldgen

import (
	"strings"
	"testing"
)

// vdmGen builds a Nether or End generator in vanilla mode, through the
// registry as the generator-mode switch installs it.
func vdmGen(t *testing.T, seed int64, nether bool) *Generator {
	t.Helper()
	g := NewEndGenerator(seed)
	if nether {
		g = NewNetherGenerator(seed)
	}
	if !g.useVanillaDimension(VanillaGenContext{Seed: seed}) {
		t.Fatal("no vanilla dimension installed")
	}
	return g
}

// A vanilla Nether chunk is the vanilla terrain at its true heights — void
// under y=0, the bedrock floor and roof at 0 and 127, the lava sea up to
// y=31 — with vanilla's biomes, and the engine's features decorating it:
// ores, glowstone and fire land in the terrain's own range, never below the
// floor or over the roof.
func TestVanillaNetherChunks(t *testing.T) {
	const seed = 1
	g := vdmGen(t, seed, true)
	v := g.vNether()
	counts := map[uint32]int{}
	noFloor, noRoof := 0, 0
	for cx := int32(-3); cx <= 3; cx++ {
		for cz := int32(-3); cz <= 3; cz++ {
			ch := g.GenerateChunk(cx, cz)
			if again := g.GenerateChunk(cx, cz); !again.Equal(ch) {
				t.Fatalf("chunk %d,%d differs between two generations", cx, cz)
			}
			terr := v.terrain(cx, cz)
			for s, b := range ch.Biomes {
				if !strings.HasPrefix(b, "minecraft:") || b != vdmNNames[terr.biome[10]] {
					t.Fatalf("chunk %d,%d section %d biome %q", cx, cz, s, b)
				}
			}
			for lx := 0; lx < 16; lx++ {
				for lz := 0; lz < 16; lz++ {
					for y := MinY; y < 0; y++ {
						if b := sectionBlockAt(ch, lx, y, lz); b != Air {
							t.Fatalf("chunk %d,%d: %v under the floor at y=%d", cx, cz, b, y)
						}
					}
					if sectionBlockAt(ch, lx, 0, lz) != Bedrock {
						noFloor++
					}
					if sectionBlockAt(ch, lx, 127, lz) != Bedrock {
						noRoof++
					}
					for y := 128; y < MinY+len(ch.Sections)*16; y++ {
						if b := sectionBlockAt(ch, lx, y, lz); b != Air {
							t.Fatalf("chunk %d,%d: %v over the roof at y=%d", cx, cz, b, y)
						}
					}
					for y := 1; y < 127; y++ {
						counts[sectionBlockAt(ch, lx, y, lz)]++
					}
				}
			}
		}
	}
	if noFloor > 49*256/100 || noRoof > 49*256/100 {
		t.Errorf("%d columns without the bedrock floor at y=0, %d without the roof at y=127", noFloor, noRoof)
	}
	for _, want := range []uint32{Netherrack, Lava, NetherQuartzOre, NetherGoldOre, Glowstone, MagmaBlock} {
		if counts[want] == 0 {
			name, _ := StateName(want)
			t.Errorf("no %s in 49 vanilla Nether chunks", name)
		}
	}
	// The lava sea: open cells below y=32 are lava in the terrain.
	terr := v.terrain(0, 0)
	for y := 1; y < 32; y++ {
		for i := 0; i < 256; i++ {
			if terr.codes[y*256+i] == vdmAir {
				t.Fatalf("terrain air below the lava level at y=%d", y)
			}
		}
	}
}

// A portal into a vanilla Nether lands on a cavern floor above the lava
// sea and under the roof, on the vanilla terrain.
func TestVanillaNetherLanding(t *testing.T) {
	g := vdmGen(t, -4172144997902289642, true)
	found := 0
	for _, p := range [][2]int{{0, 0}, {100, -40}, {-333, 512}, {2000, 2000}, {-64, 7}} {
		x, y, z, ok := g.NetherLanding(p[0], p[1])
		if !ok {
			continue
		}
		found++
		if y <= 31 || y >= 124 {
			t.Errorf("landing near %v at y=%d, outside the caverns", p, y)
		}
		if g.BlockAt(x, y, z) != Air || g.BlockAt(x, y+1, z) != Air || !Collides(g.BlockAt(x, y-1, z)) {
			t.Errorf("landing %d,%d,%d is not a floor with headroom", x, y, z)
		}
		if s := g.SurfaceY(x, z); s <= 31 || s >= 124 {
			t.Errorf("SurfaceY at %d,%d = %v", x, z, s)
		}
	}
	if found < 3 {
		t.Errorf("only %d of 5 landings found a floor", found)
	}
}

// A vanilla End chunk is the vanilla terrain with vanilla's biome rings,
// the spikes standing on the dimension's floor, the podium on the main
// island's top at the origin, and the outer islands' chorus.
func TestVanillaEndChunks(t *testing.T) {
	const seed = 1
	g := vdmGen(t, seed, false)
	v := g.vEnd()
	spikes := g.EndSpikes()
	if len(spikes) != EndPillars {
		t.Fatalf("%d spikes", len(spikes))
	}
	for _, s := range spikes {
		ch := g.GenerateChunk(int32(s.X>>4), int32(s.Z>>4))
		lx, lz := s.X&15, s.Z&15
		for _, y := range []int{0, 30, s.Height - 1} {
			if b := sectionBlockAt(ch, lx, y, lz); b != Obsidian {
				t.Errorf("spike at %d,%d: y=%d is %v, not obsidian", s.X, s.Z, y, b)
			}
		}
		if b := sectionBlockAt(ch, lx, s.Height, lz); b != Bedrock {
			t.Errorf("spike at %d,%d: cap is %v", s.X, s.Z, b)
		}
	}
	ox, oy, oz := g.EndExitPortal()
	if want := v.terrain(0, 0).top(0, 0); oy != want {
		t.Errorf("exit portal at y=%d, the island's top is %d", oy, want)
	}
	if ch := g.GenerateChunk(0, 0); sectionBlockAt(ch, ox&15, oy, oz&15) != Bedrock {
		t.Errorf("no podium at the exit portal %d,%d,%d", ox, oy, oz)
	}
	// The rings, and chorus on the highlands somewhere out there.
	chorus, rings := 0, map[string]bool{}
	for cx := int32(60); cx < 120; cx += 3 {
		for cz := int32(-30); cz < 30; cz += 3 {
			ch := g.GenerateChunk(cx, cz)
			rings[ch.Biomes[8]] = true
			if ch.Biomes[8] != vdmENames[v.chunkBiome(cx, cz)] {
				t.Fatalf("chunk %d,%d biome %q", cx, cz, ch.Biomes[8])
			}
			for s := range ch.Sections {
				for _, b := range ch.Sections[s] {
					if isChorusPlantState(b) {
						chorus++
					}
				}
			}
		}
	}
	for _, b := range []string{"minecraft:end_highlands", "minecraft:end_midlands", "minecraft:small_end_islands", "minecraft:end_barrens"} {
		if !rings[b] {
			t.Errorf("no %s among the outer chunks", b)
		}
	}
	if chorus == 0 {
		t.Error("no chorus on the outer islands")
	}
}

// Placement takes over a dimension's features when it registers for it.
func TestVanillaDimensionUsesPlacement(t *testing.T) {
	old := vanillaPlacementCtor
	defer func() { vanillaPlacementCtor = old }()
	var got []Dimension
	RegisterVanillaPlacement(func(ctx VanillaGenContext) VanillaPlacement {
		got = append(got, ctx.Dim)
		if ctx.Terrain == nil || ctx.Biomes == nil || ctx.Terrain.MinY() != 0 {
			t.Errorf("placement for %v built without the dimension's terrain and biomes", ctx.Dim)
		}
		return vdmMarkPlacement{}
	})
	for _, nether := range []bool{true, false} {
		g := vdmGen(t, 7, nether)
		ch := g.GenerateChunk(80, 3)
		if sectionBlockAt(ch, 0, 200, 0) != vdmTestMark {
			t.Errorf("nether=%v: the placement pass did not decorate the chunk", nether)
		}
	}
	if len(got) != 2 || got[0] != DimNether || got[1] != DimEnd {
		t.Errorf("placement built for %v", got)
	}
}

type vdmMarkPlacement struct{}

func (vdmMarkPlacement) Decorate(ch *Chunk, cx, cz int32) {
	setSectionBlock(ch, 0, 200, 0, vdmTestMark, true)
}

var vdmTestMark = blockBase("sponge")
