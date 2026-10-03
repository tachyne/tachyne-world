package worldgen

import (
	"math"
	"strings"
	"testing"
)

// Every expectation here is the 26.3 server's, from a program run against
// its jar (seed 1): the parameter lists as MultiNoiseBiomeSourceParameterList
// builds them, the climate RandomState samples, the biomes
// ChunkGenerator.doCreateBiomes fills, the End's biomes and erosion, and the
// quarts BiomeManager zooms to. Hashes are FNV-1a over 64-bit values in
// order, a biome name being its bytes then 0xFF.

type vbHash uint64

func newVBHash() vbHash { return 1469598103934665603 }

func (h *vbHash) mix(v int64) {
	*h ^= vbHash(uint64(v))
	*h *= 1099511628211
}

func (h *vbHash) name(s string) {
	for _, b := range []byte(strings.TrimPrefix(s, "minecraft:")) {
		h.mix(int64(b))
	}
	h.mix(0xFF)
}

func vbListHash(list []vbEntry) uint64 {
	h := newVBHash()
	for _, e := range list {
		for _, p := range e.space[:6] {
			h.mix(p.min)
			h.mix(p.max)
		}
		h.mix(e.space[6].min)
		h.name(e.biome)
	}
	return uint64(h)
}

func TestVanillaBiomeListsMatchVanilla(t *testing.T) {
	ow := vbOverworldEntries()
	if n, h := len(ow), vbListHash(ow); n != 7594 || h != 8566798999891866620 {
		t.Errorf("overworld list: %d entries hash %d, vanilla 7594 hash 8566798999891866620", n, h)
	}
	for _, c := range []struct {
		i     int
		space [7]vbParam
		biome string
	}{
		{0, [7]vbParam{{-10000, 10000}, {-10000, 10000}, {-12000, -10500}, {-10000, 10000}, {0, 0}, {-10000, 10000}, {0, 0}}, bMushroomFields},
		{1000, [7]vbParam{{-1500, 2000}, {-10000, -3500}, {-1100, 300}, {-10000, -7799}, {0, 0}, {-9333, -7666}, {0, 0}}, bSnowySlopes},
		{2500, [7]vbParam{{5500, 10000}, {-3500, -1000}, {3000, 10000}, {-2225, 500}, {0, 0}, {-5666, -4000}, {0, 0}}, bBadlands},
	} {
		if ow[c.i].space != c.space || ow[c.i].biome != c.biome {
			t.Errorf("overworld entry %d = %v %s, vanilla %v %s", c.i, ow[c.i].space, ow[c.i].biome, c.space, c.biome)
		}
	}
	ne := vbNetherEntries()
	if n, h := len(ne), vbListHash(ne); n != 5 || h != 1624800475621385309 {
		t.Errorf("nether list: %d entries hash %d, vanilla 5 hash 1624800475621385309", n, h)
	}
}

// vbOracleChunks is the oracle's chunk list: three fixed, then 61 drawn by
// java.util.Random(seed*31+7).
func vbOracleChunks(seed int64) [][2]int32 {
	out := [][2]int32{{0, 0}, {-1, -1}, {5, -3}}
	r := newVBLegacyRandom(seed*31 + 7)
	for i := 0; i < 61; i++ {
		x := r.nextInt(1200) - 600
		z := r.nextInt(1200) - 600
		out = append(out, [2]int32{x, z})
	}
	return out
}

// vbClimateHash hashes the climate at every quart column of the chunks, x
// then y then z, as the oracle walks them.
func vbClimateHash(c VanillaClimate, chunks [][2]int32, minQY, quartsY int) uint64 {
	h := newVBHash()
	for _, ch := range chunks {
		for x := 0; x < 4; x++ {
			for y := 0; y < quartsY; y++ {
				for z := 0; z < 4; z++ {
					p := c.Sample(int(ch[0])*4+x, minQY+y, int(ch[1])*4+z)
					for _, v := range [6]int64{p.Temperature, p.Humidity, p.Continentalness, p.Erosion, p.Depth, p.Weirdness} {
						h.mix(v)
					}
				}
			}
		}
	}
	return uint64(h)
}

