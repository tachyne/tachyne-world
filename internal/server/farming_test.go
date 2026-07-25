package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The tillable table is a direct port of HoeItem.TILLABLES: grass block, dirt
// path and dirt become farmland; coarse dirt degrades to plain dirt; rooted
// dirt becomes dirt AND pops hanging roots. Anything else is untouched.
func TestTillablesMatchVanilla(t *testing.T) {
	cases := []struct {
		block string
		into  string
		drop  bool
	}{
		{"grass_block", "farmland", false},
		{"dirt_path", "farmland", false},
		{"dirt", "farmland", false},
		{"coarse_dirt", "dirt", false},
		{"rooted_dirt", "dirt", true},
	}
	for _, c := range cases {
		res, ok := tillables[worldgen.BlockBase(c.block)]
		if !ok {
			t.Errorf("%s should be tillable", c.block)
			continue
		}
		if want := worldgen.BlockBase(c.into); res.into != want {
			t.Errorf("%s tills into %d, want %s (%d)", c.block, res.into, c.into, want)
		}
		if got := res.drop != 0; got != c.drop {
			t.Errorf("%s drop: got %v, want %v", c.block, got, c.drop)
		}
	}

	// Not in TILLABLES.
	for _, name := range []string{"stone", "sand", "gravel", "podzol", "mycelium"} {
		if _, ok := tillables[worldgen.BlockBase(name)]; ok {
			t.Errorf("%s must not be tillable", name)
		}
	}
}

// Only rooted dirt tills from any face; the rest require air above (vanilla's
// onlyIfAirAbove predicate).
func TestOnlyRootedDirtSkipsAirAbove(t *testing.T) {
	for state, res := range tillables {
		wantAirAbove := state != rootedDirtBlock
		if res.airAbove != wantAirAbove {
			t.Errorf("state %d: airAbove=%v, want %v", state, res.airAbove, wantAirAbove)
		}
	}
}

func TestIsHoe(t *testing.T) {
	for _, name := range []string{"wooden_hoe", "stone_hoe", "iron_hoe", "golden_hoe",
		"diamond_hoe", "netherite_hoe"} {
		if !isHoe(itemByName[name]) {
			t.Errorf("%s should be a hoe", name)
		}
	}
	for _, name := range []string{"iron_shovel", "diamond_pickaxe", "wheat_seeds", "stick"} {
		if isHoe(itemByName[name]) {
			t.Errorf("%s must not be a hoe", name)
		}
	}
}

// The planting table mirrors vanilla's ItemNameBlockItem registrations. These
// are exactly the items the generated item->block map cannot cover, because
// item and block are named differently on purpose.
func TestCropForSeedMatchesVanilla(t *testing.T) {
	cases := []struct{ item, block string }{
		{"wheat_seeds", "wheat"},
		{"carrot", "carrots"},
		{"potato", "potatoes"},
		{"beetroot_seeds", "beetroots"},
		{"melon_seeds", "melon_stem"},
		{"pumpkin_seeds", "pumpkin_stem"},
		{"torchflower_seeds", "torchflower_crop"},
		{"nether_wart", "nether_wart"},
	}
	for _, c := range cases {
		got, ok := cropForSeed(itemByName[c.item])
		if !ok {
			t.Errorf("%s should be plantable", c.item)
			continue
		}
		if want := worldgen.BlockBase(c.block); got.block != want {
			t.Errorf("%s plants %d, want %s (%d)", c.item, got.block, c.block, want)
		}
	}
	for _, name := range []string{"stick", "diamond", "wheat", "apple"} {
		if _, ok := cropForSeed(itemByName[name]); ok {
			t.Errorf("%s must not be plantable", name)
		}
	}
}

// The light gate is CropBlock.canSurvive's, so it applies to the CropBlock
// family only. StemBlock (melon/pumpkin) and NetherWartBlock extend
// VegetationBlock and never check light — getting this wrong would silently
// refuse legitimate stem planting in dim light.
func TestOnlyCropFamilyGatesOnLight(t *testing.T) {
	lit := []string{"wheat_seeds", "carrot", "potato", "beetroot_seeds", "torchflower_seeds"}
	unlit := []string{"melon_seeds", "pumpkin_seeds", "nether_wart"}

	for _, name := range lit {
		c, ok := cropForSeed(itemByName[name])
		if !ok {
			t.Fatalf("%s not plantable", name)
		}
		if !c.needsLight {
			t.Errorf("%s is a CropBlock and must gate on light", name)
		}
	}
	for _, name := range unlit {
		c, ok := cropForSeed(itemByName[name])
		if !ok {
			t.Fatalf("%s not plantable", name)
		}
		if c.needsLight {
			t.Errorf("%s is not a CropBlock and must NOT gate on light", name)
		}
	}
}

// Nether wart is the only one rooted on soul sand (#supports_nether_wart);
// everything else wants farmland (#supports_crops, which is farmland alone).
func TestNetherWartIsTheOnlySoulSandPlant(t *testing.T) {
	c, ok := cropForSeed(itemNetherWart)
	if !ok || !c.soulSand {
		t.Fatalf("nether wart should be soul-sand rooted: %+v ok=%v", c, ok)
	}
	for _, name := range []string{"wheat_seeds", "carrot", "melon_seeds", "pumpkin_seeds"} {
		c, _ := cropForSeed(itemByName[name])
		if c.soulSand {
			t.Errorf("%s must root on farmland, not soul sand", name)
		}
	}
}

// Farmland is recognised at every moisture level (0..7) and nothing adjacent in
// the state table is mistaken for it.
func TestIsFarmlandCoversAllMoisture(t *testing.T) {
	for m := uint32(0); m <= 7; m++ {
		if !isFarmland(farmlandMin + m) {
			t.Errorf("farmland moisture %d not recognised", m)
		}
	}
	if isFarmland(farmlandMin - 1) {
		t.Error("state below farmland range recognised as farmland")
	}
	if isFarmland(farmlandMin + 8) {
		t.Error("state above farmland range recognised as farmland")
	}
	if isFarmland(dirtBlock) || isFarmland(grassBlock) {
		t.Error("dirt/grass must not count as farmland")
	}
}
