package worldgen

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
	"sort"
	"sync"
)

// The vanilla biome source (GeneratorMode vanilla): 26.3's
// MultiNoiseBiomeSource over the overworld and Nether parameter lists, and
// TheEndBiomeSource, behind VanillaBiomes.
//
// A multi-noise biome is the parameter-list entry nearest the quart's
// climate (Climate.ParameterList.findValue): seven dimensions — the six
// climate values and the entry's offset — each the distance from the
// target to the entry's interval, squared and summed. The search is
// vanilla's R-tree, built the same way from the list in the same order,
// so where two entries tie it returns the one vanilla returns. Vanilla
// also seeds each search with the previous result on that thread, which
// wins any tie; a chunk's biomes are resolved in vanilla's fill order
// (ChunkAccess.fillBiomesFromNoise: sections bottom up, then x, y, z)
// carrying that hint from quart to quart, so a chunk's biomes are the
// server's. (The hint the first quart of a chunk starts from is whatever
// chunk that server thread did last — unreproducible, and only able to
// matter on an exact tie.)
//
// The overworld list is OverworldBiomeBuilder's, reimplemented here entry
// for entry and checked against the server's list (count and hash). The
// client-visible biome of a block is BiomeManager's fuzzed zoom over the
// noise biomes (VanillaBiomeZoom).

// vbParam is Climate.Parameter: a quantized interval.
type vbParam struct{ min, max int64 }

// vbQ is Climate.quantizeCoord.
func vbQ(v float32) int64 { return int64(v * 10000) }

// vbSpan is Climate.Parameter.span(float, float).
func vbSpan(lo, hi float32) vbParam { return vbParam{vbQ(lo), vbQ(hi)} }

// vbPt is Climate.Parameter.point.
func vbPt(v float32) vbParam { return vbSpan(v, v) }

// vbJoin is Climate.Parameter.span(Parameter, Parameter).
func vbJoin(a, b vbParam) vbParam { return vbParam{a.min, b.max} }

// distance is Climate.Parameter.distance.
func (p vbParam) distance(t int64) int64 {
	if above := t - p.max; above > 0 {
		return above
	}
	if below := p.min - t; below > 0 {
		return below
	}
	return 0
}

// vbEntry is one parameter-list entry: Climate.ParameterPoint's
// parameterSpace (the six intervals and the offset as a point) and its
// biome.
type vbEntry struct {
	space [7]vbParam
	biome string
}

func vbPoint(temp, hum, cont, ero, depth, weird vbParam, offset float32) [7]vbParam {
	o := vbQ(offset)
	return [7]vbParam{temp, hum, cont, ero, depth, weird, {o, o}}
}

// vbNode is a Climate.RTree node: a leaf (entry >= 0) or a subtree.
type vbNode struct {
	space    [7]vbParam
	children []*vbNode
	entry    int
	biome    string
}

// vbTree is Climate.RTree over a parameter list.
type vbTree struct{ root *vbNode }

const vbChildrenPerNode = 19

func newVBTree(list []vbEntry) *vbTree {
	leaves := make([]*vbNode, len(list))
	for i, e := range list {
		leaves[i] = &vbNode{space: e.space, entry: i, biome: e.biome}
	}
	return &vbTree{root: vbBuild(leaves)}
}

// vbSubTree is SubTree(children): the span of the children's spaces.
func vbSubTree(children []*vbNode) *vbNode {
	n := &vbNode{children: children, entry: -1}
	for d := 0; d < 7; d++ {
		p := children[0].space[d]
		for _, c := range children[1:] {
			if c.space[d].min < p.min {
				p.min = c.space[d].min
			}
			if c.space[d].max > p.max {
				p.max = c.space[d].max
			}
		}
		n.space[d] = p
	}
	return n
}

func vbCenter(p vbParam) int64 { return (p.min + p.max) / 2 }

func vbAbs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// vbSort is RTree.sort: a stable sort on dimension d's centre, then each
// following dimension's in turn (absolute centres when abs).
func vbSort(nodes []*vbNode, d int, abs bool) {
	key := func(n *vbNode, dim int) int64 {
		c := vbCenter(n.space[dim])
		if abs {
			return vbAbs(c)
		}
		return c
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		for k := 0; k < 7; k++ {
			dim := (d + k) % 7
			a, b := key(nodes[i], dim), key(nodes[j], dim)
			if a != b {
				return a < b
			}
		}
		return false
	})
}

// vbBucketize is RTree.bucketize.
func vbBucketize(nodes []*vbNode) []*vbNode {
	expected := int(math.Pow(vbChildrenPerNode, math.Floor(math.Log(float64(len(nodes))-0.01)/math.Log(vbChildrenPerNode))))
	var buckets []*vbNode
	var cur []*vbNode
	for _, n := range nodes {
		cur = append(cur, n)
		if len(cur) >= expected {
			buckets = append(buckets, vbSubTree(cur))
			cur = nil
		}
	}
	if len(cur) > 0 {
		buckets = append(buckets, vbSubTree(cur))
	}
	return buckets
}

func vbCost(space [7]vbParam) int64 {
	var c int64
	for _, p := range space {
		c += vbAbs(p.max - p.min)
	}
	return c
}

