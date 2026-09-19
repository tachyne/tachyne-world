package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func bonemealWorld(t *testing.T) (*hub, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	pl.x, pl.y, pl.z = 40.5, 180, 0.5
	return h, players
}

func withProp(s uint32, name, val string) uint32 {
	info, _ := worldgen.InfoForState(s)
	return worldgen.SetProperty(info, s, name, val)
}

func prop(s uint32, name string) string {
	info, _ := worldgen.InfoForState(s)
	return worldgen.GetProperty(info, s, name)
}

// Every extra bone-meal target in one sweep: what each block becomes.
func TestBoneMealMoreTargets(t *testing.T) {
	h, players := bonemealWorld(t)
	w := h.world
	apply := func(x, y, z int) bool { return h.applyBoneMeal(players, 0, x, y, z, w.At(x, y, z)) }

	// A sunflower pops a sunflower.
	for x := 0; x <= 3; x++ {
		w.SetBlock(x, 179, 0, worldgen.Dirt) // plants need soil, or the growth's own block change drops them
	}
	w.SetBlock(0, 180, 0, worldgen.BlockID("sunflower"))
	if !apply(0, 180, 0) {
		t.Error("sunflower refused")
	}
	popped := 0
	for _, it := range h.items {
		if it.item == int32(itemByName["sunflower"]) {
			popped += it.count
		}
	}
	if popped != 1 {
		t.Errorf("sunflowers popped %d, want 1", popped)
	}
	// Short grass grows tall; a fern a large fern.
	w.SetBlock(1, 180, 0, shortGrassState)
	if !apply(1, 180, 0) || prop(w.At(1, 180, 0), "half") != "lower" || w.At(1, 180, 0) == shortGrassState || prop(w.At(1, 181, 0), "half") != "upper" {
		t.Errorf("short grass → %d / %d", w.At(1, 180, 0), w.At(1, 181, 0))
	}
	w.SetBlock(2, 180, 0, fernState)
	w.SetBlock(2, 181, 0, worldgen.Stone)
	if apply(2, 180, 0) {
		t.Error("a fern grew into stone")
	}
	// Petals: three become four; four pop a copy.
	petals := worldgen.BlockID("pink_petals")
	name, n := segmentAmount(func() worldgen.BlockInfo { i, _ := worldgen.InfoForState(petals); return i }(), petals)
	if name == "" || n == 0 {
		t.Fatalf("no segment property on pink petals: %q %d", name, n)
	}
	w.SetBlock(3, 180, 0, withProp(petals, name, "3"))
	if !apply(3, 180, 0) || prop(w.At(3, 180, 0), name) != "4" {
		t.Errorf("petals 3 → %s", prop(w.At(3, 180, 0), name))
	}
	before := len(h.items)
	if !apply(3, 180, 0) || len(h.items) != before+1 {
		t.Error("four petals did not pop a copy")
	}
	// Sea pickle on coral in water: the clicked one fills out, others sprout.
	for x := -4; x <= 4; x++ {
		for z := -4; z <= 4; z++ {
			w.SetBlock(x, 170, z, worldgen.BlockBase("tube_coral_block"))
			w.SetBlock(x, 171, z, worldgen.Water)
			w.SetBlock(x, 172, z, worldgen.Water)
		}
	}
	pickle := withProp(withProp(worldgen.BlockBase("sea_pickle"), "pickles", "1"), "waterlogged", "true")
	w.SetBlock(0, 171, 0, pickle)
	if !apply(0, 171, 0) || prop(w.At(0, 171, 0), "pickles") != "4" {
		t.Errorf("pickle → %s", prop(w.At(0, 171, 0), "pickles"))
	}
	// Seagrass grows tall under water.
	w.SetBlock(2, 171, 2, seagrassState)
	if !apply(2, 171, 2) || prop(w.At(2, 171, 2), "half") != "lower" || prop(w.At(2, 172, 2), "half") != "upper" {
		t.Error("seagrass did not grow tall")
	}
	// Kelp head grows one; its plant body grows the head.
	kelp := growingPlants[0]
	w.SetBlock(-2, 171, -2, kelp.headAt(3, false))
	if !apply(-2, 171, -2) || w.At(-2, 171, -2) != kelp.body || kelp.age(w.At(-2, 172, -2)) != 4 {
		t.Errorf("kelp: %d / %d", w.At(-2, 171, -2), w.At(-2, 172, -2))
	}
	w.SetBlock(-2, 173, -2, worldgen.Water)
	if !apply(-2, 171, -2) || kelp.age(w.At(-2, 173, -2)) != 5 {
		t.Error("kelp body did not grow the head")
	}
	// Twisting vines grow at least one.
	tw := growingPlants[1]
	w.SetBlock(4, 180, 4, tw.headAt(0, false))
	if !apply(4, 180, 4) || w.At(4, 180, 4) != tw.body {
		t.Error("twisting vines did not grow")
	}
	// Cave vines: a bare head fruits.
	cv := growingPlants[3]
	w.SetBlock(-4, 178, -4, worldgen.Stone)
	w.SetBlock(-4, 177, -4, cv.headAt(2, false))
	if !apply(-4, 177, -4) || w.At(-4, 177, -4) != cv.headAt(2, true) {
		t.Error("cave vines did not fruit")
	}
	if apply(-4, 177, -4) {
		t.Error("fruited cave vines took more meal")
	}
	// Big dripleaf: stem below, leaf above.
	w.SetBlock(5, 180, -3, bigDripleafState("north", false))
	if !apply(5, 180, -3) || !inRange(w.At(5, 180, -3), dripleafStemRng) || !inRange(w.At(5, 181, -3), bigDripleafRange) {
		t.Error("big dripleaf did not grow")
	}
	// Small dripleaf becomes a big one, two to five tall.
	small := worldgen.BlockID("small_dripleaf")
	w.SetBlock(-5, 180, 3, withProp(small, "half", "lower"))
	w.SetBlock(-5, 181, 3, withProp(small, "half", "upper"))
	if !apply(-5, 181, 3) {
		t.Error("small dripleaf refused")
	}
	tall := 0
	for y := 180; inRange(w.At(-5, y, 3), dripleafStemRng) || inRange(w.At(-5, y, 3), bigDripleafRange); y++ {
		tall++
	}
	if tall < 2 || tall > 5 || !inRange(w.At(-5, 180+tall-1, 3), bigDripleafRange) {
		t.Errorf("small dripleaf grew %d tall", tall)
	}
	// Rooted dirt sprouts hanging roots below.
	w.SetBlock(0, 176, 5, rootedDirtState)
	if !apply(0, 176, 5) || w.At(0, 175, 5) != worldgen.BlockID("hanging_roots") {
		t.Error("rooted dirt grew no roots")
	}
	// Bamboo: one or two segments.
	w.SetBlock(3, 179, -5, worldgen.Dirt)
	w.SetBlock(3, 180, -5, bambooState(0, bambooLeavesNone, 0))
	if !apply(3, 180, -5) || !isBamboo(w.At(3, 181, -5)) {
		t.Error("bamboo did not grow")
	}
	// A hanging propagule ripens by one.
	w.SetBlock(-3, 178, 5, propaguleState(1, true, 0, false))
	if !apply(-3, 178, 5) || propaguleAge(w.At(-3, 178, 5)) != 2 {
		t.Error("hanging propagule did not ripen")
	}
	// The torchflower blooms; the pitcher crop ages.
	w.SetBlock(-1, 179, -1, farmlandMin)
	w.SetBlock(-1, 180, -1, torchflowerCropMin)
	if !apply(-1, 180, -1) || w.At(-1, 180, -1) != worldgen.BlockID("torchflower") {
		t.Error("torchflower did not bloom")
	}
	w.SetBlock(1, 179, -1, farmlandMin)
	w.SetBlock(1, 180, -1, pitcherLower(0))
	if !apply(1, 180, -1) || w.At(1, 180, -1) != pitcherLower(1) {
		t.Error("pitcher crop did not age")
	}
}

// Bone meal on water beside a solid face seeds seagrass on the sea floor.
func TestBoneMealOnWaterSeedsSeagrass(t *testing.T) {
	h, players := bonemealWorld(t)
	w := h.world
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			for y := 180; y <= 184; y++ {
				w.SetBlock(x, y, z, worldgen.Water)
			}
		}
	}
	if !h.bonemealWater(players, 0, 0, 180, 0, 0, 1, 0) {
		t.Fatal("water bone meal refused")
	}
	grass := 0
	for x := -6; x <= 6; x++ {
		for z := -6; z <= 6; z++ {
			if w.At(x, 180, z) == seagrassState || prop(w.At(x, 180, z), "half") == "lower" {
				grass++
			}
		}
	}
	if grass == 0 {
		t.Error("no seagrass grew")
	}
	if h.bonemealWater(players, 0, 0, 179, 0, 0, 1, 0) {
		t.Error("bone meal grew on stone")
	}
}
