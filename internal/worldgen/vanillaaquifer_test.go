package worldgen

import (
	"math"
	"testing"
)

// The aquifer decides exactly as 26.3's NoiseBasedAquifer does, given the
// same inputs. The oracle ran the server's own aquifer (Aquifer.Config.create
// over the overworld's barrier, floodedness, spread and lava noises and the
// "minecraft:aquifer" random, seed 1, one aquifer per chunk as NoiseChunk
// makes them) with a synthetic surface level, 30 + 60·cave_layer(x, 0, z),
// and exclusion, pillar_rareness(x, y, z) − 0.4 > 0, both of which the
// engine can sample bit for bit; it called computeSubstance over a lattice
// of cells in ten chunks with density (floorMod(7x + 13y + 3z, 11) − 8)/20.
// The answers' FNV-1a hash over S (stone stays), A, W, L in that order, and
// a few of them by hand (lava from the lava noise, the barrier rock).
func TestVanillaAquiferMatchesVanilla(t *testing.T) {
	v := newVanillaCaves(1)
	surface := func(x, z int) int {
		return int(math.Floor(float64(float32(vcSample(v.caveLayer, x, 0, z, 1, 0)*60) + 30)))
	}
	excluded := func(x, y, z int) bool { return vcSample(v.pillarRare, x, y, z, 1, 1)+-0.4 > 0 }
	letter := map[int8]byte{vaqSolid: 'S', vaqAir: 'A', vaqWater: 'W', vaqLava: 'L'}
	got := map[[3]int]byte{}
	h := uint64(1469598103934665603)
	counts := map[byte]int{}
	for _, c := range [][2]int{{0, 0}, {5, -3}, {-7, 9}, {20, -20}, {-40, 3}, {100, 100}, {-200, 50}, {37, -150}, {300, -300}, {-90, -90}} {
		aq := newVaqChunk(v.aquifer, c[0]*16, -64, c[1]*16, c[0]*16+15, 319, c[1]*16+15, surface, excluded)
		for lx := 0; lx < 16; lx += 3 {
			for lz := 0; lz < 16; lz += 5 {
				for y := -64; y < 140; y++ {
					x, z := c[0]*16+lx, c[1]*16+lz
					d := float64(((x*7+y*13+z*3)%11+11)%11-8) / 20.0
					k := letter[aq.substance(x, y, z, d)]
					got[[3]int{x, y, z}] = k
					counts[k]++
					h ^= uint64(k)
					h *= 1099511628211
				}
			}
		}
	}
	t.Logf("aquifer lattice: %d stone, %d air, %d water, %d lava", counts['S'], counts['A'], counts['W'], counts['L'])
	for _, c := range []struct {
		x, y, z int
		want    byte
	}{
		{1612, -23, 1610, 'L'}, // a deep aquifer the lava noise makes lava
		{1615, -24, 1605, 'L'},
		{0, -63, 0, 'L'},   // the global lava below -54
		{80, 49, -48, 'S'}, // barrier rock between two levels
		{-1425, 10, -1425, 'S'},
		{80, 56, -48, 'W'}, // under the synthetic sea
		{-112, 0, 144, 'W'},
		{0, -49, 0, 'A'},
	} {
		if g := got[[3]int{c.x, c.y, c.z}]; g != c.want {
			t.Errorf("aquifer at %d,%d,%d = %c, vanilla %c", c.x, c.y, c.z, g, c.want)
		}
	}
	if counts['S'] != 9628 || counts['A'] != 28976 || counts['W'] != 8383 || counts['L'] != 1973 || h != 2864526020016361634 {
		t.Errorf("aquifer lattice hash %d (counts %v), vanilla 2864526020016361634 (S 9628, A 28976, W 8383, L 1973)", h, counts)
	}
}

// The preliminary surface over the depth model: the highest multiple of
// eight at least 35/factor under the surface.
func TestVanillaPrelimSurface(t *testing.T) {
	for _, c := range []struct {
		h      int
		factor float32
		want   int
	}{
		{80, vcFactorLand, 72},  // 80 − 5.6 = 74.4 → 72
		{70, vcFactorLand, 64},  // 64.4 → 64
		{64, vcFactorLand, 56},  // 58.4 → 56
		{40, vcFactorOcean, 24}, // 40 − 8.9 = 31.1 → 24
		{5, vcFactorOcean, -8},  // -3.9 → -8
		{-50, vcFactorOcean, -64},
	} {
		if got := vcPrelimSurface(c.h, c.factor); got != c.want {
			t.Errorf("prelim surface under h=%d (factor %v) = %d, want %d", c.h, c.factor, got, c.want)
		}
	}
}