// vbBuild is RTree.build. It sorts the slice it is given in place, as
// vanilla sorts its list.
func vbBuild(children []*vbNode) *vbNode {
	switch {
	case len(children) == 1:
		return children[0]
	case len(children) <= vbChildrenPerNode:
		mag := func(n *vbNode) int64 {
			var t int64
			for _, p := range n.space {
				t += vbAbs(vbCenter(p))
			}
			return t
		}
		sort.SliceStable(children, func(i, j int) bool { return mag(children[i]) < mag(children[j]) })
		return vbSubTree(children)
	}
	minCost := int64(math.MaxInt64)
	minDim := -1
	var minBuckets []*vbNode
	for d := 0; d < 7; d++ {
		vbSort(children, d, false)
		buckets := vbBucketize(children)
		var total int64
		for _, b := range buckets {
			total += vbCost(b.space)
		}
		if minCost > total {
			minCost, minDim, minBuckets = total, d, buckets
		}
	}
	vbSort(minBuckets, minDim, true)
	out := make([]*vbNode, len(minBuckets))
	for i, b := range minBuckets {
		kids := make([]*vbNode, len(b.children))
		copy(kids, b.children)
		out[i] = vbBuild(kids)
	}
	return vbSubTree(out)
}

// distance is RTree.Node.distance.
func (n *vbNode) distance(t *[7]int64) int64 {
	var d int64
	for i := range n.space {
		v := n.space[i].distance(t[i])
		d += v * v
	}
	return d
}

// search is SubTree.search / Leaf.search.
func (n *vbNode) search(t *[7]int64, cand *vbNode) *vbNode {
	if n.children == nil {
		return n
	}
	minD := int64(math.MaxInt64)
	if cand != nil {
		minD = cand.distance(t)
	}
	closest := cand
	for _, c := range n.children {
		cd := c.distance(t)
		if minD > cd {
			leaf := c.search(t, closest)
			ld := cd
			if c != leaf {
				ld = leaf.distance(t)
			}
			if minD > ld {
				minD, closest = ld, leaf
			}
		}
	}
	return closest
}

// find is RTree.search: the nearest entry to p, starting from the hint
// (the previous result, vanilla's per-thread lastResult; nil for none).
// It returns the leaf, to be passed as the next hint.
func (t *vbTree) find(p ClimatePoint, hint *vbNode) *vbNode {
	target := [7]int64{p.Temperature, p.Humidity, p.Continentalness, p.Erosion, p.Depth, p.Weirdness, 0}
	return t.root.search(&target, hint)
}

// ---- the parameter lists ----

var (
	vbOverworldOnce sync.Once
	vbOverworldList []vbEntry
	vbOverworldTree *vbTree
	vbNetherOnce    sync.Once
	vbNetherTree    *vbTree
)

func vbOverworld() *vbTree {
	vbOverworldOnce.Do(func() {
		vbOverworldList = vbOverworldEntries()
		vbOverworldTree = newVBTree(vbOverworldList)
	})
	return vbOverworldTree
}

// vbNetherEntries is the nether preset of MultiNoiseBiomeSourceParameterList.
func vbNetherEntries() []vbEntry {
	z := vbPt(0)
	e := func(temp, hum, offset float32, b string) vbEntry {
		return vbEntry{vbPoint(vbPt(temp), vbPt(hum), z, z, z, z, offset), b}
	}
	return []vbEntry{
		e(0, 0, 0, "minecraft:nether_wastes"),
		e(0, -0.5, 0, "minecraft:soul_sand_valley"),
		e(0.4, 0, 0, "minecraft:crimson_forest"),
		e(0, 0.5, 0.375, "minecraft:warped_forest"),
		e(-0.5, 0, 0.175, "minecraft:basalt_deltas"),
	}
}

func vbNether() *vbTree {
	vbNetherOnce.Do(func() { vbNetherTree = newVBTree(vbNetherEntries()) })
	return vbNetherTree
}

// vbBuilder is OverworldBiomeBuilder.
type vbBuilder struct {
	out []vbEntry

	full, frozen, unfrozen                       vbParam
	temps, hums, eros                            []vbParam
	mushroom, deepOcean, ocean, coast, inland    vbParam
	nearInland, midInland, farInland             vbParam
	oceans                                       [2][5]string
	middle, middleVar, plateau, plateauVar, shat [5][5]string
}

const (
	bDeepFrozenOcean   = "minecraft:deep_frozen_ocean"
	bDeepColdOcean     = "minecraft:deep_cold_ocean"
	bDeepOcean         = "minecraft:deep_ocean"
	bDeepLukewarmOcean = "minecraft:deep_lukewarm_ocean"
	bWarmOcean         = "minecraft:warm_ocean"
	bFrozenOcean       = "minecraft:frozen_ocean"
	bColdOcean         = "minecraft:cold_ocean"
	bOcean             = "minecraft:ocean"
	bLukewarmOcean     = "minecraft:lukewarm_ocean"
	bSnowyPlains       = "minecraft:snowy_plains"
	bSnowyTaiga        = "minecraft:snowy_taiga"
	bTaiga             = "minecraft:taiga"
	bPlains            = "minecraft:plains"
	bForest            = "minecraft:forest"
	bOldSpruce         = "minecraft:old_growth_spruce_taiga"
	bFlowerForest      = "minecraft:flower_forest"
	bBirchForest       = "minecraft:birch_forest"
	bDarkForest        = "minecraft:dark_forest"
	bSavanna           = "minecraft:savanna"
	bJungle            = "minecraft:jungle"
	bDesert            = "minecraft:desert"
	bIceSpikes         = "minecraft:ice_spikes"
	bDappledForest     = "minecraft:dappled_forest"
	bOldPine           = "minecraft:old_growth_pine_taiga"
	bSunflowerPlains   = "minecraft:sunflower_plains"
	bOldBirch          = "minecraft:old_growth_birch_forest"
	bSparseJungle      = "minecraft:sparse_jungle"
	bBambooJungle      = "minecraft:bamboo_jungle"
	bMeadow            = "minecraft:meadow"
	bPaleGarden        = "minecraft:pale_garden"
	bSavannaPlateau    = "minecraft:savanna_plateau"
	bBadlands          = "minecraft:badlands"
	bWoodedBadlands    = "minecraft:wooded_badlands"
	bCherryGrove       = "minecraft:cherry_grove"
	bErodedBadlands    = "minecraft:eroded_badlands"
	bGravellyHills     = "minecraft:windswept_gravelly_hills"
	bWindsweptHills    = "minecraft:windswept_hills"
	bWindsweptForest   = "minecraft:windswept_forest"
	bWindsweptSavanna  = "minecraft:windswept_savanna"
	bMushroomFields    = "minecraft:mushroom_fields"
	bStonyShore        = "minecraft:stony_shore"
	bSwamp             = "minecraft:swamp"
	bMangroveSwamp     = "minecraft:mangrove_swamp"
	bFrozenRiver       = "minecraft:frozen_river"
	bRiver             = "minecraft:river"
	bSnowyBeach        = "minecraft:snowy_beach"
	bBeach             = "minecraft:beach"
	bJaggedPeaks       = "minecraft:jagged_peaks"
	bFrozenPeaks       = "minecraft:frozen_peaks"
	bStonyPeaks        = "minecraft:stony_peaks"
	bSnowySlopes       = "minecraft:snowy_slopes"
	bGrove             = "minecraft:grove"
	bDripstoneCaves    = "minecraft:dripstone_caves"
	bLushCaves         = "minecraft:lush_caves"
	bSulfurCaves       = "minecraft:sulfur_caves"
	bDeepDark          = "minecraft:deep_dark"
)

