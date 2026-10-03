package worldgen

import (
	"fmt"
	"math"
	"os"
	"testing"
)

// The vanilla-mode Nether and End are 26.3's, bit for bit: the oracle ran
// the 26.3 server's own NoiseBasedChunkGenerator.doFill,
// MaterialSystem.buildSurface and generateCarvers on a ProtoChunk (no
// structures, so no beardifier) for each chunk below and hashed the result
// after each stage — FNV-1a over a code per cell (vdmAir … vdmEndStone), y
// from 0 to 127, then z, then x — along with the chunk's noise biomes (the
// names of its 4×32×4 quarts) and its block biomes through BiomeManager's
// zoom at six heights; and sampled final_density uncached at a few points.

type vdmOracleChunk struct {
	cx, cz                          int32
	biomes, zoom, fill, surf, carve uint64
}

func vdmHashCodes(t *vdmTerrain) uint64 {
	h := uint64(1469598103934665603)
	for y := 0; y < vdmH; y++ {
		for i := 0; i < 256; i++ {
			h ^= uint64(t.codes[y*256+i])
			h *= 1099511628211
		}
	}
	return h
}

func vdmHashName(h uint64, n string) uint64 {
	for i := 0; i < len(n); i++ {
		h ^= uint64(n[i])
		h *= 1099511628211
	}
	h ^= 0
	h *= 1099511628211
	return h
}

// vdmPointDensity is final_density sampled uncached at a block:
// InterpolatedFunction.sampleValue's lerp3 over the cell's corners, then the
// squeeze.
func vdmPointDensity(corner func(x, y, z int) float32, cxz, cy, x, y, z int) float32 {
	ix, iy, iz := ((x%cxz)+cxz)%cxz, ((y%cy)+cy)%cy, ((z%cxz)+cxz)%cxz
	var v float32
	if ix == 0 && iy == 0 && iz == 0 {
		v = corner(x, y, z)
	} else {
		x0, y0, z0 := x-ix, y-iy, z-iz
		v = vnLerp3(float32(ix)/float32(cxz), float32(iy)/float32(cy), float32(iz)/float32(cxz),
			corner(x0, y0, z0), corner(x0+cxz, y0, z0), corner(x0, y0+cy, z0), corner(x0+cxz, y0+cy, z0),
			corner(x0, y0, z0+cxz), corner(x0+cxz, y0, z0+cxz), corner(x0, y0+cy, z0+cxz), corner(x0+cxz, y0+cy, z0+cxz))
	}
	return vcSqueeze(v) + 0
}

// vdmDumpDiff compares a stage with the oracle's dump of it, when the dumps
// are at hand (VDIMS_DUMPS), naming the first cells that differ.
func vdmDumpDiff(t *testing.T, dim string, seed int64, cx, cz int32, stage string, got *vdmTerrain) {
	dir := os.Getenv("VDIMS_DUMPS")
	if dir == "" {
		return
	}
	want, err := os.ReadFile(fmt.Sprintf("%s/%s-%d-%d_%d-%s.bin", dir, dim, seed, cx, cz, stage))
	if err != nil {
		t.Logf("no dump: %v", err)
		return
	}
	n := 0
	for y := 0; y < vdmH; y++ {
		for i := 0; i < 256; i++ {
			if g, w := got.codes[y*256+i], want[y*256+i]; g != w {
				if n < 12 {
					t.Errorf("%s %d,%d %s: x=%d y=%d z=%d got %d vanilla %d", dim, cx, cz, stage, int(cx)*16+i&15, y, int(cz)*16+i>>4, g, w)
				}
				n++
			}
		}
	}
	if n > 0 {
		t.Errorf("%s %d,%d %s: %d cells differ", dim, cx, cz, stage, n)
	}
}

