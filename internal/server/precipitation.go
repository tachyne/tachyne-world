package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Runtime precipitation (Biome.getPrecipitationAt): nothing where the biome
// has no precipitation, snow where the height-adjusted temperature is below
// 0.15, rain otherwise. The temperature is the biome's base, which the FROZEN
// modifier (frozen and deep frozen ocean) lifts to 0.2 in noise-picked
// patches of open water, lowered above sea level + 17 by the height and a
// ±8-block noise dither. The noises are vanilla's own: SimplexNoise seeded by
// a legacy (java.util.Random) source, so the snow line and the ice patches
// fall where vanilla's do.

// legacyRandom is LegacyRandomSource: java.util.Random's LCG.
type legacyRandom struct{ seed int64 }

func newLegacyRandom(seed int64) *legacyRandom {
	return &legacyRandom{(seed ^ 0x5DEECE66D) & (1<<48 - 1)}
}

func (r *legacyRandom) next(bits uint) int32 {
	r.seed = (r.seed*0x5DEECE66D + 0xB) & (1<<48 - 1)
	return int32(r.seed >> (48 - bits))
}

// nextInt is BitRandomSource.nextInt(bound).
func (r *legacyRandom) nextInt(bound int32) int32 {
	if bound&(bound-1) == 0 {
		return int32(int64(bound) * int64(r.next(31)) >> 31)
	}
	for {
		sample := r.next(31)
		modulo := sample % bound
		if sample-modulo+(bound-1) >= 0 {
			return modulo
		}
	}
}

// nextDouble is BitRandomSource.nextDouble.
func (r *legacyRandom) nextDouble() float64 {
	upper, lower := int64(r.next(26)), int64(r.next(27))
	return float64(upper<<27+lower) * 0x1p-53
}

// simplexNoise is SimplexNoise built with discardNoiseOffset (no offsets;
// the three offset draws are still taken from the source).
type simplexNoise struct{ perms [256]uint8 }

func newSimplexNoise(r *legacyRandom) *simplexNoise {
	r.nextDouble()
	r.nextDouble()
	r.nextDouble()
	n := &simplexNoise{}
	for i := range n.perms {
		n.perms[i] = uint8(i)
	}
	for i := 0; i < 256; i++ {
		off := r.nextInt(int32(256 - i))
		n.perms[i], n.perms[int(off)+i] = n.perms[int(off)+i], n.perms[i]
	}
	return n
}

func (n *simplexNoise) permute(x int) int { return int(n.perms[x&0xFF]) }

// simplexGrad is GradientNoise.GRADIENT's first twelve (x, y) pairs.
var simplexGrad = [12][2]float64{{1, 1}, {-1, 1}, {1, -1}, {-1, -1}, {1, 0}, {-1, 0}, {1, 0}, {-1, 0}, {0, 1}, {0, -1}, {0, 1}, {0, -1}}

func simplexCorner(g int, x, y float64) float64 {
	t := 0.5 - x*x - y*y
	if t < 0 {
		return 0
	}
	t *= t
	return t * t * (simplexGrad[g][0]*x + simplexGrad[g][1]*y)
}

var (
	simplexF2 = 0.5 * (math.Sqrt(3) - 1)
	simplexG2 = (3 - math.Sqrt(3)) / 6
)

// get is SimplexNoise.get(x, y).
func (n *simplexNoise) get(xin, yin float64) float32 {
	s := (xin + yin) * simplexF2
	i, j := int(math.Floor(xin+s)), int(math.Floor(yin+s))
	t := float64(i+j) * simplexG2
	x0, y0 := xin-(float64(i)-t), yin-(float64(j)-t)
	i1, j1 := 0, 1
	if x0 > y0 {
		i1, j1 = 1, 0
	}
	x1, y1 := x0-float64(i1)+simplexG2, y0-float64(j1)+simplexG2
	x2, y2 := x0-1+2*simplexG2, y0-1+2*simplexG2
	ii, jj := i&0xFF, j&0xFF
	gi0 := n.permute(ii+n.permute(jj)) % 12
	gi1 := n.permute(ii+i1+n.permute(jj+j1)) % 12
	gi2 := n.permute(ii+1+n.permute(jj+1)) % 12
	return float32(70 * (simplexCorner(gi0, x0, y0) + simplexCorner(gi1, x1, y1) + simplexCorner(gi2, x2, y2)))
}

// Biome's static noises.
var (
	temperatureNoise = newSimplexNoise(newLegacyRandom(1234))
	biomeInfoNoise   = newSimplexNoise(newLegacyRandom(2345))
	frozenNoise      = func() [3]*simplexNoise {
		r := newLegacyRandom(3456)
		return [3]*simplexNoise{newSimplexNoise(r), newSimplexNoise(r), newSimplexNoise(r)}
	}()
)

// frozenTemperatureNoise is FROZEN_TEMPERATURE_NOISE: three layers at
// frequency 1, ½, ¼ and amplitude 1/7, 2/7, 4/7, summed in float.
func frozenTemperatureNoise(x, z float64) float32 {
	var v float32
	v += 0.14285715 * frozenNoise[0].get(x, z)
	v += 0.2857143 * frozenNoise[1].get(x*0.5, z*0.5)
	v += 0.5714286 * frozenNoise[2].get(x*0.25, z*0.25)
	return v
}

// biomeTemperatureAt is Biome.getHeightAdjustedTemperature.
func biomeTemperatureAt(biome string, x, y, z int) float32 {
	base, frozen, _ := worldgen.BiomeClimate(biome)
	t := float32(base)
	if frozen { // TemperatureModifier.FROZEN
		large := float64(frozenTemperatureNoise(float64(x)*0.05, float64(z)*0.05) * 7)
		edge := float64(biomeInfoNoise.get(float64(x)*0.2, float64(z)*0.2))
		if large+edge < 0.3 && float64(biomeInfoNoise.get(float64(x)*0.09, float64(z)*0.09)) < 0.8 {
			t = 0.2
		}
	}
	if snowLevel := worldgen.SeaLevel + 17; y > snowLevel {
		v := temperatureNoise.get(float64(float32(x)/8), float64(float32(z)/8)) * 8
		return t - (v+float32(y)-float32(snowLevel))*0.05/40
	}
	return t
}

// precipitationAt is Biome.getPrecipitationAt for the biome at the cell.
func precipitationAt(biome string, x, y, z int) int {
	if _, _, ok := worldgen.BiomeClimate(biome); !ok {
		return worldgen.PrecipNone
	}
	if biomeTemperatureAt(biome, x, y, z) < 0.15 {
		return worldgen.PrecipSnow
	}
	return worldgen.PrecipRain
}

// precipAt is precipitationAt for a cell of a dimension's world, reading the
// biome there (not the column's surface biome).
func (h *hub) precipAt(dim, x, y, z int) int {
	w := h.worldFor(dim)
	if w == nil {
		return worldgen.PrecipNone
	}
	return precipitationAt(w.BiomeAt3D(x, y, z), x, y, z)
}