func newVBBuilder() *vbBuilder {
	b := &vbBuilder{
		full: vbSpan(-1, 1),
		temps: []vbParam{vbSpan(-1, -0.45), vbSpan(-0.45, -0.15), vbSpan(-0.15, 0.2),
			vbSpan(0.2, 0.55), vbSpan(0.55, 1)},
		hums: []vbParam{vbSpan(-1, -0.35), vbSpan(-0.35, -0.1), vbSpan(-0.1, 0.1),
			vbSpan(0.1, 0.3), vbSpan(0.3, 1)},
		eros: []vbParam{vbSpan(-1, -0.78), vbSpan(-0.78, -0.375), vbSpan(-0.375, -0.2225),
			vbSpan(-0.2225, 0.05), vbSpan(0.05, 0.45), vbSpan(0.45, 0.55), vbSpan(0.55, 1)},
		mushroom:   vbSpan(-1.2, -1.05),
		deepOcean:  vbSpan(-1.05, -0.455),
		ocean:      vbSpan(-0.455, -0.19),
		coast:      vbSpan(-0.19, -0.11),
		inland:     vbSpan(-0.11, 0.55),
		nearInland: vbSpan(-0.11, 0.03),
		midInland:  vbSpan(0.03, 0.3),
		farInland:  vbSpan(0.3, 1),
		oceans: [2][5]string{
			{bDeepFrozenOcean, bDeepColdOcean, bDeepOcean, bDeepLukewarmOcean, bWarmOcean},
			{bFrozenOcean, bColdOcean, bOcean, bLukewarmOcean, bWarmOcean},
		},
		middle: [5][5]string{
			{bSnowyPlains, bSnowyPlains, bSnowyPlains, bSnowyTaiga, bTaiga},
			{bPlains, bPlains, bForest, bTaiga, bOldSpruce},
			{bFlowerForest, bPlains, bForest, bBirchForest, bDarkForest},
			{bSavanna, bSavanna, bForest, bJungle, bJungle},
			{bDesert, bDesert, bDesert, bDesert, bDesert},
		},
		middleVar: [5][5]string{
			{bIceSpikes, "", bSnowyTaiga, "", ""},
			{bDappledForest, "", "", "", bOldPine},
			{bSunflowerPlains, "", "", bOldBirch, ""},
			{"", "", bPlains, bSparseJungle, bBambooJungle},
			{"", "", "", "", ""},
		},
		plateau: [5][5]string{
			{bSnowyPlains, bSnowyPlains, bSnowyPlains, bSnowyTaiga, bSnowyTaiga},
			{bMeadow, bMeadow, bForest, bTaiga, bOldSpruce},
			{bMeadow, bMeadow, bMeadow, bMeadow, bPaleGarden},
			{bSavannaPlateau, bSavannaPlateau, bForest, bForest, bJungle},
			{bBadlands, bBadlands, bBadlands, bWoodedBadlands, bWoodedBadlands},
		},
		plateauVar: [5][5]string{
			{bIceSpikes, "", "", "", ""},
			{bCherryGrove, "", bMeadow, bMeadow, bOldPine},
			{bCherryGrove, bCherryGrove, bForest, bBirchForest, ""},
			{"", "", "", "", ""},
			{bErodedBadlands, bErodedBadlands, "", "", ""},
		},
		shat: [5][5]string{
			{bGravellyHills, bGravellyHills, bWindsweptHills, bWindsweptForest, bWindsweptForest},
			{bGravellyHills, bGravellyHills, bWindsweptHills, bWindsweptForest, bWindsweptForest},
			{bWindsweptHills, bWindsweptHills, bWindsweptHills, bWindsweptForest, bWindsweptForest},
			{"", "", "", "", ""},
			{"", "", "", "", ""},
		},
	}
	b.frozen = b.temps[0]
	b.unfrozen = vbJoin(b.temps[1], b.temps[4])
	return b
}

// vbOverworldEntries is OverworldBiomeBuilder.addBiomes.
func vbOverworldEntries() []vbEntry {
	b := newVBBuilder()
	b.addOffCoast()
	b.addInland()
	b.addUnderground()
	return b.out
}

