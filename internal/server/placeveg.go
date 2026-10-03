package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// /place feature for 26.3's vegetation features (VegetationFeatures). In
// 26.3 a patch is a placed feature — the count, the spread and the
// survival filter are its placement — and the configured feature /place
// names is the single plant: a SimpleBlockFeature (the provider's state,
// placed when it can survive there; a double plant only with room above)
// or a BlockColumnFeature (sugar cane, cactus: a column whose height is
// cut where the cells above stop being air). Both write with
// UPDATE_CLIENTS, as the rest of /place does.

// vegWeight is one entry of a WeightedStateProvider.
type vegWeight struct {
	state  uint32
	weight int
}

// vegSimple are the SimpleBlockFeature vegetation features by id: the
// provider's states and weights.
var vegSimple = map[string][]vegWeight{
	"grass":              {{worldgen.BlockBase("short_grass"), 1}},
	"taiga_grass":        {{worldgen.BlockBase("short_grass"), 1}, {worldgen.BlockBase("fern"), 4}},
	"grass_jungle":       {{worldgen.BlockBase("short_grass"), 3}, {worldgen.BlockBase("fern"), 1}},
	"dead_bush":          {{worldgen.BlockBase("dead_bush"), 1}},
	"dry_grass":          {{worldgen.BlockBase("short_dry_grass"), 1}, {worldgen.BlockBase("tall_dry_grass"), 1}},
	"melon":              {{worldgen.BlockBase("melon"), 1}},
	"waterlily":          {{worldgen.BlockBase("lily_pad"), 1}},
	"tall_grass":         {{vegLower("tall_grass"), 1}},
	"large_fern":         {{vegLower("large_fern"), 1}},
	"sunflower":          {{vegLower("sunflower"), 1}},
	"bush":               {{worldgen.BlockBase("bush"), 1}},
	"red_shrub":          {{worldgen.BlockBase("red_shrub"), 1}},
	"firefly_bush":       {{worldgen.BlockBase("firefly_bush"), 1}},
	"brown_mushroom":     {{worldgen.BlockBase("brown_mushroom"), 1}},
	"red_mushroom":       {{worldgen.BlockBase("red_mushroom"), 1}},
	"pumpkin":            {{worldgen.BlockBase("pumpkin"), 1}},
	"berry_bush":         {{vegProps("sweet_berry_bush", map[string]string{"age": "3"}), 1}},
	"flower_default":     {{worldgen.BlockBase("poppy"), 2}, {worldgen.BlockBase("dandelion"), 1}},
	"flower_swamp":       {{worldgen.BlockBase("blue_orchid"), 1}},
	"flower_pale_garden": {{worldgen.BlockBase("closed_eyeblossom"), 1}},
	"leaf_litter":        vegSegmented("leaf_litter", 1, 3),
	"flower_cherry":      vegSegmented("pink_petals", 1, 4),
	"wildflower":         vegSegmented("wildflowers", 1, 4),
}

// vegColumns are the BlockColumnFeature vegetation features.
var vegColumns = map[string]bool{"sugar_cane": true, "cactus": true}

// vegetationFeature reports whether /place grows this feature here.
func vegetationFeature(name string) bool {
	_, simple := vegSimple[name]
	return simple || vegColumns[name]
}

// vegProps is a block's base state with properties set.
func vegProps(name string, props map[string]string) uint32 {
	s := worldgen.BlockBase(name)
	info, ok := worldgen.InfoForState(s)
	if !ok {
		return s
	}
	for k, v := range props {
		s = worldgen.SetProperty(info, s, k, v)
	}
	return s
}

// vegLower is a double plant's lower half.
func vegLower(name string) uint32 { return vegProps(name, map[string]string{"half": "lower"}) }

