// Package worldgen produces deterministic, seed-driven terrain. It is our own
// generator (not vanilla-identical): the same seed always yields the same world,
// built from layered noise that mimics real-world signals — continental shelves,
// hills, and climate — rather than arbitrary noise.
package worldgen

import "math"

// Perlin is a seeded 2D improved-Perlin noise source with fractal (fBm) support.
type Perlin struct {
	perm [512]int
}

// NewPerlin builds a permutation table deterministically from seed.
func NewPerlin(seed int64) *Perlin {
	var p [256]int
	for i := range p {
		p[i] = i
	}
	// Fisher–Yates shuffle driven by a small LCG so the table is seed-stable.
	r := uint64(seed) ^ 0x9e3779b97f4a7c15
	for i := 255; i > 0; i-- {
		r = r*6364136223846793005 + 1442695040888963407
		j := int(r>>33) % (i + 1)
		p[i], p[j] = p[j], p[i]
	}
	var per Perlin
	for i := 0; i < 512; i++ {
		per.perm[i] = p[i&255]
	}
	return &per
}

func fade(t float64) float64       { return t * t * t * (t*(t*6-15) + 10) }
func lerp(a, b, t float64) float64 { return a + t*(b-a) }

func grad(hash int, x, y float64) float64 {
	switch hash & 3 {
	case 0:
		return x + y
	case 1:
		return -x + y
	case 2:
		return x - y
	default:
		return -x - y
	}
}

// Noise2 returns 2D Perlin noise in roughly [-1, 1].
func (p *Perlin) Noise2(x, y float64) float64 {
	xi := int(math.Floor(x)) & 255
	yi := int(math.Floor(y)) & 255
	xf := x - math.Floor(x)
	yf := y - math.Floor(y)
	u := fade(xf)
	v := fade(yf)
	aa := p.perm[p.perm[xi]+yi]
	ab := p.perm[p.perm[xi]+yi+1]
	ba := p.perm[p.perm[xi+1]+yi]
	bb := p.perm[p.perm[xi+1]+yi+1]
	x1 := lerp(grad(aa, xf, yf), grad(ba, xf-1, yf), u)
	x2 := lerp(grad(ab, xf, yf-1), grad(bb, xf-1, yf-1), u)
	return lerp(x1, x2, v)
}

func grad3(hash int, x, y, z float64) float64 {
	h := hash & 15
	u := x
	if h >= 8 {
		u = y
	}
	v := z
	if h < 4 {
		v = y
	} else if h == 12 || h == 14 {
		v = x
	}
	if h&1 != 0 {
		u = -u
	}
	if h&2 != 0 {
		v = -v
	}
	return u + v
}

// Noise3 returns 3D Perlin noise in roughly [-1, 1]. Used for caves, where the
// third dimension lets tunnels wind up and down.
func (p *Perlin) Noise3(x, y, z float64) float64 {
	xi := int(math.Floor(x)) & 255
	yi := int(math.Floor(y)) & 255
	zi := int(math.Floor(z)) & 255
	xf := x - math.Floor(x)
	yf := y - math.Floor(y)
	zf := z - math.Floor(z)
	u, v, w := fade(xf), fade(yf), fade(zf)

	aaa := p.perm[p.perm[p.perm[xi]+yi]+zi]
	aba := p.perm[p.perm[p.perm[xi]+yi+1]+zi]
	aab := p.perm[p.perm[p.perm[xi]+yi]+zi+1]
	abb := p.perm[p.perm[p.perm[xi]+yi+1]+zi+1]
	baa := p.perm[p.perm[p.perm[xi+1]+yi]+zi]
	bba := p.perm[p.perm[p.perm[xi+1]+yi+1]+zi]
	bab := p.perm[p.perm[p.perm[xi+1]+yi]+zi+1]
	bbb := p.perm[p.perm[p.perm[xi+1]+yi+1]+zi+1]

	x1 := lerp(grad3(aaa, xf, yf, zf), grad3(baa, xf-1, yf, zf), u)
	x2 := lerp(grad3(aba, xf, yf-1, zf), grad3(bba, xf-1, yf-1, zf), u)
	y1 := lerp(x1, x2, v)
	x1 = lerp(grad3(aab, xf, yf, zf-1), grad3(bab, xf-1, yf, zf-1), u)
	x2 = lerp(grad3(abb, xf, yf-1, zf-1), grad3(bbb, xf-1, yf-1, zf-1), u)
	y2 := lerp(x1, x2, v)
	return lerp(y1, y2, w)
}

// FBm sums octaves of Noise2 into fractal Brownian motion, normalized to ~[-1,1].
// More octaves add finer detail; gain < 1 makes each octave contribute less.
func (p *Perlin) FBm(x, y float64, octaves int, lacunarity, gain float64) float64 {
	freq, amp, sum, norm := 1.0, 1.0, 0.0, 0.0
	for i := 0; i < octaves; i++ {
		sum += amp * p.Noise2(x*freq, y*freq)
		norm += amp
		freq *= lacunarity
		amp *= gain
	}
	return sum / norm
}
