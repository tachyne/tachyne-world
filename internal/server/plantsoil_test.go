package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// plantEditBeside is a player setting stone beside a cell, as the hub's
// evBlock case runs it: onBlock, then the support sweep around the edit.
func plantEditBeside(h *hub, players map[int32]*tracked, at blockPos, state uint32) {
	h.world.SetBlock(at.x, at.y, at.z, state)
	h.onBlock(players, evBlock{dim: dimOverworld, x: at.x, y: at.y, z: at.z, state: state, placed: true})
	h.dropUnsupported(players, dimOverworld, at)
}

// Each plant's own mayPlaceOn, not one shared soil list: set on the wrong
// ground, a flower is refused and goes at the next neighbour change; the
// plant whose tag names that ground stays.
func TestPlantsRootInTheirOwnGround(t *testing.T) {
	cases := []struct {
		name         string
		plant, soil  uint32
		wantSurvives bool
	}{
		{"poppy on grass", worldgen.BlockBase("poppy"), worldgen.GrassBlock, true},
		{"poppy on sand", worldgen.BlockBase("poppy"), worldgen.Sand, false},
		{"poppy on netherrack", worldgen.BlockBase("poppy"), worldgen.Netherrack, false},
		{"dead bush on sand", worldgen.BlockBase("dead_bush"), worldgen.Sand, true},
		{"dead bush on terracotta", worldgen.BlockBase("dead_bush"), worldgen.BlockBase("terracotta"), true},
		{"wither rose on netherrack", worldgen.BlockBase("wither_rose"), worldgen.Netherrack, true},
		{"nether wart on soul sand", worldgen.BlockBase("nether_wart"), worldgen.SoulSand, true},
		{"nether wart on grass", worldgen.BlockBase("nether_wart"), worldgen.GrassBlock, false},
		{"crimson roots on soul soil", worldgen.BlockBase("crimson_roots"), worldgen.SoulSoil, true},
		{"crimson roots on sand", worldgen.BlockBase("crimson_roots"), worldgen.Sand, false},
		{"azalea on clay", worldgen.BlockBase("azalea"), worldgen.Clay, true},
		{"oak sapling on clay", worldgen.BlockBase("oak_sapling"), worldgen.Clay, false},
		{"seagrass on stone", worldgen.BlockBase("seagrass"), worldgen.Stone, true},
		{"seagrass on magma", worldgen.BlockBase("seagrass"), magmaBlockState, false},
		{"melon stem on farmland", worldgen.BlockBase("melon_stem"), worldgen.BlockBase("farmland"), true},
		{"melon stem on grass", worldgen.BlockBase("melon_stem"), worldgen.GrassBlock, false},
	}
	for _, c := range cases {
		h := newTestHub(world.New(1))
		players := map[int32]*tracked{}
		w := h.world
		pos := blockPos{40, 180, 40}
		w.ForceLoad(pos.x, pos.z, 1)
		clearAirBox(w, pos.x, pos.y, pos.z, 2)
		w.SetBlock(pos.x, pos.y-1, pos.z, c.soil)
		if got := canPlaceAt(w, pos, c.plant); got != c.wantSurvives {
			t.Errorf("%s: canPlaceAt = %v, want %v", c.name, got, c.wantSurvives)
		}
		w.SetBlock(pos.x, pos.y, pos.z, c.plant)
		plantEditBeside(h, players, blockPos{pos.x + 1, pos.y, pos.z}, worldgen.Stone)
		if stays := w.At(pos.x, pos.y, pos.z) == c.plant; stays != c.wantSurvives {
			t.Errorf("%s: after a neighbour edit the plant stayed = %v, want %v", c.name, stays, c.wantSurvives)
		}
	}
}

// SmallDripleafBlock.mayPlaceOn: clay or moss always; ordinary ground only
// for a dripleaf standing in a water source (waterlogged).
func TestSmallDripleafGround(t *testing.T) {
	w := world.New(1)
	pos := blockPos{0, 180, 0}
	lower := withProps(t, worldgen.BlockBase("small_dripleaf"), map[string]string{"half": "lower", "waterlogged": "false"})
	wet := withProps(t, lower, map[string]string{"waterlogged": "true"})
	if !plantMayPlaceOn(w, pos, lower, worldgen.Clay) || !plantMayPlaceOn(w, pos, lower, worldgen.MossBlock) {
		t.Error("a small dripleaf should root in clay and moss")
	}
	if plantMayPlaceOn(w, pos, lower, worldgen.GrassBlock) {
		t.Error("a dry small dripleaf should not root in grass")
	}
	if !plantMayPlaceOn(w, pos, wet, worldgen.GrassBlock) {
		t.Error("a small dripleaf in a water source roots in grass")
	}
}

// MangrovePropaguleBlock: a hanging propagule holds to mangrove leaves above
// it; a planted one's ground is looked at only when the block ABOVE it
// changes (its updateShape answers the UP side alone).
func TestMangrovePropaguleSupport(t *testing.T) {
	h := newTestHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.world
	pos := blockPos{70, 180, 70}
	w.ForceLoad(pos.x, pos.z, 1)
	clearAirBox(w, pos.x, pos.y, pos.z, 2)

	hanging := propaguleState(4, true, 0, false)
	w.SetBlock(pos.x, pos.y+1, pos.z, worldgen.BlockBase("mangrove_leaves"))
	w.SetBlock(pos.x, pos.y, pos.z, hanging)
	plantEditBeside(h, players, blockPos{pos.x + 1, pos.y, pos.z}, worldgen.Stone)
	if w.At(pos.x, pos.y, pos.z) != hanging {
		t.Fatal("a propagule hanging from mangrove leaves fell at a neighbour edit")
	}

	planted := propaguleState(4, false, 0, false)
	clearAirBox(w, pos.x, pos.y, pos.z, 2)
	w.SetBlock(pos.x, pos.y-1, pos.z, worldgen.BlockBase("mud"))
	w.SetBlock(pos.x, pos.y, pos.z, planted)
	plantEditBeside(h, players, blockPos{pos.x, pos.y - 1, pos.z}, worldgen.Air) // its ground dug out
	if w.At(pos.x, pos.y, pos.z) != planted {
		t.Fatal("a planted propagule went when its ground did; vanilla's waits for a change above it")
	}
	plantEditBeside(h, players, blockPos{pos.x, pos.y + 1, pos.z}, worldgen.Stone)
	if w.At(pos.x, pos.y, pos.z) == planted {
		t.Fatal("a propagule with no ground stayed after the block above it changed")
	}
}

