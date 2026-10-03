package worldgen

import "sort"

// Vanilla's biome decoration (ChunkGenerator.applyBiomeDecoration): a
// chunk's decoration seed from the world seed and its corner, then the
// eleven steps in order; in each, every placed feature some biome of the
// chunk's 3x3 neighbourhood lists, in the dimension's global feature
// order, on its own stream (setFeatureSeed with its global index and the
// step) — its placement modifiers turning the chunk's corner into the
// positions the feature is placed at. The structures of a step come first
// in it, each with a stream of its own (their pieces are the engine's
// structure code; vanillastructs.go places their starts).

// vanillaDecor is one dimension's decoration plan.
type vanillaDecor struct {
	seed     int64
	d        *vpData
	possible map[string]bool
	// steps is FeatureSorter's per-step order over the dimension's
	// possible biomes; index maps a placed feature to its place there.
	steps [][]*vpPlacedFeature
	index []map[string]int
	// has is BiomeGenerationSettings.hasFeature: every placed feature a
	// biome lists, in any step.
	has map[string]map[string]bool
	// structSteps is the structure registry grouped by step, in registry
	// order: a structure's index in its step salts its stream.
	structSteps [vpStepCount][]string
	minY        int
	height      int
	seaLevel    int
}

// newVanillaDecor builds the plan for a dimension ("overworld", "nether",
// "end").
func newVanillaDecor(seed int64, dim string) (*vanillaDecor, error) {
	d := mustVPData()
	steps, err := vpStepFeatures(d.Possible[dim], d.Biomes)
	if err != nil {
		return nil, err
	}
	v := &vanillaDecor{seed: seed, d: d, possible: map[string]bool{}, has: map[string]map[string]bool{},
		minY: MinY, height: SectionCount * 16, seaLevel: SeaLevel}
	switch dim {
	case "nether":
		v.minY, v.height, v.seaLevel = 0, 128, 32
	case "end":
		v.minY, v.height, v.seaLevel = 0, 128, 0
	}
	for _, b := range d.Possible[dim] {
		v.possible[b] = true
	}
	for b, ss := range d.Biomes {
		m := map[string]bool{}
		for _, s := range ss {
			for _, f := range s {
				m[f] = true
			}
		}
		v.has[b] = m
	}
	for _, s := range steps {
		idx := map[string]int{}
		list := make([]*vpPlacedFeature, len(s))
		for i, name := range s {
			idx[name] = i
			list[i] = d.Placed[name]
		}
		v.steps = append(v.steps, list)
		v.index = append(v.index, idx)
	}
	for _, name := range d.StructNames {
		st := d.Structures[name]
		v.structSteps[st.Step] = append(v.structSteps[st.Step], name)
	}
	return v, nil
}

// vpPlacement is one feature placement a chunk's decoration makes: the
// step, the feature's global index in it, the placed feature, the
// position, and the stream the feature itself draws from (positioned just
// after the modifiers drew theirs).
type vpPlacement struct {
	step, index int
	placed      *vpPlacedFeature
	pos         vpPos
	rng         *vwRandom
}

// decorate runs chunk (cx, cz)'s decoration over lv: biomes are the
// biomes of the chunk's 3x3 neighbourhood (the ones its sections hold);
// place is called for every position a feature is placed at, in vanilla's
// order, with the shared stream — a feature that draws must draw from it,
// as the next placement of the same feature continues the stream.
// structure, when set, is called at the head of each step with the step's
// structures and their streams.
func (v *vanillaDecor) decorate(cx, cz int32, lv vpLevel, biomes []string, place func(p *vpPlacement),
	structure func(step, index int, name string, r *vwRandom)) {
	v.decorateSteps(cx, cz, lv, biomes, 0, vpStepCount-1, nil, place, structure)
}

// decorateTrace is decorate with a hook at the head of every placed
// feature the chunk tries (the oracle tests print one line per feature).
func (v *vanillaDecor) decorateTrace(cx, cz int32, lv vpLevel, biomes []string,
	each func(step, index int, pf *vpPlacedFeature, r *vwRandom), place func(p *vpPlacement)) {
	v.decorateSteps(cx, cz, lv, biomes, 0, vpStepCount-1, each, place, nil)
}

// decorateSteps runs steps lo..hi of a chunk's decoration (each step's
// streams are seeded afresh, so a step can run on its own).
func (v *vanillaDecor) decorateSteps(cx, cz int32, lv vpLevel, biomes []string, lo, hi int,
	each func(step, index int, pf *vpPlacedFeature, r *vwRandom), place func(p *vpPlacement),
	structure func(step, index int, name string, r *vwRandom)) {
	r := newVWXoroshiro(0)
	minX, minZ := int(cx)<<4, int(cz)<<4
	ds := r.setDecorationSeed(v.seed, int32(minX), int32(minZ))
	origin := vpPos{minX, v.minY, minZ}
	ctx := &vpCtx{lv: lv, minY: v.minY, height: v.height, seaLevel: v.seaLevel,
		hasFeature: func(b, f string) bool { return v.has[b][f] }}
	n := max(vpStepCount, len(v.steps))
	for step := max(lo, 0); step < n && step <= hi; step++ {
		if structure != nil && step < vpStepCount {
			for i, name := range v.structSteps[step] {
				r.setFeatureSeed(ds, i, step)
				structure(step, i, name, r)
			}
		}
		if step >= len(v.steps) {
			continue
		}
		seen := map[int]bool{}
		var idx []int
		for _, b := range biomes {
			if !v.possible[b] {
				continue
			}
			bs := v.d.Biomes[b]
			if step >= len(bs) {
				continue
			}
			for _, f := range bs[step] {
				if i, ok := v.index[step][f]; ok && !seen[i] {
					seen[i] = true
					idx = append(idx, i)
				}
			}
		}
		sort.Ints(idx)
		for _, gi := range idx {
			pf := v.steps[step][gi]
			if each != nil {
				each(step, gi, pf, r) // before the reseed: the trace reads the last stream's tail
			}
			r.setFeatureSeed(ds, gi, step)
			ctx.top = pf
			vpPlace(ctx, pf, r, origin, func(p vpPos) {
				place(&vpPlacement{step: step, index: gi, placed: pf, pos: p, rng: r})
			})
		}
	}
}

// vpPlace is FeaturePlacer.place: the modifiers run depth first, each
// output position taken through the rest of the list before the next, and
// the feature placed at every position that comes out of the last.
func vpPlace(c *vpCtx, pf *vpPlacedFeature, r *vwRandom, origin vpPos, feature func(vpPos)) {
	if len(pf.Modifiers) == 0 {
		feature(origin)
		return
	}
	type item struct {
		p vpPos
		m int
	}
	stack := []item{{origin, 0}}
	var mod []vpPos
	for len(stack) > 0 {
		it := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		mod = mod[:0]
		pf.Modifiers[it.m].modify(c, r, it.p, func(q vpPos) { mod = append(mod, q) })
		if next := it.m + 1; next < len(pf.Modifiers) {
			for i := len(mod) - 1; i >= 0; i-- {
				stack = append(stack, item{mod[i], next})
			}
			continue
		}
		for _, q := range mod {
			feature(q)
		}
	}
}