func TestVanillaClimateMatchesVanilla(t *testing.T) {
	chunks := vbOracleChunks(1)
	for _, c := range []struct {
		name    string
		climate VanillaClimate
		minQY   int
		quartsY int
		hash    uint64
		spots   [][9]int64 // qx qy qz, then the six values
	}{
		{"normal", NewVanillaOverworldClimate(1, PresetNormal), -16, 96, 16927885549349884480, [][9]int64{
			{0, -16, 0, -478, -2816, -4844, -2258, 8049, 2582},
			{-1562, 51, 704, -313, -1050, 232, -106, -11927, -32},
			{872, -7, 1824, 1105, 2136, 2423, -4321, 11789, -4621},
			{-1723, 31, -128, 658, 2849, -2356, 1589, -5925, 112}}},
		{"large_biomes", NewVanillaOverworldClimate(1, PresetLargeBiomes), -16, 96, 4275796151979015297, [][9]int64{
			{0, -16, 0, -2043, 2392, -6347, -252, 7740, 2582},
			{-1779, 22, -924, -6488, 1690, 2659, -1854, -860, 2731},
			{1658, 60, -2228, 414, -3111, 441, -2816, -12461, 2933}}},
		{"amplified", NewVanillaOverworldClimate(1, PresetAmplified), -16, 96, 6180547388729300441, [][9]int64{
			{872, -7, 1824, 1105, 2136, 2423, -4321, 16217, -4621},
			{1658, 60, -2228, 4677, 2760, 846, -3000, -9960, 2933}}},
		{"nether", NewVanillaNetherClimate(1), 0, 64, 1887573501840853635, [][9]int64{
			{0, 0, 0, -456, 105, 0, 0, 0, 0},
			{1392, 3, 1268, -2395, 1289, 0, 0, 0, 0},
			{-312, 9, 444, -424, -3360, 0, 0, 0, 0}}},
	} {
		for _, s := range c.spots {
			p := c.climate.Sample(int(s[0]), int(s[1]), int(s[2]))
			got := [6]int64{p.Temperature, p.Humidity, p.Continentalness, p.Erosion, p.Depth, p.Weirdness}
			if want := [6]int64(s[3:]); got != want {
				t.Errorf("%s climate at quart %v = %v, vanilla %v", c.name, s[:3], got, want)
			}
		}
		if h := vbClimateHash(c.climate, chunks, c.minQY, c.quartsY); h != c.hash {
			t.Errorf("%s climate over %d chunks hashes %d, vanilla %d", c.name, len(chunks), h, c.hash)
		}
	}
}

// The chunks' biomes, filled in vanilla's order with the search hint
// carried from quart to quart and chunk to chunk exactly as the oracle's
// single thread carried it (the three overworld presets share one list,
// so one hint runs through all three; the Nether's list has its own).
func TestVanillaChunkBiomesMatchVanilla(t *testing.T) {
	chunks := vbOracleChunks(1)
	fill := func(m *vbMultiNoise, hint *vbNode) (uint64, *vbNode) {
		h := newVBHash()
		for _, ch := range chunks {
			var c *vbChunkBiomes
			c, hint = m.fillChunk(ch[0], ch[1], hint)
			for sec := 0; sec < m.quartsY/4; sec++ {
				for x := 0; x < 4; x++ {
					for y := 0; y < 4; y++ {
						for z := 0; z < 4; z++ {
							h.name(c.b[((sec*4+y)*4+z)*4+x])
						}
					}
				}
			}
		}
		return uint64(h), hint
	}
	var hint *vbNode
	for _, c := range []struct {
		preset WorldPreset
		hash   uint64
	}{
		{PresetNormal, 16705857851413152762},
		{PresetLargeBiomes, 12385226345089760086},
		{PresetAmplified, 16517647527780054650},
	} {
		var h uint64
		m := newVBMultiNoise(vbOverworld(), NewVanillaOverworldClimate(1, c.preset), -64, 384)
		if h, hint = fill(m, hint); h != c.hash {
			t.Errorf("%v biomes over %d chunks hash %d, vanilla %d", c.preset, len(chunks), h, c.hash)
		}
	}
	m := newVBMultiNoise(vbNether(), NewVanillaNetherClimate(1), 0, 256)
	if h, _ := fill(m, nil); h != 8198904525800831299 {
		t.Errorf("nether biomes over %d chunks hash %d, vanilla 8198904525800831299", len(chunks), h)
	}
}

