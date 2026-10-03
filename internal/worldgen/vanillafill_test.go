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
