package worldgen

// Dense O(1) tables for the per-block light queries. LightFilter and
// LightEmission are the hottest calls in chunk lighting — profiled at ~40% of
// a chunk's first-touch CPU as per-block binary searches. These tables are
// built once at init from the same generated range data, so results are
// byte-identical; the flood fill (world/light.go) reads these instead.
var (
	lightFilterTab   []uint8
	lightEmissionTab []uint8
)

func init() {
	max := lightFilterRanges[len(lightFilterRanges)-1].Max
	if m := lightRanges[len(lightRanges)-1].hi; m > max {
		max = m
	}
	lightFilterTab = make([]uint8, max+1)
	for i := range lightFilterTab {
		lightFilterTab[i] = Opaque
	}
	for _, r := range lightFilterRanges {
		for s := r.Min; s <= r.Max; s++ {
			lightFilterTab[s] = uint8(r.Filter)
		}
	}
	lightEmissionTab = make([]uint8, max+1)
	for _, r := range lightRanges {
		for s := r.lo; s <= r.hi; s++ {
			lightEmissionTab[s] = r.lvl
		}
	}
	// Copper bulbs emit by STATE (lit) — a shape the generated per-block ranges
	// can't carry. Patch their lit states in (copperBulbLines is built in
	// copperbulb.go, which sorts before this file so its init runs first).
	for _, l := range copperBulbLines {
		for off := uint32(0); off < 4; off++ {
			if s := l.base + off; int(s) < len(lightEmissionTab) {
				lightEmissionTab[s] = copperBulbEmission(s)
			}
		}
	}
}

// LightFilterFast is LightFilter as a flat table lookup (identical results).
func LightFilterFast(state uint32) uint8 {
	if int(state) < len(lightFilterTab) {
		return lightFilterTab[state]
	}
	return Opaque
}

// LightEmissionFast is LightEmission as a flat table lookup (identical results).
func LightEmissionFast(state uint32) uint8 {
	if int(state) < len(lightEmissionTab) {
		return lightEmissionTab[state]
	}
	return 0
}