func (b *vbBuilder) surface(temp, hum, cont, ero, weird vbParam, offset float32, biome string) {
	b.out = append(b.out,
		vbEntry{vbPoint(temp, hum, cont, ero, vbPt(0), weird, offset), biome},
		vbEntry{vbPoint(temp, hum, cont, ero, vbPt(1), weird, offset), biome})
}

func (b *vbBuilder) underground(temp, hum, cont, ero, weird vbParam, offset float32, biome string) {
	b.out = append(b.out, vbEntry{vbPoint(temp, hum, cont, ero, vbSpan(0.2, 0.9), weird, offset), biome})
}

func (b *vbBuilder) bottom(temp, hum, cont, ero, weird vbParam, offset float32, biome string) {
	b.out = append(b.out, vbEntry{vbPoint(temp, hum, cont, ero, vbPt(1.1), weird, offset), biome})
}

func (b *vbBuilder) addOffCoast() {
	b.surface(b.full, b.full, b.mushroom, b.full, b.full, 0, bMushroomFields)
	for ti, t := range b.temps {
		b.surface(t, b.full, b.deepOcean, b.full, b.full, 0, b.oceans[0][ti])
		b.surface(t, b.full, b.ocean, b.full, b.full, 0, b.oceans[1][ti])
	}
}

func (b *vbBuilder) addInland() {
	b.addMidSlice(vbSpan(-1, -0.93333334))
	b.addHighSlice(vbSpan(-0.93333334, -0.7666667))
	b.addPeaks(vbSpan(-0.7666667, -0.56666666))
	b.addHighSlice(vbSpan(-0.56666666, -0.4))
	b.addMidSlice(vbSpan(-0.4, -0.26666668))
	b.addLowSlice(vbSpan(-0.26666668, -0.05))
	b.addValleys(vbSpan(-0.05, 0.05))
	b.addLowSlice(vbSpan(0.05, 0.26666668))
	b.addMidSlice(vbSpan(0.26666668, 0.4))
	b.addHighSlice(vbSpan(0.4, 0.56666666))
	b.addPeaks(vbSpan(0.56666666, 0.7666667))
	b.addHighSlice(vbSpan(0.7666667, 0.93333334))
	b.addMidSlice(vbSpan(0.93333334, 1))
}

func (b *vbBuilder) addPeaks(w vbParam) {
	for ti, t := range b.temps {
		for hi, h := range b.hums {
			middle := b.pickMiddle(ti, hi, w)
			middleBad := b.pickMiddleOrBadlandsIfHot(ti, hi, w)
			middleBadSlope := b.pickMiddleOrBadlandsIfHotOrSlopeIfCold(ti, hi, w)
			plateau := b.pickPlateau(ti, hi, w)
			shattered := b.pickShattered(ti, hi, w)
			shatteredSav := b.maybeWindsweptSavanna(ti, hi, w, shattered)
			peak := b.pickPeak(ti, hi, w)
			b.surface(t, h, vbJoin(b.coast, b.farInland), b.eros[0], w, 0, peak)
			b.surface(t, h, vbJoin(b.coast, b.nearInland), b.eros[1], w, 0, middleBadSlope)
			b.surface(t, h, vbJoin(b.midInland, b.farInland), b.eros[1], w, 0, peak)
			b.surface(t, h, vbJoin(b.coast, b.nearInland), vbJoin(b.eros[2], b.eros[3]), w, 0, middle)
			b.surface(t, h, vbJoin(b.midInland, b.farInland), b.eros[2], w, 0, plateau)
			b.surface(t, h, b.midInland, b.eros[3], w, 0, middleBad)
			b.surface(t, h, b.farInland, b.eros[3], w, 0, plateau)
			b.surface(t, h, vbJoin(b.coast, b.farInland), b.eros[4], w, 0, middle)
			b.surface(t, h, vbJoin(b.coast, b.nearInland), b.eros[5], w, 0, shatteredSav)
			b.surface(t, h, vbJoin(b.midInland, b.farInland), b.eros[5], w, 0, shattered)
			b.surface(t, h, vbJoin(b.coast, b.farInland), b.eros[6], w, 0, middle)
		}
	}
}

func (b *vbBuilder) addHighSlice(w vbParam) {
	for ti, t := range b.temps {
		for hi, h := range b.hums {
			middle := b.pickMiddle(ti, hi, w)
			middleBad := b.pickMiddleOrBadlandsIfHot(ti, hi, w)
			middleBadSlope := b.pickMiddleOrBadlandsIfHotOrSlopeIfCold(ti, hi, w)
			plateau := b.pickPlateau(ti, hi, w)
			shattered := b.pickShattered(ti, hi, w)
			middleSav := b.maybeWindsweptSavanna(ti, hi, w, middle)
			slope := b.pickSlope(ti, hi, w)
			peak := b.pickPeak(ti, hi, w)
			b.surface(t, h, b.coast, vbJoin(b.eros[0], b.eros[1]), w, 0, middle)
			b.surface(t, h, b.nearInland, b.eros[0], w, 0, slope)
			b.surface(t, h, vbJoin(b.midInland, b.farInland), b.eros[0], w, 0, peak)
			b.surface(t, h, b.nearInland, b.eros[1], w, 0, middleBadSlope)
			b.surface(t, h, vbJoin(b.midInland, b.farInland), b.eros[1], w, 0, slope)
			b.surface(t, h, vbJoin(b.coast, b.nearInland), vbJoin(b.eros[2], b.eros[3]), w, 0, middle)
			b.surface(t, h, vbJoin(b.midInland, b.farInland), b.eros[2], w, 0, plateau)
			b.surface(t, h, b.midInland, b.eros[3], w, 0, middleBad)
			b.surface(t, h, b.farInland, b.eros[3], w, 0, plateau)
			b.surface(t, h, vbJoin(b.coast, b.farInland), b.eros[4], w, 0, middle)
			b.surface(t, h, vbJoin(b.coast, b.nearInland), b.eros[5], w, 0, middleSav)
			b.surface(t, h, vbJoin(b.midInland, b.farInland), b.eros[5], w, 0, shattered)
			b.surface(t, h, vbJoin(b.coast, b.farInland), b.eros[6], w, 0, middle)
		}
	}
}