var vdmNOracle = map[int64][]vdmOracleChunk{
	1: {
		{0, 0, 11987133322247101315, 15963302969041095555, 2832305118654747485, 5911810572222742563, 841575591735566595},
		{3, -7, 11987133322247101315, 15963302969041095555, 5233400631039647501, 16069022898459225951, 15696135999358076907},
		{-12, 5, 11987133322247101315, 15963302969041095555, 12306035719215911687, 1121918955200015777, 12528342277584251868},
		{40, 40, 4166180001547089795, 6909246723324698499, 9484498288176855222, 7103033124365867983, 7103033124365867983},
		{-300, 211, 11987133322247101315, 15963302969041095555, 7500813937489912399, 7818050011161817605, 6584901858956421438},
		{100, -50, 11018846368547138435, 361461043481197443, 2705885455863485911, 7425201365815679175, 2889395225738447587},
		{-77, 33, 11987133322247101315, 15963302969041095555, 17068288247390013995, 13252974584411790617, 11908711072555762287},
		{250, 250, 13649291356104670659, 222320301362773462, 8672036013411204483, 11141117468166760499, 11141117468166760499},
		{9, 123, 579174697668935555, 5528343765382625155, 4101052995985868541, 16358606355338492295, 1250225446121847913},
		{-500, -500, 4164172056855779715, 15950289319025132931, 18144037145306251263, 15895322112302095652, 2189009197071989970},
		{1000, -2000, 11018846368547138435, 361461043481197443, 17498485684041480136, 13139261312691802065, 5130824923921130889},
		{-1, -1, 11987133322247101315, 15963302969041095555, 15708767057762028815, 10686117530610988695, 14632920438156243326},
	},
	-4172144997902289642: {
		{0, 0, 4164172056855779715, 15950289319025132931, 16706532926538374290, 17864371166267509995, 5115404644061079876},
		{3, -7, 4164172056855779715, 15950289319025132931, 17698215785467295687, 8598444392973188586, 6966842065827135407},
		{-12, 5, 4164172056855779715, 15950289319025132931, 5267404521758586279, 772456843048380204, 9435462469282696586},
		{40, 40, 11987133322247101315, 15963302969041095555, 15672825733866329161, 3710485529808474728, 5115402794406324872},
		{-300, 211, 4164172056855779715, 15950289319025132931, 2692172677913500555, 18036663046197538337, 4068738379198954089},
		{100, -50, 579174697668935555, 5528343765382625155, 11376966097267208361, 4913510746701728447, 8400499367396607645},
		{-77, 33, 11987133322247101315, 15963302969041095555, 2913302207257811750, 3962488823826188222, 15062668305351390424},
		{250, 250, 4166180001547089795, 6909246723324698499, 13786508224258970548, 13325040043961648401, 6681083077203085456},
		{9, 123, 4166180001547089795, 6909246723324698499, 11403357798552387556, 8042417913600916734, 15205447763711139640},
		{-500, -500, 11987133322247101315, 15963302969041095555, 18401473873463864386, 7810561794674900242, 7810561794674900242},
		{1000, -2000, 4166180001547089795, 6909246723324698499, 576326101361799075, 2975945763204562847, 9891885175517688479},
		{-1, -1, 4164172056855779715, 15950289319025132931, 11804607746146213519, 10116343613881711386, 4247976353963402308},
	},
}

// vdmNOracleDensity is final_density at a few blocks (float bits), per seed.
var vdmNOracleDensity = map[int64][][4]int64{
	1: {
		{0, 0, 0, 1055566507}, {100, 40, -37, -1119161573}, {-1234, 64, 5678, -1114185147},
		{17, 127, -3, 1049588172}, {4, 8, 4, 1051942101}, {-8000, 100, 9000, -1132862744},
	},
	-4172144997902289642: {
		{0, 0, 0, 1055566507}, {100, 40, -37, -1114816737}, {-1234, 64, 5678, -1108824176},
		{17, 127, -3, 1049556410}, {4, 8, 4, 1052001242}, {-8000, 100, 9000, -1108246504},
	},
}

func TestVanillaZoomSeed(t *testing.T) {
	for seed, want := range map[int64]int64{1: -6467378160175308932, -4172144997902289642: 2159143436479834350} {
		if got := vdmZoomSeed(seed); got != want {
			t.Errorf("obfuscateSeed(%d) = %d, vanilla %d", seed, got, want)
		}
	}
}

