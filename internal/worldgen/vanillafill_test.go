package worldgen

import (
	"math"
	"testing"
)

// The vanilla generator's noise stage against the 26.3 server. The numbers
// are a Java oracle's (FillOracle: the real RandomState, NoiseChunk and
// aquifer of the server jar, run against its overworld noise settings):
// FNV-1 hashes over the float bits of the router's functions at a lattice
// of points (sampleBlockValueUncached — the point pass, no caches), and per
// chunk over the final_density volume's bits and doFill's substance codes
// (0 air, 1 the default block, 2 water, 3 lava) in its loop order.

func vtFNV(h uint64, v uint64) uint64 { return (h ^ v) * 1099511628211 }

const vtFNVBasis = 1469598103934665603

type vtFillCase struct {
	cx, cz                 int32
	density, codes         uint64
	air, solid, water, lav int
}

func vtCheckRouterPoints(t *testing.T, g *vtGen, want map[string]uint64) {
	t.Helper()
	fns := map[string]vdSampler{"final": g.final, "temperature": g.temperature, "vegetation": g.vegetation,
		"continents": g.continents, "erosion": g.erosion, "depth": g.depth, "ridges": g.ridges, "chunk_surface": g.chunkSurface}
	for name, w := range want {
		f := fns[name]
		h := uint64(vtFNVBasis)
		for x := -2000; x <= 2000; x += 397 {
			for z := -2000; z <= 2000; z += 411 {
				for y := g.set.minY; y < g.set.minY+g.set.height; y += 37 {
					h = vtFNV(h, uint64(math.Float32bits(f.value(vdUncached(), x, y, z))))
				}
			}
		}
		if h != w {
			t.Errorf("%s: point hash %d, the server's %d", name, h, w)
		}
	}
}

func vtCheckFill(t *testing.T, g *vtGen, cases []vtFillCase) {
	t.Helper()
	for _, c := range cases {
		vol := vtVol(16, g.set.height, 16, int(c.cx)*16, g.set.minY, int(c.cz)*16)
		nc := g.newNoiseChunk(vol, nil)
		dens := vdBuf(vol.size())
		g.final.volume(nc.ctx, dens, vol)
		h := uint64(vtFNVBasis)
		for _, d := range dens {
			h = vtFNV(h, uint64(math.Float32bits(d)))
		}
		if h != c.density {
			t.Errorf("chunk %d,%d: final_density volume hash %d, the server's %d", c.cx, c.cz, h, c.density)
		}
		h = vtFNVBasis
		var counts [4]int
		for z := 0; z < 16; z++ {
			for x := 0; x < 16; x++ {
				for y := vol.sy - 1; y >= 0; y-- {
					s := nc.substance(vol.blockX(x), vol.blockY(y), vol.blockZ(z), float64(dens[vol.index(x, y, z)]))
					code := uint64(0)
					switch s {
					case g.set.defaultBlock:
						code = 1
					case Water:
						code = 2
					case Lava:
						code = 3
					}
					counts[code]++
					h = vtFNV(h, code)
				}
			}
		}
		if h != c.codes || counts != [4]int{c.air, c.solid, c.water, c.lav} {
			t.Errorf("chunk %d,%d: fill hash %d %v, the server's %d %v", c.cx, c.cz, h, counts, c.codes,
				[4]int{c.air, c.solid, c.water, c.lav})
		}
	}
}

func TestVanillaNoiseStageMatchesServer(t *testing.T) {
	g, err := newVTGen("minecraft:overworld", 1)
	if err != nil {
		t.Fatal(err)
	}
	vtCheckRouterPoints(t, g, map[string]uint64{
		"final":         243125552515104637,
		"temperature":   13687466906842575450,
		"vegetation":    16574813833329657738,
		"continents":    8868829649226039781,
		"erosion":       11070271834814757964,
		"depth":         15686854322090919909,
		"ridges":        14834309286643828822,
		"chunk_surface": 1203638446042282139,
	})
	vtCheckFill(t, g, []vtFillCase{
		{0, 0, 785275497569193115, 918327429180815844, 67606, 24267, 6431, 0},
		{3, -7, 3621180397743243390, 17093278071845862257, 65826, 24666, 7812, 0},
		{-12, 5, 13228098300543559679, 12939032985709881349, 65792, 26384, 6128, 0},
		{40, 40, 18109436498702108576, 13643884932348905636, 64315, 33989, 0, 0},
		{100, -200, 10616754582923933420, 9816093652413435439, 65951, 31684, 645, 24},
		{-300, 77, 10375591749304858709, 18138679412257799527, 53808, 44496, 0, 0},
	})
}