// oldPlantSoils is the single soil list every plant used to share. A plant
// the generator puts on one of these grounds must still stand under its own
// rule: tightening the rule may not pop generated vegetation.
var oldPlantSoils = []string{
	"dirt", "grass_block", "podzol", "coarse_dirt", "rooted_dirt", "mycelium",
	"moss_block", "pale_moss_block", "mud", "muddy_mangrove_roots", "farmland",
	"sand", "red_sand", "suspicious_sand", "soul_sand", "soul_soil",
	"crimson_nylium", "warped_nylium", "clay", "gravel", "terracotta",
	"snow_block", "powder_snow", "end_stone", "netherrack",
}

// generatedPlants walks the plants the generator grew in a patch of w
// (lower halves and single plants that root in soil; the column plants
// keep their own rules) and calls fn with each and the ground under it.
func generatedPlants(w *world.World, x0, z0, size, yLo, yHi int, fn func(pos blockPos, st, ground uint32)) {
	for x := x0; x < x0+size; x++ {
		for z := z0; z < z0+size; z++ {
			for y := yLo; y <= yHi; y++ {
				st := w.At(x, y, z)
				if worldgen.SupportFor(st) != worldgen.SupportSoil {
					continue
				}
				if inStates(st, caneStates) || inStates(st, cactusStates) || inStates(st, bambooStates) ||
					inStates(st, bambooSapStates) || st == cactusFlowerState {
					continue
				}
				if info, ok := worldgen.InfoForState(st); ok && info.HasProperty("half") && worldgen.GetProperty(info, st, "half") == "upper" {
					continue
				}
				fn(blockPos{x, y, z}, st, w.At(x, y-1, z))
			}
		}
	}
}

// Every plant the generator grows on ground the old shared list accepted
// still stands under its own mayPlaceOn — in the overworld's biomes and in
// the Nether's forests.
func TestGeneratedPlantsKeepTheirGround(t *testing.T) {
	old := map[uint32]bool{}
	for _, n := range oldPlantSoils {
		if lo, hi, ok := worldgen.BlockRangeOK(n); ok {
			for s := lo; s <= hi; s++ {
				old[s] = true
			}
		}
	}
	found := 0
	check := func(w *world.World, what string) func(pos blockPos, st, ground uint32) {
		return func(pos blockPos, st, ground uint32) {
			found++
			if old[ground] && !plantMayPlaceOn(w, pos, st, ground) {
				t.Errorf("%s: generated %s at %v on %s no longer stands", what, describeState(st), pos, describeState(ground))
			}
		}
	}
	ow := world.New(1)
	for _, o := range [][2]int{{0, 0}, {1600, -1200}, {-2400, 2000}, {4000, 4000}, {-5200, -800}} {
		top := int(ow.SurfaceY(o[0]+16, o[1]+16))
		generatedPlants(ow, o[0], o[1], 48, top-24, top+12, check(ow, "overworld"))
	}
	nw, _ := world.NewNether(1, nil)
	for _, o := range [][2]int{{0, 0}, {300, -200}, {-500, 400}} {
		generatedPlants(nw, o[0], o[1], 32, 30, 110, check(nw, "nether"))
	}
	if found == 0 {
		t.Fatal("the scan found no generated plants at all; it proves nothing")
	}
	t.Logf("%d generated plants checked", found)
}

// A plant the generator grew on its natural ground stays when a player
// builds right beside it.
func TestGeneratedPlantSurvivesANeighbourEdit(t *testing.T) {
	w := world.New(1)
	h := newTestHub(w)
	players := map[int32]*tracked{}
	var plant blockPos
	var state uint32
	for _, o := range [][2]int{{0, 0}, {1600, -1200}, {-2400, 2000}, {4000, 4000}, {-5200, -800}} {
		top := int(w.SurfaceY(o[0]+16, o[1]+16))
		generatedPlants(w, o[0], o[1], 48, top-24, top+12, func(pos blockPos, st, ground uint32) {
			if state != 0 || !inRanges2(ground, supportsVegetation) {
				return
			}
			if info, ok := worldgen.InfoForState(st); ok && info.HasProperty("half") {
				return // a single plant, so the edit beside it is only beside it
			}
			if w.At(pos.x+1, pos.y, pos.z) != worldgen.Air {
				return
			}
			plant, state = pos, st
		})
		if state != 0 {
			break
		}
	}
	if state == 0 {
		t.Skip("no single generated plant on vegetation ground with air beside it in the sampled patches")
	}
	w.ForceLoad(plant.x, plant.z, 1)
	plantEditBeside(h, players, blockPos{plant.x + 1, plant.y, plant.z}, worldgen.Stone)
	if got := w.At(plant.x, plant.y, plant.z); got != state {
		t.Fatalf("the generated %s at %v went at a neighbour edit (now %s)", describeState(state), plant, describeState(got))
	}
}