func TestVanillaNetherMatchesVanilla(t *testing.T) {
	for seed, chunks := range vdmNOracle {
		v := newVanillaNether(seed, NewNetherGenerator(seed))
		for _, p := range vdmNOracleDensity[seed] {
			d := vdmPointDensity(v.corner, 4, 8, int(p[0]), int(p[1]), int(p[2]))
			if int32(math.Float32bits(d)) != int32(p[3]) {
				t.Errorf("seed %d: final_density at %d,%d,%d = %v, vanilla %v", seed, p[0], p[1], p[2], d, math.Float32frombits(uint32(p[3])))
			}
		}
		for _, c := range chunks {
			fill := v.buildStages(c.cx, c.cz, 1)
			if h := vdmHashCodes(fill); h != c.fill {
				t.Errorf("seed %d chunk %d,%d: fill hash %d, vanilla %d", seed, c.cx, c.cz, h, c.fill)
				vdmDumpDiff(t, "nether", seed, c.cx, c.cz, "fill", fill)
				continue
			}
			bh := uint64(1469598103934665603)
			for qy := 0; qy < 32; qy++ {
				for qz := 0; qz < 4; qz++ {
					for qx := 0; qx < 4; qx++ {
						bh = vdmHashName(bh, vdmNNames[fill.biome[qz*4+qx]][10:])
					}
				}
			}
			if bh != c.biomes {
				t.Errorf("seed %d chunk %d,%d: noise biomes hash %d, vanilla %d", seed, c.cx, c.cz, bh, c.biomes)
			}
			zh := uint64(1469598103934665603)
			for _, y := range []int{0, 31, 32, 64, 100, 127} {
				for z := 0; z < 16; z++ {
					for x := 0; x < 16; x++ {
						qx, _, qz := vdmZoom(v.zoom, int(c.cx)*16+x, y, int(c.cz)*16+z)
						zh = vdmHashName(zh, vdmNNames[v.noiseBiome(qx, qz)][10:])
					}
				}
			}
			if zh != c.zoom {
				t.Errorf("seed %d chunk %d,%d: zoomed biomes hash %d, vanilla %d", seed, c.cx, c.cz, zh, c.zoom)
			}
			surf := v.buildStages(c.cx, c.cz, 2)
			if h := vdmHashCodes(surf); h != c.surf {
				t.Errorf("seed %d chunk %d,%d: surface hash %d, vanilla %d", seed, c.cx, c.cz, h, c.surf)
				vdmDumpDiff(t, "nether", seed, c.cx, c.cz, "surf", surf)
				continue
			}
			full := v.buildStages(c.cx, c.cz, 3)
			if h := vdmHashCodes(full); h != c.carve {
				t.Errorf("seed %d chunk %d,%d: carved hash %d, vanilla %d", seed, c.cx, c.cz, h, c.carve)
				vdmDumpDiff(t, "nether", seed, c.cx, c.cz, "carve", full)
			}
		}
	}
}

var vdmEOracle = map[int64][]vdmOracleChunk{
	1: {
		{0, 0, 15412342006762666883, 17233105309615549315, 5082070847555146623, 5082070847555146623, 5082070847555146623},
		{3, -7, 15412342006762666883, 17233105309615549315, 2092149027815752579, 2092149027815752579, 2092149027815752579},
		{-12, 5, 15412342006762666883, 17233105309615549315, 2092149027815752579, 2092149027815752579, 2092149027815752579},
		{40, 40, 15412342006762666883, 17233105309615549315, 2092149027815752579, 2092149027815752579, 2092149027815752579},
		{-300, 211, 2509338299184085891, 5101583496879106947, 4120882390026166469, 4120882390026166469, 4120882390026166469},
		{100, -50, 14305248368452797315, 4684623192653608819, 2092149027815752579, 2092149027815752579, 2092149027815752579},
		{-77, 33, 14305248368452797315, 8567610338577384323, 2092149027815752579, 2092149027815752579, 2092149027815752579},
		{250, 250, 10969062203670461315, 5610618480712280963, 17746006717295366494, 17746006717295366494, 17746006717295366494},
		{9, 123, 10969062203670461315, 5610618480712280963, 1088204970602409691, 1088204970602409691, 1088204970602409691},
		{-500, -500, 14305248368452797315, 8567610338577384323, 2092149027815752579, 2092149027815752579, 2092149027815752579},
		{1000, -2000, 10969062203670461315, 5610618480712280963, 4037096016809464359, 4037096016809464359, 4037096016809464359},
		{-1, -1, 15412342006762666883, 17233105309615549315, 3703647474206485768, 3703647474206485768, 3703647474206485768},
	},
	-4172144997902289642: {
		{0, 0, 15412342006762666883, 17233105309615549315, 16477621505568339085, 16477621505568339085, 16477621505568339085},
		{3, -7, 15412342006762666883, 17233105309615549315, 10055739923798921488, 10055739923798921488, 10055739923798921488},
		{-12, 5, 15412342006762666883, 17233105309615549315, 2092149027815752579, 2092149027815752579, 2092149027815752579},
		{40, 40, 15412342006762666883, 17233105309615549315, 2092149027815752579, 2092149027815752579, 2092149027815752579},
		{-300, 211, 2509338299184085891, 16207324820432155594, 16987939852421900432, 16987939852421900432, 16987939852421900432},
		{100, -50, 14305248368452797315, 8567610338577384323, 2092149027815752579, 2092149027815752579, 2092149027815752579},
		{-77, 33, 10969062203670461315, 5610618480712280963, 1884813454404760420, 1884813454404760420, 1884813454404760420},
		{250, 250, 14305248368452797315, 8567610338577384323, 2092149027815752579, 2092149027815752579, 2092149027815752579},
		{9, 123, 14305248368452797315, 11616895541208609736, 2092149027815752579, 2092149027815752579, 2092149027815752579},
		{-500, -500, 2509338299184085891, 3476285438169378471, 1999718336176343368, 1999718336176343368, 1999718336176343368},
		{1000, -2000, 14305248368452797315, 8567610338577384323, 2092149027815752579, 2092149027815752579, 2092149027815752579},
		{-1, -1, 15412342006762666883, 17233105309615549315, 4323367677622178989, 4323367677622178989, 4323367677622178989},
	},
}

