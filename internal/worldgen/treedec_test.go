package worldgen

import (
	"math/rand"
	"testing"
)

// The ported tree decorators: vines and cocoa. What a bare placer cannot
// express — a jungle tree IS its vines — and the split the aliasing hid:
// jungle_tree and jungle_tree_no_vine place the same trunk and differ only
// here, worldgen growing the first and saplings the second.

func grownStates(name string, seed int64) map[uint32]int {
	c := TreeFeatures[name]
	out := map[uint32]int{}
	rng := rand.New(rand.NewSource(seed))
	set := func(x, y, z int, state uint32, leaf bool) { out[state]++ }
	PlaceTree(c, 0, 0, 0, rng, set, func(x, y, z int) bool { return true })
	return out
}

func countIn(states map[uint32]int, lo, hi uint32) int {
	n := 0
	for s, k := range states {
		if s >= lo && s <= hi {
			n += k
		}
	}
	return n
}

func TestJungleTreesGrowVinesAndCocoa(t *testing.T) {
	vlo, vhi := BlockRange("vine")
	clo, chi := BlockRange("cocoa")
	vines, cocoa := 0, 0
	for seed := int64(1); seed <= 40; seed++ {
		st := grownStates("jungle_tree", seed)
		vines += countIn(st, vlo, vhi)
		cocoa += countIn(st, clo, chi)
	}
	if vines == 0 {
		t.Error("forty jungle trees grew no vines at all")
	}
	if cocoa == 0 {
		t.Error("forty jungle trees grew no cocoa (gate is 0.2 — expect some)")
	}
	// …and the sapling-grown variant never does either.
	for seed := int64(1); seed <= 40; seed++ {
		st := grownStates("jungle_tree_no_vine", seed)
		if countIn(st, vlo, vhi)+countIn(st, clo, chi) > 0 {
			t.Fatalf("jungle_tree_no_vine grew a vine or cocoa (seed %d)", seed)
		}
	}
}

func TestSwampOaksHangVines(t *testing.T) {
	if _, ok := TreeFeatures["swamp_oak"]; !ok {
		t.Fatal("swamp_oak missing from the feature table")
	}
	vlo, vhi := BlockRange("vine")
	vines := 0
	for seed := int64(1); seed <= 40; seed++ {
		vines += countIn(grownStates("swamp_oak", seed), vlo, vhi)
	}
	if vines == 0 {
		t.Error("forty swamp oaks grew no vines")
	}
	// Plain oaks never do.
	for seed := int64(1); seed <= 40; seed++ {
		if countIn(grownStates("oak", seed), vlo, vhi) > 0 {
			t.Fatalf("a plain oak grew a vine (seed %d)", seed)
		}
	}
}