// The other presets' noise settings, seed 5: amplified and large biomes
// (other splines and climate noises), caves and floating islands (the
// legacy random source, no aquifer, their own sea levels).
func TestVanillaPresetNoiseMatchesServer(t *testing.T) {
	cases := []struct {
		settings string
		points   map[string]uint64
		chunks   []vtFillCase
	}{
		{"minecraft:amplified", map[string]uint64{
			"final": 17502182022827677221, "temperature": 8730827571817055132, "vegetation": 17274090563466570886,
			"continents": 2092207034218357044, "erosion": 12317375897048944901, "depth": 7207834411362825342,
			"ridges": 2316660504845635305, "chunk_surface": 16407363633099094171,
		}, []vtFillCase{
			{0, 0, 15885580155227785556, 14626509793812914548, 65850, 31921, 533, 0},
			{3, -7, 6125586280534707926, 8477269948939636213, 65699, 30738, 1867, 0},
			{-12, 5, 1386268660196839217, 2350823781402010138, 64777, 32916, 482, 129},
			{40, 40, 646699635687970793, 11291229650536897128, 67744, 25775, 4785, 0},
		}},
		{"minecraft:large_biomes", map[string]uint64{
			"final": 9988920356358674041, "temperature": 9745060323247004302, "vegetation": 11418205871799467671,
			"continents": 2559336946712574013, "erosion": 15589720639908766566, "depth": 17119209607859011402,
			"ridges": 2316660504845635305, "chunk_surface": 3722460728012695707,
		}, []vtFillCase{
			{0, 0, 421322216906652333, 10808747786192246881, 74369, 23932, 3, 0},
			{3, -7, 5975353104996937095, 17602683595848448291, 69127, 29170, 7, 0},
			{-12, 5, 5745271130883148964, 10334205057563876822, 66067, 32237, 0, 0},
			{40, 40, 7936589726826653820, 11291229650536897128, 67744, 25775, 4785, 0},
		}},
		{"minecraft:caves", map[string]uint64{"final": 1865587137721933514, "temperature": 4504323313408099315}, []vtFillCase{
			{0, 0, 3116137987822168727, 16300207862148487208, 10499, 20727, 17926, 0},
			{3, -7, 3712339099735304695, 13834712441768174863, 18867, 17488, 12797, 0},
			{-12, 5, 3300429252511296222, 15367036485500826381, 10271, 28412, 10469, 0},
			{40, 40, 14198880236764434550, 9870678563790887799, 14745, 25606, 8801, 0},
		}},
		{"minecraft:floating_islands", map[string]uint64{"final": 14690726076577729722, "temperature": 17213706936909659387}, []vtFillCase{
			{0, 0, 3669523331032396470, 7318303901249655396, 52447, 13089, 0, 0},
			{3, -7, 2196696520647178954, 1947595853928672361, 64578, 958, 0, 0},
			{-12, 5, 17824875743794414492, 1973108123395150466, 45105, 20431, 0, 0},
			{40, 40, 4709217298110515651, 793872441813498509, 56278, 9258, 0, 0},
		}},
	}
	for _, c := range cases {
		t.Run(c.settings, func(t *testing.T) {
			g, err := newVTGen(c.settings, 5)
			if err != nil {
				t.Fatal(err)
			}
			vtCheckRouterPoints(t, g, c.points)
			vtCheckFill(t, g, c.chunks)
		})
	}
}