var vdmEOracleDensity = map[int64][][4]int64{
	1:                    {{0, 0, 0, -1114026017}, {100, 40, -37, 1033942664}, {-1234, 64, 5678, -1091917141}, {17, 127, -3, -1091917141}, {4, 8, 4, -1119666167}, {-8000, 100, 9000, -1091917141}},
	-4172144997902289642: {{0, 0, 0, -1114026017}, {100, 40, -37, -1110361538}, {-1234, 64, 5678, -1097419230}, {17, 127, -3, -1091917141}, {4, 8, 4, -1123511997}, {-8000, 100, 9000, -1091917141}},
}

func TestVanillaEndMatchesVanilla(t *testing.T) {
	for seed, chunks := range vdmEOracle {
		v := newVanillaEnd(seed, NewEndGenerator(seed))
		for _, p := range vdmEOracleDensity[seed] {
			d := vdmPointDensity(v.corner, 8, 4, int(p[0]), int(p[1]), int(p[2]))
			if int32(math.Float32bits(d)) != int32(p[3]) {
				t.Errorf("seed %d: final_density at %d,%d,%d = %v, vanilla %v", seed, p[0], p[1], p[2], d, math.Float32frombits(uint32(p[3])))
			}
		}
		for _, c := range chunks {
			got := v.build(c.cx, c.cz)
			if h := vdmHashCodes(got); h != c.carve {
				t.Errorf("seed %d chunk %d,%d: terrain hash %d, vanilla %d", seed, c.cx, c.cz, h, c.carve)
				vdmDumpDiff(t, "end", seed, c.cx, c.cz, "carve", got)
			}
			bh := uint64(1469598103934665603)
			for i := 0; i < 32*16; i++ {
				bh = vdmHashName(bh, vdmENames[got.biome[i%16]][10:])
			}
			if bh != c.biomes {
				t.Errorf("seed %d chunk %d,%d: noise biomes hash %d, vanilla %d", seed, c.cx, c.cz, bh, c.biomes)
			}
			zh := uint64(1469598103934665603)
			for _, y := range []int{0, 31, 32, 64, 100, 127} {
				for z := 0; z < 16; z++ {
					for x := 0; x < 16; x++ {
						qx, _, qz := vdmZoom(vdmZoomSeed(seed), int(c.cx)*16+x, y, int(c.cz)*16+z)
						zh = vdmHashName(zh, vdmENames[v.chunkBiome(int32(qx>>2), int32(qz>>2))][10:])
					}
				}
			}
			if zh != c.zoom {
				t.Errorf("seed %d chunk %d,%d: zoomed biomes hash %d, vanilla %d", seed, c.cx, c.cz, zh, c.zoom)
			}
		}
	}
}