func (b *vbBuilder) addMidSlice(w vbParam) {
	b.surface(b.full, b.full, b.coast, vbJoin(b.eros[0], b.eros[2]), w, 0, bStonyShore)
	b.surface(vbJoin(b.temps[1], b.temps[2]), b.full, vbJoin(b.nearInland, b.farInland), b.eros[6], w, 0, bSwamp)
	b.surface(vbJoin(b.temps[3], b.temps[4]), b.full, vbJoin(b.nearInland, b.farInland), b.eros[6], w, 0, bMangroveSwamp)
	for ti, t := range b.temps {
		for hi, h := range b.hums {
			middle := b.pickMiddle(ti, hi, w)
			middleBad := b.pickMiddleOrBadlandsIfHot(ti, hi, w)
			middleBadSlope := b.pickMiddleOrBadlandsIfHotOrSlopeIfCold(ti, hi, w)
			shattered := b.pickShattered(ti, hi, w)
			plateau := b.pickPlateau(ti, hi, w)
			beach := b.pickBeach(ti, hi)
			middleSav := b.maybeWindsweptSavanna(ti, hi, w, middle)
			shatteredCoast := b.pickShatteredCoast(ti, hi, w)
			slope := b.pickSlope(ti, hi, w)
			b.surface(t, h, vbJoin(b.nearInland, b.farInland), b.eros[0], w, 0, slope)
			b.surface(t, h, vbJoin(b.nearInland, b.midInland), b.eros[1], w, 0, middleBadSlope)
			if ti == 0 {
				b.surface(t, h, b.farInland, b.eros[1], w, 0, slope)
			} else {
				b.surface(t, h, b.farInland, b.eros[1], w, 0, plateau)
			}
			b.surface(t, h, b.nearInland, b.eros[2], w, 0, middle)
			b.surface(t, h, b.midInland, b.eros[2], w, 0, middleBad)
			b.surface(t, h, b.farInland, b.eros[2], w, 0, plateau)
			b.surface(t, h, vbJoin(b.coast, b.nearInland), b.eros[3], w, 0, middle)
			b.surface(t, h, vbJoin(b.midInland, b.farInland), b.eros[3], w, 0, middleBad)
			if w.max < 0 {
				b.surface(t, h, b.coast, b.eros[4], w, 0, beach)
				b.surface(t, h, vbJoin(b.nearInland, b.farInland), b.eros[4], w, 0, middle)
			} else {
				b.surface(t, h, vbJoin(b.coast, b.farInland), b.eros[4], w, 0, middle)
			}
			b.surface(t, h, b.coast, b.eros[5], w, 0, shatteredCoast)
			b.surface(t, h, b.nearInland, b.eros[5], w, 0, middleSav)
			b.surface(t, h, vbJoin(b.midInland, b.farInland), b.eros[5], w, 0, shattered)
			if w.max < 0 {
				b.surface(t, h, b.coast, b.eros[6], w, 0, beach)
			} else {
				b.surface(t, h, b.coast, b.eros[6], w, 0, middle)
			}
			if ti == 0 {
				b.surface(t, h, vbJoin(b.nearInland, b.farInland), b.eros[6], w, 0, middle)
			}
		}
	}
}

func (b *vbBuilder) addLowSlice(w vbParam) {
	b.surface(b.full, b.full, b.coast, vbJoin(b.eros[0], b.eros[2]), w, 0, bStonyShore)
	b.surface(vbJoin(b.temps[1], b.temps[2]), b.full, vbJoin(b.nearInland, b.farInland), b.eros[6], w, 0, bSwamp)
	b.surface(vbJoin(b.temps[3], b.temps[4]), b.full, vbJoin(b.nearInland, b.farInland), b.eros[6], w, 0, bMangroveSwamp)
	for ti, t := range b.temps {
		for hi, h := range b.hums {
			middle := b.pickMiddle(ti, hi, w)
			middleBad := b.pickMiddleOrBadlandsIfHot(ti, hi, w)
			middleBadSlope := b.pickMiddleOrBadlandsIfHotOrSlopeIfCold(ti, hi, w)
			beach := b.pickBeach(ti, hi)
			middleSav := b.maybeWindsweptSavanna(ti, hi, w, middle)
			shatteredCoast := b.pickShatteredCoast(ti, hi, w)
			b.surface(t, h, b.nearInland, vbJoin(b.eros[0], b.eros[1]), w, 0, middleBad)
			b.surface(t, h, vbJoin(b.midInland, b.farInland), vbJoin(b.eros[0], b.eros[1]), w, 0, middleBadSlope)
			b.surface(t, h, b.nearInland, vbJoin(b.eros[2], b.eros[3]), w, 0, middle)
			b.surface(t, h, vbJoin(b.midInland, b.farInland), vbJoin(b.eros[2], b.eros[3]), w, 0, middleBad)
			b.surface(t, h, b.coast, vbJoin(b.eros[3], b.eros[4]), w, 0, beach)
			b.surface(t, h, vbJoin(b.nearInland, b.farInland), b.eros[4], w, 0, middle)
			b.surface(t, h, b.coast, b.eros[5], w, 0, shatteredCoast)
			b.surface(t, h, b.nearInland, b.eros[5], w, 0, middleSav)
			b.surface(t, h, vbJoin(b.midInland, b.farInland), b.eros[5], w, 0, middle)
			b.surface(t, h, b.coast, b.eros[6], w, 0, beach)
			if ti == 0 {
				b.surface(t, h, vbJoin(b.nearInland, b.farInland), b.eros[6], w, 0, middle)
			}
		}
	}
}

