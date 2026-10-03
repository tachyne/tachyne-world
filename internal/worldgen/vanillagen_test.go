package worldgen

import (
	"hash/fnv"
	"testing"
)

func nativeChunkHash(ch *Chunk) uint64 {
	h := fnv.New64a()
	var b [4]byte
	for i := range ch.Sections {
		for _, s := range ch.Sections[i] {
			b[0], b[1], b[2], b[3] = byte(s), byte(s>>8), byte(s>>16), byte(s>>24)
			h.Write(b[:])
		}
		h.Write([]byte(ch.Biomes[i]))
	}
	for _, v := range ch.Heightmap {
		h.Write([]byte{byte(v), byte(v >> 8)})
	}
	return h.Sum64()
}

// The live world is native and must stay byte for byte what it was: chunks
// of the native generator (both cave modes, the Nether, the End) hash to
// what the generator before the vanilla one made (frozen from f1f8903), and
// setting the native generator explicitly changes nothing.
func TestNativeGeneratorUnchanged(t *testing.T) {
	want := []struct {
		seed   int64
		caves  CaveMode
		cx, cz int32
		hash   uint64
	}{
		{7, 0, 0, 0, 8874669957560828447},
		{7, 0, 5, -3, 14382882826036255756},
		{7, 0, -20, 13, 6751257366145384637},
		{7, 0, 64, 64, 5781325112658318579},
		{7, 1, 0, 0, 4994517466347274603},
		{7, 1, 5, -3, 504240754968165385},
		{7, 1, -20, 13, 8677183219414127880},
		{7, 1, 64, 64, 11242964389022958295},
		{1, 0, 0, 0, 3119347440346940515},
		{1, 0, 5, -3, 10718195529394173},
		{1, 0, -20, 13, 271789060874359615},
		{1, 0, 64, 64, 185477975305308540},
		{1, 1, 0, 0, 4240512898527976501},
		{1, 1, 5, -3, 1398424885343830014},
		{1, 1, -20, 13, 9840743314289553773},
		{1, 1, 64, 64, 8591632560053764153},
	}
	for _, w := range want {
		g := NewGenerator(w.seed)
		if err := g.SetGenerator(GeneratorNative, PresetNormal); err != nil {
			t.Fatal(err)
		}
		g.SetCaveMode(w.caves)
		if got := nativeChunkHash(g.GenerateChunk(w.cx, w.cz)); got != w.hash {
			t.Errorf("seed %d caves %v chunk %d,%d: hash %d, before the vanilla generator %d", w.seed, w.caves, w.cx, w.cz, got, w.hash)
		}
	}
	dims := []struct {
		gen    func(int64) *Generator
		cx, cz int32
		hash   uint64
	}{
		{NewNetherGenerator, 0, 0, 45878025970750782},
		{NewEndGenerator, 0, 0, 3107068645354613248},
		{NewNetherGenerator, 3, 3, 9370255707511420312},
		{NewEndGenerator, 3, 3, 2274547433917522186},
	}
	for _, d := range dims {
		g := d.gen(1)
		if got := nativeChunkHash(g.GenerateChunk(d.cx, d.cz)); got != d.hash {
			t.Errorf("dimension chunk %d,%d: hash %d, before the vanilla generator %d", d.cx, d.cz, got, d.hash)
		}
	}
}

// A vanilla world through GenerateChunk: its terrain is what the surface
// test checks (filled, surfaced, carved), BlockAt and Height agree with it
// (both read the terrain before features), biomes come from the biome
// source, and the native world of the same seed is not touched.
func TestVanillaGeneratorGenerates(t *testing.T) {
	g := NewGenerator(1)
	if err := g.SetGenerator(GeneratorVanilla, PresetNormal); err != nil {
		t.Fatal(err)
	}
	if g.GeneratorMode() != GeneratorVanilla || g.CaveMode() != CavesVanilla {
		t.Fatalf("mode %v caves %v", g.GeneratorMode(), g.CaveMode())
	}
	full := g.GenerateChunk(0, 0)
	ch := g.vw.terrainChunk(0, 0) // before features: what BlockAt and Height read
	for x := 0; x < 16; x++ {
		for z := 0; z < 16; z++ {
			for y := MinY; y < 320; y += 7 {
				if got, want := g.BlockAt(x, y, z), ch.getGen(x, y, z); got != want {
					t.Fatalf("BlockAt(%d,%d,%d) = %d, the chunk %d", x, y, z, got, want)
				}
			}
			h := g.Height(x, z)
			if s := ch.getGen(x, h-1, z); vtIsAir(s) || vtIsFluid(s) {
				t.Fatalf("Height(%d,%d) = %d sits on %d", x, z, h, s)
			}
			for y := h; y < 320; y++ {
				if s := ch.getGen(x, y, z); !vtIsAir(s) && !vtIsFluid(s) {
					t.Fatalf("Height(%d,%d) = %d under solid %d at %d", x, z, h, s, y)
				}
			}
		}
	}
	if ch.getGen(0, MinY, 0) != Bedrock {
		t.Error("no bedrock floor")
	}
	for _, b := range full.Biomes {
		if b == "" {
			t.Fatal("a section has no biome")
		}
	}
	native := NewGenerator(1)
	if nativeChunkHash(native.GenerateChunk(0, 0)) != 3119347440346940515 {
		t.Error("the native generator changed beside a vanilla one")
	}
}

