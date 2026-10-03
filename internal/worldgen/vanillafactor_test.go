package worldgen

import (
	"math"
	"testing"
)

// The factor's inputs and the factor itself are vanilla's, bit for bit:
// overworld/continents, erosion, ridges, ridges_folded and factor sampled by
// the 26.3 server (RandomState.sampleBlockValueUncached, seed 1) at every
// 37th block over ±6000 — 105625 columns — hashed (FNV-1a over the five
// floats' bits per column, in order), and a few of them by hand.
func TestVanillaFactorMatchesVanilla(t *testing.T) {
	f := newVanillaFactor(1)
	eval := func(x, z int) [5]uint32 {
		c, e, r := f.climate(x, z)
		return [5]uint32{math.Float32bits(c), math.Float32bits(e), math.Float32bits(r),
			math.Float32bits(vfFold(r)), math.Float32bits(f.at(x, z, nil))}
	}
	for _, c := range []struct {
		x, z int
		bits [5]uint32
	}{
		{-6000, -2448, [5]uint32{1050985211, 3201886414, 1034716620, 3208596950, 1081605670}},
		{-5001, -4113, [5]uint32{1042771592, 1046107865, 3195178441, 3197381461, 1086953882}},
		{-4002, -5778, [5]uint32{3212566957, 3197663223, 3190315206, 3204562286, 1081920717}},
		{-3040, 4582, [5]uint32{3206379649, 3205523226, 3206240228, 1062340524, 1081920717}},
		{-2041, 2917, [5]uint32{3193872850, 3190941546, 3206545251, 1063255592, 1081920717}},
		{-1042, 1252, [5]uint32{3203570553, 1033232742, 3198540499, 3169253504, 1081920717}},
		{-43, -413, [5]uint32{3207741503, 1026498236, 3198762385, 3154700736, 1081920717}},
		{956, -2078, [5]uint32{3200629507, 1042634426, 3202986087, 1052578100, 1081920717}},
		{1955, -3743, [5]uint32{1053281211, 3171267923, 3192644405, 3201182515, 1083354414}},
		{2954, -5408, [5]uint32{3181452178, 3199738633, 1052634019, 1038551968, 1085085419}},
		{3916, 4952, [5]uint32{1042190262, 1035523223, 1042981686, 3204449690, 1083644011}},
		{4915, 3287, [5]uint32{3190667293, 3192501186, 3195113731, 3197478523, 1083435353}},
		{5914, 1622, [5]uint32{1054612229, 3193939998, 3190413577, 3204488506, 1086021810}}} {
		if got := eval(c.x, c.z); got != c.bits {
			t.Errorf("climate/factor at %d,%d = %v, vanilla %v", c.x, c.z, got, c.bits)
		}
	}
	h, n := uint64(1469598103934665603), 0
	for x := -6000; x <= 6000; x += 37 {
		for z := -6000; z <= 6000; z += 37 {
			for _, b := range eval(x, z) {
				h ^= uint64(b)
				h *= 1099511628211
			}
			n++
		}
	}
	if n != 105625 || h != 12079163738829396196 {
		t.Errorf("%d columns hash %d, vanilla 105625 columns hash 12079163738829396196", n, h)
	}
}

// The coast rule: under the engine's sea the factor is vanilla's ocean
// value; on its land, never.
func TestVanillaFactorCoast(t *testing.T) {
	f := newVanillaFactor(1)
	sea, land := true, false
	for x := -3000; x <= 3000; x += 97 {
		for z := -3000; z <= 3000; z += 89 {
			if v := f.at(x, z, &sea); v != 3.95 {
				t.Fatalf("factor under the sea at %d,%d = %v", x, z, v)
			}
			if v := f.at(x, z, &land); v == 3.95 && f.at(x, z, nil) != 3.95 {
				t.Fatalf("factor on land at %d,%d took the ocean value", x, z)
			}
		}
	}
}
