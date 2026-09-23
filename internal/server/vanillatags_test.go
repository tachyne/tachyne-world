package server

import (
	"strings"
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The piston reactions are the game's own: the hand lists they replaced
// pushed leaves, buttons, beds, candles and pumpkins, which vanilla pops.
func TestPushReactionsFromTheGame(t *testing.T) {
	for name, want := range map[string]pushReaction{
		"stone": pushNormal, "oak_planks": pushNormal,
		"oak_leaves": pushDestroy, "red_poplar_leaves": pushDestroy, "stone_button": pushDestroy,
		"pumpkin": pushDestroy, "white_bed": pushDestroy, "candle": pushDestroy, "poplar_door": pushDestroy,
		"obsidian": pushBlock, "reinforced_deepslate": pushBlock,
		"white_glazed_terracotta": pushOnly,
	} {
		if got := pushReactionOf(worldgen.BlockBase(name)); got != want {
			t.Errorf("%s: push reaction %d, want %d", name, got, want)
		}
	}
	for name, want := range map[string]bool{"chest": true, "poplar_sign": true, "poplar_shelf": true, "stone": false, "white_wool_slab": false} {
		if got := hasBlockEntity(worldgen.BlockBase(name)); got != want {
			t.Errorf("%s: block entity %v, want %v", name, got, want)
		}
	}
}

// The families that used to be hand-kept name lists are vanilla's tags, so a
// new wood set (poplar, in 26.3) is in them without anyone adding it.
func TestTagFamiliesTakeNewWood(t *testing.T) {
	b := worldgen.BlockBase
	for name, ok := range map[string]bool{
		"button":          isButton(b("poplar_button")),
		"pressure plate":  isPlate(b("poplar_pressure_plate")),
		"fence":           isFence(b("poplar_fence")),
		"shelf":           isWoodShelf(b("poplar_shelf")),
		"leaf":            isAnyLeaf(b("yellow_poplar_leaves")),
		"azalea leaf":     isAnyLeaf(b("azalea_leaves")),
		"trunk":           isTrunkBlock(b("stripped_poplar_wood")),
		"not a fence":     !isFence(b("poplar_fence_gate")),
		"not a leaf":      !isAnyLeaf(b("poplar_planks")),
		"shield mends":    repairMaterials[int32(itemByName["shield"])][int32(itemByName["poplar_planks"])],
		"wood tool mends": repairMaterials[int32(itemByName["wooden_axe"])][int32(itemByName["poplar_planks"])],
	} {
		if !ok {
			t.Errorf("%s: poplar not recognised", name)
		}
	}
}

func TestUnknownTagPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("an ungenerated tag must panic")
		}
	}()
	worldgen.BlockTag("no_such_tag")
}

// Fire's odds are the game's own, per state: wool stairs and slabs burn like
// wool, poplar like wood, and a waterlogged state never burns.
func TestFlammabilityFromTheGame(t *testing.T) {
	for name, want := range map[string][2]uint32{
		"white_wool_slab": {30, 60}, "red_wool_stairs": {30, 60}, "poplar_planks": {5, 20},
		"poplar_log": {5, 5}, "yellow_poplar_leaves": {30, 60}, "oak_shelf": {30, 20},
		"white_concrete_slab": {0, 0}, "cinnabar_bricks": {0, 0},
	} {
		st := worldgen.BlockBase(name)
		if info, ok := worldgen.InfoForState(st); ok && info.HasProperty("waterlogged") {
			st = worldgen.SetProperty(info, st, "waterlogged", "false")
		}
		if ig, bu := worldgen.Flammability(st); [2]uint32{ig, bu} != want {
			t.Errorf("%s: odds %d/%d, want %v", name, ig, bu, want)
		}
	}
	wet := worldgen.BlockBase("poplar_slab")
	info, _ := worldgen.InfoForState(wet)
	wet = worldgen.SetProperty(info, wet, "waterlogged", "true")
	if ig, bu := worldgen.Flammability(wet); ig != 0 || bu != 0 {
		t.Errorf("a waterlogged poplar slab burns: %d/%d", ig, bu)
	}
}

// phase2Blocks are 26.3's derivative blocks: the wool and concrete stairs and
// slabs, the cinnabar family and the poplar wood set.
func phase2Blocks() []string {
	var out []string
	for _, c := range []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
		"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"} {
		out = append(out, c+"_wool_slab", c+"_wool_stairs", c+"_concrete_slab", c+"_concrete_stairs")
	}
	out = append(out, "cinnabar", "cinnabar_slab", "cinnabar_stairs", "cinnabar_wall", "chiseled_cinnabar",
		"polished_cinnabar", "polished_cinnabar_slab", "polished_cinnabar_stairs", "polished_cinnabar_wall",
		"cinnabar_bricks", "cinnabar_brick_slab", "cinnabar_brick_stairs", "cinnabar_brick_wall")
	for _, p := range []string{"log", "wood", "planks", "slab", "stairs", "fence", "fence_gate", "door", "trapdoor",
		"button", "pressure_plate", "sign", "wall_sign", "hanging_sign", "wall_hanging_sign", "shelf", "sapling"} {
		out = append(out, "poplar_"+p)
	}
	return append(out, "stripped_poplar_log", "stripped_poplar_wood",
		"red_poplar_leaves", "orange_poplar_leaves", "yellow_poplar_leaves")
}

// Every Phase 2 block is in the engine end to end: it breaks, drops by its
// loot table, its item places it, and a recipe makes it where vanilla has one.
func TestPhase2BlocksEndToEnd(t *testing.T) {
	made := map[int32]bool{}
	for _, r := range shapedRecipes {
		made[r.Result] = true
	}
	for _, r := range shapelessRecipes {
		made[r.Result] = true
	}
	for _, r := range protocol.StonecuttingRecipes {
		made[r.Out] = true
	}
	uncrafted := map[string]bool{"poplar_log": true, "poplar_sapling": true, "stripped_poplar_log": true,
		"stripped_poplar_wood": true, "red_poplar_leaves": true, "orange_poplar_leaves": true,
		"yellow_poplar_leaves": true, "cinnabar": true}
	names := phase2Blocks()
	if len(names) != 64+13+22 {
		t.Fatalf("%d phase 2 blocks, want %d", len(names), 64+13+22)
	}
	for _, name := range names {
		lo, hi, ok := worldgen.BlockRangeOK(name)
		if !ok {
			t.Errorf("%s: no such block", name)
			continue
		}
		if h := worldgen.Hardness(lo); h < 0 {
			t.Errorf("%s: unbreakable (hardness %v)", name, h)
		}
		looted := false
		for _, r := range blockLoot {
			if r.Lo <= lo && hi <= r.Hi {
				looted = true
			}
		}
		if !looted {
			t.Errorf("%s: no loot table", name)
		}
		item := strings.Replace(name, "wall_", "", 1)
		id, ok := itemByName[item]
		if !ok {
			t.Errorf("%s: no item %s", name, item)
			continue
		}
		if st, ok := protocol.BlockForItem(int32(id)); !ok || (item == name && (st < lo || st > hi)) {
			t.Errorf("%s: its item places state %d (ok %v), not in %d..%d", name, st, ok, lo, hi)
		}
		if !uncrafted[item] && !made[int32(id)] {
			t.Errorf("%s: no recipe makes it", name)
		}
	}
}