// The End: the erosion TheEndBiomeSource reads and the biome it picks, at
// 16 quarts around each oracle chunk scaled out threefold.
func TestVanillaEndBiomesMatchVanilla(t *testing.T) {
	e := &vbEnd{islands: newVBEndIslands(1)}
	for _, s := range []struct {
		qx, qy, qz int
		bits       int32
		biome      string
	}{
		{0, 8, 14, 1049475536, "the_end"},
		{3439, 28, -1971, 1042219068, "end_midlands"},
		{-3336, 4, 4339, -1104542728, "end_barrens"},
		{1982, 44, 4665, 974906368, "end_midlands"},
		{-2400, 0, 2760, -1087740254, "small_end_islands"},
		{-5736, 8, 2198, 1049846264, "end_highlands"},
		{-6039, 60, -5175, -1102777400, "end_barrens"},
	} {
		bx, bz := s.qx<<2, s.qz<<2
		if got := e.BiomeAt(s.qx, s.qy, s.qz); got != "minecraft:"+s.biome {
			t.Errorf("End biome at quart %d,%d,%d = %s, vanilla %s", s.qx, s.qy, s.qz, got, s.biome)
		}
		cx, cz := bx>>4, bz>>4
		if cx*cx+cz*cz <= 4096 {
			continue // the main island: the oracle's value is the centre term
		}
		if got := int32(math.Float32bits(e.islands.erosion((cx*2+1)*8, (cz*2+1)*8))); got != s.bits {
			t.Errorf("End erosion for quart %d,%d = %d, vanilla %d", s.qx, s.qz, got, s.bits)
		}
	}
	h := newVBHash()
	n := 0
	for _, ch := range vbOracleChunks(1) {
		for dx := 0; dx < 4; dx++ {
			for dz := 0; dz < 4; dz++ {
				qx, qz, qy := int(ch[0])*12+dx*7, int(ch[1])*12+dz*7, (dx*4+dz)*4
				h.name(e.BiomeAt(qx, qy, qz))
				n++
			}
		}
	}
	if n != 1024 || uint64(h) != vbEndBiomeHash {
		t.Errorf("End biomes at %d quarts hash %d, vanilla %d", n, uint64(h), vbEndBiomeHash)
	}
}

// vbEndBiomeHash is the oracle's End biomes (its E lines) hashed in order.
const vbEndBiomeHash uint64 = 12661035762767465588

func TestVanillaBiomeZoomMatchesVanilla(t *testing.T) {
	z := NewVanillaBiomeZoom(1)
	if z.seed != -6467378160175308932 {
		t.Fatalf("obfuscated seed %d, vanilla -6467378160175308932", z.seed)
	}
	for _, s := range [][6]int{
		{48985, 76, -58153, 12246, 18, -14538},
		{-82685, 309, 43586, -20672, 77, 10896},
		{-61123, -93, -86797, -15282, -24, -21699},
		{50287, -8, 5567, 12571, -3, 1391},
	} {
		if qx, qy, qz := z.Quart(s[0], s[1], s[2]); [3]int{qx, qy, qz} != [3]int(s[3:]) {
			t.Errorf("zoom of %v = %d,%d,%d, vanilla %v", s[:3], qx, qy, qz, s[3:])
		}
	}
	r := newVBLegacyRandom(1)
	h := newVBHash()
	for i := 0; i < 200000; i++ {
		x := int(r.nextInt(200000)) - 100000
		y := int(r.nextInt(448)) - 96
		zz := int(r.nextInt(200000)) - 100000
		qx, qy, qz := z.Quart(x, y, zz)
		h.mix(int64(qx))
		h.mix(int64(qy))
		h.mix(int64(qz))
	}
	if uint64(h) != 8191712400605504542 {
		t.Errorf("zoom of 200000 blocks hashes %d, vanilla 8191712400605504542", uint64(h))
	}
}

// Through the seam the terrain core calls: the registered constructor
// builds each dimension's source, and a chunk's quarts read back the
// biomes its fill stored (the overworld's first oracle chunk at the
// surface quarts the oracle hashed agrees with fillChunk).
func TestVanillaBiomesRegistered(t *testing.T) {
	if vanillaBiomesCtor == nil {
		t.Fatal("no vanilla biome source registered")
	}
	ow := vanillaBiomesCtor(VanillaGenContext{Seed: 1, Dim: DimOverworld, Preset: PresetNormal})
	m, ok := ow.(*vbMultiNoise)
	if !ok {
		t.Fatalf("overworld source is %T", ow)
	}
	want, _ := m.fillChunk(5, -3, nil)
	q := VanillaQuartBiomes(ow, 5, -3, -64, 24)
	for s := range q {
		for i, n := range q[s] {
			if w := want.b[s*64+i]; n != w {
				t.Fatalf("section %d quart %d = %s, fill %s", s, i, n, w)
			}
		}
	}
	// y outside the dimension reads the nearest stored quart.
	if a, b := ow.BiomeAt(20, -40, -12), ow.BiomeAt(20, -16, -12); a != b {
		t.Errorf("below the floor %s, at the floor %s", a, b)
	}
	if a, b := ow.BiomeAt(20, 200, -12), ow.BiomeAt(20, 79, -12); a != b {
		t.Errorf("above the top %s, at the top %s", a, b)
	}
	sec := VanillaSectionBiomes(ow, 5, -3, -64, 24)
	if len(sec) != 24 || sec[0] == "" {
		t.Fatalf("section biomes %v", sec)
	}
	if ne := vanillaBiomesCtor(VanillaGenContext{Seed: 1, Dim: DimNether}); ne.BiomeAt(0, 0, 0) == "" {
		t.Error("no Nether biome")
	}
	if e := vanillaBiomesCtor(VanillaGenContext{Seed: 1, Dim: DimEnd}); e.BiomeAt(0, 16, 0) != "minecraft:the_end" {
		t.Errorf("End centre is %s", e.BiomeAt(0, 16, 0))
	}
	if f := vanillaBiomesCtor(VanillaGenContext{Seed: 1, Preset: PresetSingleBiome}); f.BiomeAt(9, 9, 9) != bPlains {
		t.Errorf("single biome preset is %s", f.BiomeAt(9, 9, 9))
	}
	// Deep underground under deep-dark erosion and depth, the overworld
	// carries the cave biomes too: count what a spread of chunks holds.
	seen := map[string]bool{}
	for _, ch := range vbOracleChunks(1) {
		for _, sec := range VanillaQuartBiomes(ow, ch[0], ch[1], -64, 24) {
			for _, n := range sec {
				seen[n] = true
			}
		}
	}
	for _, n := range []string{bDeepDark, bLushCaves, bDripstoneCaves, bPlains, bOcean, bRiver} {
		if !seen[n] {
			t.Errorf("no %s in 64 chunks", n)
		}
	}
}