func (b *vbBuilder) addValleys(w vbParam) {
	shoreOr := func(r string) string {
		if w.max < 0 {
			return bStonyShore
		}
		return r
	}
	b.surface(b.frozen, b.full, b.coast, vbJoin(b.eros[0], b.eros[1]), w, 0, shoreOr(bFrozenRiver))
	b.surface(b.unfrozen, b.full, b.coast, vbJoin(b.eros[0], b.eros[1]), w, 0, shoreOr(bRiver))
	b.surface(b.frozen, b.full, b.nearInland, vbJoin(b.eros[0], b.eros[1]), w, 0, bFrozenRiver)
	b.surface(b.unfrozen, b.full, b.nearInland, vbJoin(b.eros[0], b.eros[1]), w, 0, bRiver)
	b.surface(b.frozen, b.full, vbJoin(b.coast, b.farInland), vbJoin(b.eros[2], b.eros[5]), w, 0, bFrozenRiver)
	b.surface(b.unfrozen, b.full, vbJoin(b.coast, b.farInland), vbJoin(b.eros[2], b.eros[5]), w, 0, bRiver)
	b.surface(b.frozen, b.full, b.coast, b.eros[6], w, 0, bFrozenRiver)
	b.surface(b.unfrozen, b.full, b.coast, b.eros[6], w, 0, bRiver)
	b.surface(vbJoin(b.temps[1], b.temps[2]), b.full, vbJoin(b.inland, b.farInland), b.eros[6], w, 0, bSwamp)
	b.surface(vbJoin(b.temps[3], b.temps[4]), b.full, vbJoin(b.inland, b.farInland), b.eros[6], w, 0, bMangroveSwamp)
	b.surface(b.frozen, b.full, vbJoin(b.inland, b.farInland), b.eros[6], w, 0, bFrozenRiver)
	for ti, t := range b.temps {
		for hi, h := range b.hums {
			b.surface(t, h, vbJoin(b.midInland, b.farInland), vbJoin(b.eros[0], b.eros[1]), w, 0, b.pickMiddleOrBadlandsIfHot(ti, hi, w))
		}
	}
}

func (b *vbBuilder) addUnderground() {
	b.underground(b.full, b.full, vbSpan(0.8, 1), b.full, b.full, 0, bDripstoneCaves)
	b.underground(b.full, vbSpan(0.7, 1), b.full, b.full, b.full, 0, bLushCaves)
	b.underground(b.full, b.full, vbJoin(b.coast, b.inland), vbJoin(b.eros[5], b.eros[6]), vbSpan(-1.1, -0.85), 0, bSulfurCaves)
	b.bottom(b.full, b.full, b.full, vbJoin(b.eros[0], b.eros[1]), b.full, 0, bDeepDark)
}

func (b *vbBuilder) pickMiddle(ti, hi int, w vbParam) string {
	if w.max < 0 {
		return b.middle[ti][hi]
	}
	if v := b.middleVar[ti][hi]; v != "" {
		return v
	}
	return b.middle[ti][hi]
}

func (b *vbBuilder) pickMiddleOrBadlandsIfHot(ti, hi int, w vbParam) string {
	if ti == 4 {
		return b.pickBadlands(hi, w)
	}
	return b.pickMiddle(ti, hi, w)
}

func (b *vbBuilder) pickMiddleOrBadlandsIfHotOrSlopeIfCold(ti, hi int, w vbParam) string {
	if ti == 0 {
		return b.pickSlope(ti, hi, w)
	}
	return b.pickMiddleOrBadlandsIfHot(ti, hi, w)
}

func (b *vbBuilder) maybeWindsweptSavanna(ti, hi int, w vbParam, under string) string {
	if ti > 1 && hi < 4 && w.max >= 0 {
		return bWindsweptSavanna
	}
	return under
}

func (b *vbBuilder) pickShatteredCoast(ti, hi int, w vbParam) string {
	var r string
	if w.max >= 0 {
		r = b.pickMiddle(ti, hi, w)
	} else {
		r = b.pickBeach(ti, hi)
	}
	return b.maybeWindsweptSavanna(ti, hi, w, r)
}

func (b *vbBuilder) pickBeach(ti, _ int) string {
	switch ti {
	case 0:
		return bSnowyBeach
	case 4:
		return bDesert
	}
	return bBeach
}

func (b *vbBuilder) pickBadlands(hi int, w vbParam) string {
	switch {
	case hi < 2 && w.max < 0:
		return bBadlands
	case hi < 2:
		return bErodedBadlands
	case hi < 3:
		return bBadlands
	}
	return bWoodedBadlands
}

func (b *vbBuilder) pickPlateau(ti, hi int, w vbParam) string {
	if w.max >= 0 {
		if v := b.plateauVar[ti][hi]; v != "" {
			return v
		}
	}
	return b.plateau[ti][hi]
}

func (b *vbBuilder) pickPeak(ti, hi int, w vbParam) string {
	switch {
	case ti <= 2 && w.max < 0:
		return bJaggedPeaks
	case ti <= 2:
		return bFrozenPeaks
	case ti == 3:
		return bStonyPeaks
	}
	return b.pickBadlands(hi, w)
}

