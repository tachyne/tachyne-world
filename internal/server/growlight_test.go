package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Growth light gates, ported from the vanilla randomTick implementations.
// These pin the thing the old sky-column approximation got wrong: vanilla asks
// for BRIGHTNESS, so artificial light works, and a roofed farm is fine as long
// as it is lit.

// carveChamber fills a 5x5x5 block of solid stone and hollows out the listed
// cells, giving a genuinely unlit pocket.
//
// These tests sit DEEP (y=40), not high in open air like the older growth
// tests. Lighting here is height-capped — the sky flood fill stops just above
// terrain — so blocks placed high above the surface never darken anything and a
// "sealed box" at y=200 still reads brightness 12. Underground the light values
// are real.
func carveChamber(h *hub, cells ...[3]int) {
	c0 := cells[0]
	for dx := -2; dx <= 2; dx++ {
		for dy := -2; dy <= 2; dy++ {
			for dz := -2; dz <= 2; dz++ {
				h.world.SetBlock(c0[0]+dx, c0[1]+dy, c0[2]+dz, worldgen.Stone)
			}
		}
	}
	for _, c := range cells {
		h.world.SetBlock(c[0], c[1], c[2], worldgen.Air)
	}
}

// A torch-lit indoor farm grows in vanilla. Under the old sky-column check it
// could not, because the roof blocked the column outright.
func TestCropGrowsUnderArtificialLight(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 40, 40, 40

	// A carved two-cell pocket: the crop, and a glowstone beside it.
	carveChamber(h, [3]int{x, y, z}, [3]int{x + 1, y, z})
	h.world.SetBlock(x+1, y, z, worldgen.BlockID("glowstone"))
	h.world.SetBlock(x, y-1, z, farmlandMin+7) // moist farmland, fastest growth
	base := worldgen.BlockBase("wheat")
	h.world.SetBlock(x, y, z, base)

	if got := h.plantBrightness(x, y, z, 0); got < 9 {
		t.Fatalf("test setup: brightness %d under the lamp, need >= 9", got)
	}

	for i := 0; i < 400 && h.world.At(x, y, z) < base+7; i++ {
		h.tickCrop(players, x, y, z, h.world.At(x, y, z))
	}
	if got := h.world.At(x, y, z); got == base {
		t.Errorf("wheat did not grow under artificial light: state %d", got)
	}
}

// The flip side: genuinely dark ground must not grow. Sealed, unlit.
func TestCropDoesNotGrowInTheDark(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 60, 40, 60

	carveChamber(h, [3]int{x, y, z})
	h.world.SetBlock(x, y-1, z, farmlandMin+7)
	base := worldgen.BlockBase("wheat")
	h.world.SetBlock(x, y, z, base)

	if got := h.plantBrightness(x, y, z, 0); got >= 9 {
		t.Fatalf("test setup: brightness %d in the sealed box, want < 9", got)
	}
	for i := 0; i < 400; i++ {
		h.tickCrop(players, x, y, z, h.world.At(x, y, z))
	}
	if got := h.world.At(x, y, z); got != base {
		t.Errorf("wheat grew in the dark: state %d", got)
	}
}

// SaplingBlock.randomTick rolls nextInt(7) before advancing. Without it a
// sapling advanced on every lit random tick — roughly seven times too fast.
// Over many trials the observed rate must sit near 1/7, not 1.
func TestSaplingAdvancesAtVanillaRate(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	base, _ := worldgen.BlockRange("oak_sapling")
	const trials = 3000
	advanced := 0
	for i := 0; i < trials; i++ {
		x, y, z := 100+i%50, 200, 100+i/50
		h.world.SetBlock(x, y, z, base)
		h.tickSapling(players, x, y, z, base)
		if h.world.At(x, y, z) != base {
			advanced++
		}
	}
	rate := float64(advanced) / float64(trials)
	if rate < 0.10 || rate > 0.19 { // 1/7 = 0.143, generous band for 3000 trials
		t.Errorf("sapling advance rate %.3f, want ~0.143 (1 in 7)", rate)
	}
}