func BenchmarkVanillaChunkBiomes(b *testing.B) {
	m := newVBMultiNoise(vbOverworld(), NewVanillaOverworldClimate(1, PresetNormal), -64, 384)
	for i := 0; i < b.N; i++ {
		m.fillChunk(int32(i), int32(i*7), nil)
	}
}

// A second, negative seed through every part, against the same oracle.
func TestVanillaBiomesSecondSeed(t *testing.T) {
	const seed = -987654321987
	chunks := vbOracleChunks(seed)
	ow := NewVanillaOverworldClimate(seed, PresetNormal)
	if h := vbClimateHash(ow, chunks, -16, 96); h != 15921607196175645434 {
		t.Errorf("overworld climate hash %d, vanilla 15921607196175645434", h)
	}
	ne := NewVanillaNetherClimate(seed)
	if h := vbClimateHash(ne, chunks, 0, 64); h != 10321903032756993923 {
		t.Errorf("nether climate hash %d, vanilla 10321903032756993923", h)
	}
	for _, c := range []struct {
		m    *vbMultiNoise
		hash uint64
	}{
		{newVBMultiNoise(vbOverworld(), ow, -64, 384), 4683028266139748261},
		{newVBMultiNoise(vbNether(), ne, 0, 256), 919460470652440195},
	} {
		h := newVBHash()
		var hint *vbNode
		for _, ch := range chunks {
			var b *vbChunkBiomes
			b, hint = c.m.fillChunk(ch[0], ch[1], hint)
			for sec := 0; sec < c.m.quartsY/4; sec++ {
				for i := 0; i < 64; i++ { // x, y, z order
					x, y, z := i>>4, (i>>2)&3, i&3
					h.name(b.b[((sec*4+y)*4+z)*4+x])
				}
			}
		}
		if uint64(h) != c.hash {
			t.Errorf("biomes (min y %d) hash %d, vanilla %d", c.m.minQY*4, uint64(h), c.hash)
		}
	}
	e := &vbEnd{islands: newVBEndIslands(seed)}
	h := newVBHash()
	for _, ch := range chunks {
		for dx := 0; dx < 4; dx++ {
			for dz := 0; dz < 4; dz++ {
				h.name(e.BiomeAt(int(ch[0])*12+dx*7, (dx*4+dz)*4, int(ch[1])*12+dz*7))
			}
		}
	}
	if uint64(h) != 12413068664365501793 {
		t.Errorf("End biomes hash %d, vanilla 12413068664365501793", uint64(h))
	}
	z := NewVanillaBiomeZoom(seed)
	if z.seed != -6745549008937716262 {
		t.Errorf("obfuscated seed %d, vanilla -6745549008937716262", z.seed)
	}
	r := newVBLegacyRandom(seed)
	zh := newVBHash()
	for i := 0; i < 200000; i++ {
		x := int(r.nextInt(200000)) - 100000
		y := int(r.nextInt(448)) - 96
		zz := int(r.nextInt(200000)) - 100000
		qx, qy, qz := z.Quart(x, y, zz)
		zh.mix(int64(qx))
		zh.mix(int64(qy))
		zh.mix(int64(qz))
	}
	if uint64(zh) != 12271156905197434080 {
		t.Errorf("zoom hash %d, vanilla 12271156905197434080", uint64(zh))
	}
}