func (b *vbBuilder) pickSlope(ti, hi int, w vbParam) string {
	if ti >= 3 {
		return b.pickPlateau(ti, hi, w)
	}
	if hi <= 1 {
		return bSnowySlopes
	}
	return bGrove
}

func (b *vbBuilder) pickShattered(ti, hi int, w vbParam) string {
	if v := b.shat[ti][hi]; v != "" {
		return v
	}
	return b.pickMiddle(ti, hi, w)
}

// ---- the sources ----

// vbChunkBiomes is one chunk's noise biomes: 4x4 quarts across, every
// quart from the bottom of the dimension to the top, as vanilla stores
// them (LevelChunkSection biomes).
type vbChunkBiomes struct {
	minQY int
	b     []string // index ((qy-minQY)*4 + qz)*4 + qx
}

// vbMultiNoise is a MultiNoiseBiomeSource over a climate sampler.
type vbMultiNoise struct {
	tree    *vbTree
	climate VanillaClimate
	minQY   int // the dimension's bottom quart (QuartPos.fromBlock(minY))
	quartsY int // its height in quarts

	mu     sync.Mutex
	chunks map[[2]int32]*vbChunkBiomes
	order  [][2]int32
}

const vbChunkCacheSize = 1024

func newVBMultiNoise(tree *vbTree, c VanillaClimate, minY, height int) *vbMultiNoise {
	return &vbMultiNoise{tree: tree, climate: c, minQY: minY >> 2, quartsY: height >> 2,
		chunks: map[[2]int32]*vbChunkBiomes{}}
}

// vbColumnClimate is a VanillaClimate that samples a quart column at once
// (the climate is two-dimensional but for depth's y gradient): out[i] is
// Sample(qx, qy0+i, qz), the same values.
type vbColumnClimate interface {
	SampleColumn(qx, qz, qy0 int, out []ClimatePoint)
}

// fillChunk is ChunkGenerator.doCreateBiomes: every quart of the chunk in
// ChunkAccess.fillBiomesFromNoise's order (sections bottom up; in each,
// x, then y, then z), each search seeded with the one before. hint is the
// search's starting hint and is returned updated.
func (m *vbMultiNoise) fillChunk(cx, cz int32, hint *vbNode) (*vbChunkBiomes, *vbNode) {
	out := &vbChunkBiomes{minQY: m.minQY, b: make([]string, m.quartsY*16)}
	qx0, qz0 := int(cx)*4, int(cz)*4
	var cols [16][]ClimatePoint
	colc, columns := m.climate.(vbColumnClimate)
	if columns {
		for i := range cols {
			cols[i] = make([]ClimatePoint, m.quartsY)
			colc.SampleColumn(qx0+(i&3), qz0+(i>>2), m.minQY, cols[i])
		}
	}
	for sec := 0; sec < m.quartsY/4; sec++ {
		for x := 0; x < 4; x++ {
			for y := 0; y < 4; y++ {
				for z := 0; z < 4; z++ {
					qy := sec*4 + y
					var p ClimatePoint
					if columns {
						p = cols[z<<2|x][qy] // column i = z*4 + x
					} else {
						p = m.climate.Sample(qx0+x, m.minQY+qy, qz0+z)
					}
					hint = m.tree.find(p, hint)
					out.b[(qy*4+z)*4+x] = hint.biome
				}
			}
		}
	}
	return out, hint
}

func (m *vbMultiNoise) chunk(cx, cz int32) *vbChunkBiomes {
	key := [2]int32{cx, cz}
	m.mu.Lock()
	c := m.chunks[key]
	m.mu.Unlock()
	if c != nil {
		return c
	}
	c, _ = m.fillChunk(cx, cz, nil)
	m.mu.Lock()
	if old := m.chunks[key]; old != nil {
		c = old
	} else {
		m.chunks[key] = c
		m.order = append(m.order, key)
		if len(m.order) > vbChunkCacheSize {
			delete(m.chunks, m.order[0])
			m.order = m.order[1:]
		}
	}
	m.mu.Unlock()
	return c
}

// BiomeAt is the noise biome at a quart, as the chunk holding it stores
// it (y held to the dimension, as ChunkAccess.getNoiseBiome does).
func (m *vbMultiNoise) BiomeAt(qx, qy, qz int) string {
	c := m.chunk(int32(qx>>2), int32(qz>>2))
	qy = min(max(qy-m.minQY, 0), m.quartsY-1)
	return c.b[(qy*4+(qz&3))*4+(qx&3)]
}

// NoiseBiome is getNoiseBiome(sampler.sample(q)) alone, with no hint
// carried in: the biome a structure or locate query computes on its own.
func (m *vbMultiNoise) NoiseBiome(qx, qy, qz int) string {
	return m.tree.find(m.climate.Sample(qx, qy, qz), nil).biome
}

// vbFixed is FixedBiomeSource.
type vbFixed string

func (f vbFixed) BiomeAt(qx, qy, qz int) string { return string(f) }

// vbEnd is TheEndBiomeSource over the End's erosion (end/islands).
type vbEnd struct{ islands *vbEndIslands }

// BiomeAt is TheEndBiomeSource.getNoiseBiome.
func (e *vbEnd) BiomeAt(qx, qy, qz int) string {
	bx, bz := qx<<2, qz<<2
	cx, cz := int64(bx>>4), int64(bz>>4)
	if cx*cx+cz*cz <= 4096 {
		return "minecraft:the_end"
	}
	h := float64(e.islands.erosion(int((bx>>4)*2+1)*8, int((bz>>4)*2+1)*8))
	switch {
	case h > 0.25:
		return "minecraft:end_highlands"
	case h >= -0.0625:
		return "minecraft:end_midlands"
	case h < -0.21875:
		return "minecraft:small_end_islands"
	}
	return "minecraft:end_barrens"
}

