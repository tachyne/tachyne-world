package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A table that rolled nothing must not look like a missing table. When it
// did, a failed roll fell through to the hand-written fallback and was rolled
// again: short grass broken by hand dropped seeds 23% of the time, not 12.5%.
func TestEmptyRollIsNotAMissingTable(t *testing.T) {
	h := newHub(world.New(1))
	glass := worldgen.BlockID("glass") // drops only with Silk Touch
	ds := h.evalBlockLoot(lootCtx{state: glass, rng: h.rng.Intn, randf: h.rng.Float64})
	if ds == nil || len(ds) != 0 {
		t.Fatalf("glass without silk: got %v (nil=%v), want an empty non-nil result", ds, ds == nil)
	}
	if h.evalBlockLoot(lootCtx{state: worldgen.Air, rng: h.rng.Intn, randf: h.rng.Float64}) != nil {
		t.Fatal("air has no loot table and must say so with nil")
	}
}

// The player-break path — real table, falling back only when there is none —
// gives vanilla's 12.5% seed rate for short grass.
func TestShortGrassSeedRateIsVanillas(t *testing.T) {
	h := newHub(world.New(1))
	const n = 200000
	seeds := 0
	for i := 0; i < n; i++ {
		ds := h.evalBlockLoot(lootCtx{state: worldgen.ShortGrass, rng: h.rng.Intn, randf: h.rng.Float64})
		if ds == nil {
			ds = h.rollDrops(worldgen.ShortGrass)
		}
		for _, d := range ds {
			if d.item == itemWheatSeeds {
				seeds++
			}
		}
	}
	if rate := float64(seeds) / n; math.Abs(rate-0.125) > 0.006 {
		t.Fatalf("seed rate %.4f, want 0.125", rate)
	}
}

// A decaying leaf drops its own sapling, and only oak and dark oak drop apples.
// The generic table this replaced gave every leaf an oak sapling.
func TestDecayingLeavesDropTheirOwnSapling(t *testing.T) {
	h := newHub(world.New(1))
	count := func(leaf string) map[int32]int {
		got := map[int32]int{}
		for i := 0; i < 20000; i++ {
			for _, d := range h.rollDrops(worldgen.BlockID(leaf)) {
				got[d.item] += d.count
			}
		}
		return got
	}
	birch := count("birch_leaves")
	if birch[itemByName["birch_sapling"]] == 0 {
		t.Error("birch leaves never dropped a birch sapling")
	}
	if birch[itemByName["oak_sapling"]] != 0 {
		t.Error("birch leaves dropped an oak sapling")
	}
	if birch[itemByName["apple"]] != 0 {
		t.Error("birch leaves dropped an apple; only oak and dark oak do")
	}
	if oak := count("oak_leaves"); oak[itemByName["apple"]] == 0 {
		t.Error("oak leaves never dropped an apple in 20000 decays")
	}
	if mangrove := count("mangrove_leaves"); mangrove[itemByName["mangrove_propagule"]] != 0 {
		t.Error("mangrove leaves are not what drops propagules")
	}
}

// A wall torch, sign, banner or head drops from the standing block's table,
// which it names itself; those used to have no table here at all.
func TestWallVariantsUseTheStandingTable(t *testing.T) {
	h := newHub(world.New(1))
	for wall, want := range map[string]string{
		"wall_torch":            "torch",
		"oak_wall_sign":         "oak_sign",
		"white_wall_banner":     "white_banner",
		"skeleton_wall_skull":   "skeleton_skull",
		"oak_wall_hanging_sign": "oak_hanging_sign",
	} {
		ds := h.rollDrops(worldgen.BlockID(wall))
		if len(ds) != 1 || ds[0].item != itemByName[want] {
			t.Errorf("%s drops %v, want one %s", wall, ds, want)
		}
	}
	// And the one table the evaluator cannot run still gives the pot back.
	if ds := h.rollDrops(worldgen.BlockID("decorated_pot")); len(ds) != 1 || ds[0].item != itemDecoratedPot {
		t.Errorf("decorated pot drops %v, want itself", ds)
	}
}