// TorchflowerCropBlock: AGE tops out at 1 but getMaxAge() is 2, so the last
// step swaps the crop for the torchflower block.
func TestTorchflowerGrowsIntoTheFlower(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 20, 200, 20

	h.world.SetBlock(x, y-1, z, farmlandMin+7)
	h.world.SetBlock(x, y, z, torchflowerCropMin)

	flower := worldgen.BlockID("torchflower")
	for i := 0; i < 4000 && h.world.At(x, y, z) != flower; i++ {
		h.tickTorchflower(players, x, y, z, h.world.At(x, y, z))
	}
	if got := h.world.At(x, y, z); got != flower {
		t.Errorf("torchflower crop never became the flower: state %d (want %d)", got, flower)
	}
}

// The pitcher crop's state layout is arithmetic, so pin it: the block's default
// state is age 0 LOWER, one past its minimum (age 0 UPPER).
func TestPitcherStateLayout(t *testing.T) {
	if got := worldgen.BlockID("pitcher_crop"); got != pitcherLower(0) {
		t.Errorf("default pitcher_crop state %d, want age-0 lower %d", got, pitcherLower(0))
	}
	if pitcherUpper(0) != pitcherCropMin {
		t.Errorf("age-0 upper %d, want the block minimum %d", pitcherUpper(0), pitcherCropMin)
	}
	if pitcherLower(4) != pitcherCropMax {
		t.Errorf("age-4 lower %d, want the block maximum %d", pitcherLower(4), pitcherCropMax)
	}
	for age := 0; age <= 4; age++ {
		if a, lower := pitcherAgeHalf(pitcherLower(age)); a != age || !lower {
			t.Errorf("lower age %d round-tripped to (%d, lower=%v)", age, a, lower)
		}
		if a, lower := pitcherAgeHalf(pitcherUpper(age)); a != age || lower {
			t.Errorf("upper age %d round-tripped to (%d, lower=%v)", age, a, lower)
		}
	}
	// isDouble: two cells from age 3.
	for age := 0; age <= 2; age++ {
		if pitcherIsDouble(age) {
			t.Errorf("age %d should be single-cell", age)
		}
	}
	for age := 3; age <= 4; age++ {
		if !pitcherIsDouble(age) {
			t.Errorf("age %d should be double", age)
		}
	}
}

// Growing past age 2 writes the upper half above the plant, and only the lower
// half ticks.
func TestPitcherGrowsAnUpperHalf(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 30, 200, 30

	h.world.SetBlock(x, y-1, z, farmlandMin+7)
	h.world.SetBlock(x, y, z, pitcherLower(0))

	for i := 0; i < 6000 && h.world.At(x, y, z) != pitcherLower(4); i++ {
		h.tickPitcher(players, x, y, z, h.world.At(x, y, z))
	}
	if got := h.world.At(x, y, z); got != pitcherLower(4) {
		t.Fatalf("pitcher crop never matured: state %d, want %d", got, pitcherLower(4))
	}
	if got := h.world.At(x, y+1, z); got != pitcherUpper(4) {
		t.Errorf("no upper half above a mature pitcher crop: state %d, want %d", got, pitcherUpper(4))
	}

	// The upper half must not tick on its own.
	before := h.world.At(x, y+1, z)
	h.tickPitcher(players, x, y+1, z, before)
	if got := h.world.At(x, y+1, z); got != before {
		t.Errorf("upper half advanced itself: %d -> %d", before, got)
	}
}

// A blocked cell above stops the plant going double (canGrowInto).
func TestPitcherWillNotGrowIntoASolidBlock(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 35, 200, 35

	h.world.SetBlock(x, y-1, z, farmlandMin+7)
	h.world.SetBlock(x, y, z, pitcherLower(2)) // one step short of double
	h.world.SetBlock(x, y+1, z, worldgen.Stone)

	for i := 0; i < 2000; i++ {
		h.tickPitcher(players, x, y, z, h.world.At(x, y, z))
	}
	if got := h.world.At(x, y, z); got != pitcherLower(2) {
		t.Errorf("pitcher grew under a solid block: state %d", got)
	}
	if got := h.world.At(x, y+1, z); got != worldgen.Stone {
		t.Errorf("pitcher overwrote the block above: state %d", got)
	}
}