// vegSegmented is segmentedBlockPatchBuilder: every amount from lo to hi in
// each horizontal facing (north, east, south, west), one weight each.
func vegSegmented(name string, lo, hi int) []vegWeight {
	var out []vegWeight
	for n := lo; n <= hi; n++ {
		for _, f := range []string{"north", "east", "south", "west"} {
			amount := "flower_amount"
			if name == "leaf_litter" {
				amount = "segment_amount"
			}
			out = append(out, vegWeight{vegProps(name, map[string]string{amount: itoa(n), "facing": f}), 1})
		}
	}
	return out
}

// pickVeg is WeightedList.getRandom: one draw over the total weight.
func (h *hub) pickVeg(ws []vegWeight) uint32 {
	total := 0
	for _, w := range ws {
		total += w.weight
	}
	r := h.rng.Intn(total)
	for _, w := range ws {
		if r < w.weight {
			return w.state
		}
		r -= w.weight
	}
	return ws[len(ws)-1].state
}

// placeVegetation grows one vegetation feature at pos; false when it
// placed nothing (Feature.place's result).
func (h *hub) placeVegetation(players map[int32]*tracked, dim int, pos blockPos, name string) bool {
	w := h.worldFor(dim)
	if ws, ok := vegSimple[name]; ok { // SimpleBlockFeature.place
		state := h.pickVeg(ws)
		info, _ := worldgen.InfoForState(state)
		double := worldgen.GetProperty(info, state, "half") == "lower"
		if double {
			// DoublePlantBlock.canSurvive for the lower half: the plant's own
			// ground (its upper half is placed with it).
			if !plantMayPlaceOn(w, pos, state, w.At(pos.x, pos.y-1, pos.z)) {
				return false
			}
		} else if !canPlaceAt(w, pos, state) {
			return false
		}
		if double { // DoublePlantBlock.placeAt
			above := w.At(pos.x, pos.y+1, pos.z)
			if above != worldgen.Air && !(worldgen.IsWater(above) && worldgen.IsWater(w.At(pos.x, pos.y, pos.z))) {
				return false
			}
			upper := worldgen.SetProperty(info, state, "half", "upper")
			h.placeEdits(players, dim, []worldgen.TemplateCell{{X: pos.x, Y: pos.y, Z: pos.z, State: state}, {X: pos.x, Y: pos.y + 1, Z: pos.z, State: upper}})
			return true
		}
		h.placeEdits(players, dim, []worldgen.TemplateCell{{X: pos.x, Y: pos.y, Z: pos.z, State: state}})
		return true
	}
	// BlockColumnFeature.place, up, only into air.
	type layer struct {
		height int
		state  uint32
	}
	var layers []layer
	switch name {
	case "sugar_cane": // simple(BiasedToBottomInt(2, 4), sugar_cane)
		layers = []layer{{2 + h.rng.Intn(h.rng.Intn(3)+1), worldgen.BlockBase("sugar_cane")}}
	case "cactus": // BiasedToBottomInt(1, 3) cactus, then a flower one time in four
		n := 1 + h.rng.Intn(h.rng.Intn(3)+1)
		f := 0
		if h.rng.Intn(4) >= 3 { // WeightedListInt: 0 ×3, 1 ×1
			f = 1
		}
		layers = []layer{{n, worldgen.BlockBase("cactus")}, {f, worldgen.BlockBase("cactus_flower")}}
	default:
		return false
	}
	total := 0
	for _, l := range layers {
		total += l.height
	}
	if total == 0 {
		return false
	}
	for y := 0; y < total; y++ { // the cell past the y-th must be air, or the column ends at y
		if w.At(pos.x, pos.y+y+1, pos.z) != worldgen.Air {
			cut := total - y // truncate, not prioritising the tip: from the last layer down
			for i := len(layers) - 1; i >= 0 && cut > 0; i-- {
				d := min(layers[i].height, cut)
				layers[i].height -= d
				cut -= d
			}
			break
		}
	}
	var cells []worldgen.TemplateCell
	y := pos.y
	for _, l := range layers {
		for i := 0; i < l.height; i++ {
			cells = append(cells, worldgen.TemplateCell{X: pos.x, Y: y, Z: pos.z, State: l.state})
			y++
		}
	}
	h.placeEdits(players, dim, cells)
	return true
}