// NewVanillaBiomes builds a dimension's vanilla biome source: the
// overworld's and Nether's multi-noise sources over ctx.Climate (or this
// file's own climate samplers when the terrain core passes none), the
// End's ring source, a fixed plains for the flat and single-biome presets.
func NewVanillaBiomes(ctx VanillaGenContext) VanillaBiomes {
	switch ctx.Dim {
	case DimNether:
		c := ctx.Climate
		if c == nil {
			c = NewVanillaNetherClimate(ctx.Seed)
		}
		return newVBMultiNoise(vbNether(), c, 0, 256)
	case DimEnd:
		return &vbEnd{islands: newVBEndIslands(ctx.Seed)}
	}
	switch ctx.Preset {
	case PresetFlat, PresetSingleBiome:
		return vbFixed(bPlains)
	}
	c := ctx.Climate
	if c == nil {
		c = NewVanillaOverworldClimate(ctx.Seed, ctx.Preset)
	}
	return newVBMultiNoise(vbOverworld(), c, -64, 384)
}

func init() { RegisterVanillaBiomes(NewVanillaBiomes) }

// ---- the zoom ----

// VanillaBiomeZoom is BiomeManager: which quart's biome a block shows —
// the nearest of the eight surrounding quart corners after each is
// jittered by a hash of the (obfuscated) world seed.
type VanillaBiomeZoom struct{ seed int64 }

// NewVanillaBiomeZoom is new BiomeManager(…, BiomeManager.obfuscateSeed(seed)):
// the first eight bytes, little-endian, of SHA-256 over the seed's eight
// little-endian bytes.
func NewVanillaBiomeZoom(seed int64) VanillaBiomeZoom {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(seed))
	h := sha256.Sum256(b[:])
	return VanillaBiomeZoom{int64(binary.LittleEndian.Uint64(h[:8]))}
}

func vbLCG(r, c int64) int64 { return r*(r*6364136223846793005+1442695040888963407) + c }

func vbFiddle(r int64) float64 {
	u := float64(((r>>24)%1024+1024)%1024) / 1024.0
	return (u - 0.5) * 0.9
}

func (z VanillaBiomeZoom) fiddled(x, y, zz int, dx, dy, dz float64) float64 {
	r := vbLCG(z.seed, int64(x))
	r = vbLCG(r, int64(y))
	r = vbLCG(r, int64(zz))
	r = vbLCG(r, int64(x))
	r = vbLCG(r, int64(y))
	r = vbLCG(r, int64(zz))
	fx := vbFiddle(r)
	r = vbLCG(r, z.seed)
	fy := vbFiddle(r)
	r = vbLCG(r, z.seed)
	fz := vbFiddle(r)
	sq := func(v float64) float64 { return v * v }
	return sq(dz+fz) + sq(dy+fy) + sq(dx+fx)
}

// Quart is BiomeManager.getBiome's choice of quart for a block.
func (z VanillaBiomeZoom) Quart(x, y, zz int) (qx, qy, qz int) {
	ax, ay, az := x-2, y-2, zz-2
	px, py, pz := ax>>2, ay>>2, az>>2
	fx, fy, fz := float64(ax&3)/4, float64(ay&3)/4, float64(az&3)/4
	best, bestD := 0, math.Inf(1)
	for i := 0; i < 8; i++ {
		cx, cy, cz := px, py, pz
		dx, dy, dz := fx, fy, fz
		if i&4 != 0 {
			cx, dx = px+1, fx-1
		}
		if i&2 != 0 {
			cy, dy = py+1, fy-1
		}
		if i&1 != 0 {
			cz, dz = pz+1, fz-1
		}
		if d := z.fiddled(cx, cy, cz, dx, dy, dz); bestD > d {
			best, bestD = i, d
		}
	}
	qx, qy, qz = px, py, pz
	if best&4 != 0 {
		qx++
	}
	if best&2 != 0 {
		qy++
	}
	if best&1 != 0 {
		qz++
	}
	return
}

// BiomeAt is the biome a block shows: b's biome at the zoomed quart.
func (z VanillaBiomeZoom) BiomeAt(b VanillaBiomes, x, y, zz int) string {
	qx, qy, qz := z.Quart(x, y, zz)
	return b.BiomeAt(qx, qy, qz)
}

// ---- chunk biomes ----

// VanillaQuartBiomes is a chunk's noise biomes as vanilla stores them:
// for each section bottom up, its 64 quarts in PalettedContainer order
// ((y*4 + z)*4 + x). minY is the dimension's bottom, sections its count.
func VanillaQuartBiomes(b VanillaBiomes, cx, cz int32, minY, sections int) [][64]string {
	out := make([][64]string, sections)
	qx0, qz0, qy0 := int(cx)*4, int(cz)*4, minY>>2
	for s := range out {
		for i := range out[s] {
			out[s][i] = b.BiomeAt(qx0+(i&3), qy0+s*4+(i>>4), qz0+((i>>2)&3))
		}
	}
	return out
}

// VanillaSectionBiomes is one biome per section, for a chunk format that
// carries no more (Chunk.Biomes): the section's most common noise biome,
// the first in storage order on a tie.
func VanillaSectionBiomes(b VanillaBiomes, cx, cz int32, minY, sections int) []string {
	q := VanillaQuartBiomes(b, cx, cz, minY, sections)
	out := make([]string, sections)
	for s := range q {
		count := map[string]int{}
		for _, n := range q[s] {
			count[n]++
		}
		best := q[s][0]
		for _, n := range q[s] {
			if count[n] > count[best] {
				best = n
			}
		}
		out[s] = best
	}
	return out
}