// Every preset builds and generates a chunk.
func TestVanillaPresetsGenerate(t *testing.T) {
	if testing.Short() {
		t.Skip("heavy: whole vanilla chunks and searches; runs in the gate's non-race pass")
	}
	for _, p := range []WorldPreset{PresetNormal, PresetLargeBiomes, PresetAmplified, PresetSingleBiome, PresetCaves, PresetFloatingIslands, PresetFlat} {
		g := NewGenerator(3)
		if err := g.SetGenerator(GeneratorVanilla, p); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		ch := g.GenerateChunk(1, 2)
		solid := 0
		for _, sec := range ch.Sections {
			for _, s := range sec {
				if s != Air {
					solid++
				}
			}
		}
		if solid == 0 {
			t.Errorf("%s: an empty chunk", p)
		}
		if p == PresetFlat && (ch.getGen(0, MinY, 0) != Bedrock || ch.getGen(0, MinY+3, 0) != GrassBlock || ch.getGen(0, MinY+4, 0) != Air) {
			t.Errorf("flat: layers %d %d %d", ch.getGen(0, MinY, 0), ch.getGen(0, MinY+3, 0), ch.getGen(0, MinY+4, 0))
		}
	}
	if err := NewNetherGenerator(1).SetGenerator(GeneratorVanilla, PresetNormal); err != nil {
		t.Errorf("Nether: %v", err)
	}
}

// The climate the biome source reads is the router's, quantized as
// Climate.target quantizes it.
func TestVanillaClimateQuantizes(t *testing.T) {
	gen, err := newVTGen("minecraft:overworld", 1)
	if err != nil {
		t.Fatal(err)
	}
	c := vtClimate{gen}
	p := c.Sample(10, 16, -7)
	ctx := vdUncached()
	if want := int64(gen.continents.value(ctx, 40, 64, -28) * 10000); p.Continentalness != want {
		t.Errorf("continentalness %d, want %d", p.Continentalness, want)
	}
	if want := int64(gen.depth.value(ctx, 40, 64, -28) * 10000); p.Depth != want {
		t.Errorf("depth %d, want %d", p.Depth, want)
	}
}

// The spawn targets' fitness on the router's climate against the 26.3
// server's SpawnTargetPoint.sampleFitness, over a lattice (seed 1).
func TestVanillaSpawnFitnessMatchesServer(t *testing.T) {
	g := NewGenerator(1)
	if err := g.SetGenerator(GeneratorVanilla, PresetNormal); err != nil {
		t.Fatal(err)
	}
	h := uint64(vtFNVBasis)
	for x := -3000; x <= 3000; x += 97 {
		for z := -3000; z <= 3000; z += 89 {
			f, ok := g.VanillaSpawnFitness(x, z)
			if !ok {
				t.Fatal("no spawn targets")
			}
			h = vtFNV(h, uint64(f))
		}
	}
	if h != 3332146208064911510 {
		t.Errorf("fitness hash %d, the server's 3332146208064911510", h)
	}
	if _, ok := NewGenerator(1).VanillaSpawnFitness(0, 0); ok {
		t.Error("a native generator scored vanilla spawn targets")
	}
}

// SampleColumn is Sample at every quart of the column.
func TestVanillaClimateColumn(t *testing.T) {
	gen, err := newVTGen("minecraft:overworld", 1)
	if err != nil {
		t.Fatal(err)
	}
	c := vtClimate{gen}
	if gen.climateY != [6]bool{false, false, false, false, true, false} {
		t.Errorf("climate y dependence %v, want depth alone", gen.climateY)
	}
	out := make([]ClimatePoint, 96)
	c.SampleColumn(-37, 112, -16, out)
	for i, p := range out {
		if want := c.Sample(-37, -16+i, 112); p != want {
			t.Fatalf("quart y %d: column %+v, point %+v", -16+i, p, want)
		}
	}
}
