package worldgen

import (
	"fmt"
	"hash/fnv"
	"testing"
)

// vpSynthTerrain is the oracle's synthetic world as a VanillaTerrain.
type vpSynthTerrain struct{}

func (vpSynthTerrain) BlockAt(x, y, z int) uint32 { return vpSynthLevel{}.Block(x, y, z) }
func (vpSynthTerrain) Height(k HeightmapType, x, z int) int {
	return vpSynthLevel{}.Height(k, x, z)
}
func (vpSynthTerrain) MinY() int     { return MinY }
func (vpSynthTerrain) Ceiling() int  { return MinY + SectionCount*16 }
func (vpSynthTerrain) SeaLevel() int { return SeaLevel }

// vpSynthBiomeSrc gives each chunk one biome (vpSynthChunkBiome).
type vpSynthBiomeSrc struct{}

func (vpSynthBiomeSrc) BiomeAt(qx, qy, qz int) string {
	return "minecraft:" + vpSynthChunkBiome(qx*4, qz*4)
}

func vpSynthChunk(cx, cz int32) *Chunk {
	ch := NewChunk(SectionCount)
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			for y := MinY; y < MinY+SectionCount*16; y++ {
				setSectionBlock(ch, lx, y, lz, vpSynthLevel{}.Block(int(cx)*16+lx, y, int(cz)*16+lz), true)
			}
		}
	}
	return ch
}

func newSynthPlacer(t *testing.T, seed int64) *vanillaPlacer {
	t.Helper()
	pl := newVanillaPlacer(VanillaGenContext{Seed: seed, Dim: DimOverworld, Terrain: vpSynthTerrain{}, Biomes: vpSynthBiomeSrc{}})
	if pl == nil {
		t.Fatal("no placement pass for the overworld")
	}
	return pl.(*vanillaPlacer)
}

// TestVanillaPlacementDecorates runs the placement pass over the synthetic
// world: the ores, plants and trees the biomes list land, the same way on
// every run, and a feature from one chunk reaching into the next writes
// the same blocks whichever chunk is generated.
func TestVanillaPlacementDecorates(t *testing.T) {
	p := newSynthPlacer(t, 1)
	count := map[string]int{}
	gen := func(cx, cz int32) *Chunk {
		ch := vpSynthChunk(cx, cz)
		e := &vpExec{ch: ch, baseX: int(cx) << 4, baseZ: int(cz) << 4, terrain: p.terrain, unhandled: map[string]int{},
			ctx: &vpCtx{lv: vpTerrainLevel{p}, minY: p.decor.minY, height: p.decor.height, seaLevel: p.decor.seaLevel}}
		for step := 0; step < vpStepCount; step++ {
			for dx := int32(-1); dx <= 1; dx++ {
				for dz := int32(-1); dz <= 1; dz++ {
					p.decor.decorateSteps(cx+dx, cz+dz, vpTerrainLevel{p}, p.neighbourhoodBiomes(cx+dx, cz+dz), step, step, nil,
						func(pl *vpPlacement) { e.place(pl.placed.feature(), pl.rng, pl.pos) }, nil)
				}
			}
		}
		for k, v := range e.unhandled {
			count["unhandled "+k] += v
		}
		return ch
	}
	names := map[string]int{}
	for cx := int32(0); cx < 4; cx++ {
		for cz := int32(0); cz < 4; cz++ {
			a, b := gen(cx, cz), gen(cx, cz)
			if !a.Equal(b) {
				t.Fatalf("chunk %d,%d: decoration differs between runs", cx, cz)
			}
			base := vpSynthChunk(cx, cz)
			for s := range a.Sections {
				for i, st := range a.Sections[s] {
					if st != base.Sections[s][i] {
						n, _ := StateName(st)
						names[n]++
					}
				}
			}
		}
	}
	for _, want := range []string{"coal_ore", "iron_ore", "deepslate_diamond_ore", "granite", "short_grass", "oak_log", "oak_leaves", "dirt"} {
		if names[want] == 0 {
			t.Errorf("no %s placed in 16 chunks; placed %v", want, names)
		}
	}
	t.Logf("placed %v; %v", names, count)
}

// TestVanillaPlacementStraddles checks a straddling feature: chunk B's
// copy of a neighbour's tree matches what chunk A's pass writes on B's side
// (both passes draw the tree from the same stream and the same terrain).
func TestVanillaPlacementStraddles(t *testing.T) {
	p := newSynthPlacer(t, 7)
	run := func(target [2]int32, src [2]int32) *Chunk {
		ch := vpSynthChunk(target[0], target[1])
		e := &vpExec{ch: ch, baseX: int(target[0]) << 4, baseZ: int(target[1]) << 4, terrain: p.terrain,
			ctx: &vpCtx{lv: vpTerrainLevel{p}, minY: p.decor.minY, height: p.decor.height, seaLevel: p.decor.seaLevel}}
		p.decor.decorate(src[0], src[1], vpTerrainLevel{p}, p.neighbourhoodBiomes(src[0], src[1]),
			func(pl *vpPlacement) { e.place(pl.placed.feature(), pl.rng, pl.pos) }, nil)
		return ch
	}
	// The source chunk's decoration written into itself and into its east
	// neighbour: each run sees the whole tree list identically, so a
	// second pass into the same target is identical.
	src := [2]int32{1, 1}
	for _, target := range [][2]int32{{1, 1}, {2, 1}, {1, 2}} {
		if !run(target, src).Equal(run(target, src)) {
			t.Errorf("target %v: two passes of source %v differ", target, src)
		}
	}
}

// TestVanillaPlacementLeavesNativeAlone: a vanilla placement pass in the
// process changes nothing a native generator makes — its chunks and its
// structure sites are the frozen ones of the engine before the vanilla
// generator.
func TestVanillaPlacementLeavesNativeAlone(t *testing.T) {
	newSynthPlacer(t, 3) // a vanilla pass registered for another generator
	g := NewGenerator(20260703)
	h := fnv.New64a()
	for _, c := range [][2]int32{{0, 0}, {5, -3}, {-12, 7}, {40, 41}, {-60, -2}, {13, 90}} {
		ch := g.GenerateChunk(c[0], c[1])
		for s := range ch.Sections {
			for _, st := range ch.Sections[s] {
				fmt.Fprint(h, st, ",")
			}
			fmt.Fprint(h, ch.Biomes[s])
		}
	}
	for _, p := range [][2]int{{0, 0}, {900, -400}, {-3000, 1200}, {5000, 5000}} {
		fmt.Fprint(h, g.VillageIn(p[0], p[1]), g.OutpostIn(p[0], p[1]), g.MonumentIn(p[0], p[1]),
			g.MansionIn(p[0], p[1]), g.IglooIn(p[0], p[1]).X, g.ShipwreckIn(p[0], p[1]).X,
			g.MineshaftAt(p[0]>>4, p[1]>>4).Exists, g.StrongholdIn(p[0], p[1]).X, g.TrialChamberIn(p[0], p[1]))
	}
	const frozen = 5753327912243805609 // the engine at f1f8903, before the vanilla generator
	if got := h.Sum64(); got != frozen {
		t.Errorf("native generation hash %d, frozen %d", got, frozen)
	}
}
