package worldgen

import "math"

// Vanilla tree generation — the trunk and foliage placers, ported from the
// 26.2 tree (TreeFeature, trunkplacers/*, foliageplacers/*) and parameterised
// from the configured_feature JSONs.
//
// Trees used to be hand-rolled silhouettes here: one column of log and a
// radius-2 blob of leaves, with a `conical` flag for spruce. That is the kind
// of "close enough" that makes a world feel wrong without anyone being able to
// say why — and it hid a real bug for months, because the generator built dark
// oak on one column while the sapling grower built it on four, and nothing
// noticed since "a tree appeared" satisfied every check.
//
// So this is one implementation with two drivers. The chunk stamper and the
// sapling grower both call PlaceTree; they cannot disagree about what a tree is.

// TreeRNG is the randomness a tree is grown from. The DRAW ORDER matters as
// much as the formulas: vanilla's shape distribution is a consequence of which
// call happens when, so the ports below keep the sequence even where a
// different order would read better.
type TreeRNG interface {
	Intn(n int) int
	Float64() float64
}

// TreeSetter places one block of a tree. Leaves are placed with leaf=true so a
// driver can refuse to overwrite anything but air with them.
type TreeSetter func(x, y, z int, state uint32, leaf bool)

// TreeFree reports whether a cell is free for a tree to grow into (vanilla
// TreeFeature.validTreePos: air, leaves, or replaceable plants).
type TreeFree func(x, y, z int) bool

// TreeRead answers what actually stands at a cell. Decorators that inspect
// the ground — the podzol rule's #dirt check — need real block states, not
// just free/occupied. Reads and their gated writes are always the SAME cell,
// so a driver that can only answer for part of the world (the chunk stamper)
// stays consistent: what it can't read it also won't write.
type TreeRead func(x, y, z int) uint32

// TreeDriver bundles what a driver supplies PlaceTree about the world.
type TreeDriver struct {
	Set  TreeSetter
	Free TreeFree
	Read TreeRead
	// DirtGround answers setDirtAt's "is the ground already #dirt" test —
	// separately from Read, because its answer decides whether the dirt cell
	// joins the trunk-position list, and THAT feeds decorator geometry and
	// shuffle sizes. The live driver answers from the real world; the chunk
	// stamper answers from the TERRAIN MODEL, so every pass over a
	// chunk-straddling tree records the same list — a buffer read would flip
	// with a neighbouring tree's decorations and desynchronise the passes.
	DirtGround func(x, y, z int) bool
	// SurfaceTop is the MOTION_BLOCKING_NO_LEAVES heightmap answer for a
	// column, MINUS this tree (PlaceTree folds its own logs in): the first Y
	// with nothing motion-blocking except leaves at or above it. Leaf litter
	// only settles on ground open to the sky through at most a canopy.
	SurfaceTop func(x, z int) int
	// RootThrough reports #mangrove_roots_can_grow_through beyond what Free
	// allows — mud, existing roots, moss carpet, vines, propagules, snow.
	// Like DirtGround, the chunk stamper answers from the terrain model so
	// the root walk (whose DRAWS follow its reads) is identical in every
	// pass; the live driver reads the real world.
	RootThrough func(x, y, z int) bool
}

// TrunkKind selects one of vanilla's nine trunk placers.
type TrunkKind uint8

const (
	TrunkStraight TrunkKind = iota
	TrunkForking
	TrunkDarkOak
	TrunkGiant
	TrunkMegaJungle
	TrunkBending
	TrunkUpwardsBranching
	TrunkCherry
	TrunkFancy
)

// FoliageKind selects one of vanilla's foliage placers.
type FoliageKind uint8

const (
	FoliageBlob FoliageKind = iota
	FoliageFancy
	FoliageDarkOak
	FoliageAcacia
	FoliageBush
	FoliageSpruce
	FoliagePine
	FoliageMegaPine
	FoliageMegaJungle
	FoliageRandomSpread
	FoliageCherry
)

// TreeConfig is one configured_feature: the blocks, the two placers, and the
// numbers the JSON carries for them.
type TreeConfig struct {
	Log, Leaves uint32
	// A weighted foliage provider (azalea's 3:1 flowering mix): each placed
	// leaf rolls Leaves2 at Leaves2Chance. Zero for the single-leaf species.
	Leaves2       uint32
	Leaves2Chance float64

	Trunk                            TrunkKind
	BaseHeight, HeightRandA, HeightB int

	Foliage              FoliageKind
	RadiusMin, RadiusMax int
	OffsetMin, OffsetMax int
	FoliageH             int // blob/bush/mega_jungle fixed height
	FoliageHMin          int // sampled heights (pine/cherry/mega_pine)
	FoliageHMax          int

	// bending
	MinHeightForLeaves           int
	BendLengthMin, BendLengthMax int
	// upwards_branching (mangrove)
	BranchProbability                        float64
	ExtraBranchStepsMin, ExtraBranchStepsMax int
	ExtraBranchLenMin, ExtraBranchLenMax     int
	// cherry
	BranchCountMin, BranchCountMax         int
	BranchHorizMin, BranchHorizMax         int
	BranchStartMin, BranchStartMax         int
	BranchEndMin, BranchEndMax             int
	HangingLeavesChance, HangingExtChance  float64
	WideBottomHoleChance, CornerHoleChance float64
	// spruce
	TrunkHeightMin, TrunkHeightMax int
	// random_spread
	LeafPlacementAttempts int

	// minimum_size — the clearance a tree needs to grow here at all.
	SizeThreeLayer                        bool
	SizeLimit, SizeLower                  int
	SizeMiddle, SizeUpperLimit, SizeUpper int
	MinClippedHeight                      int

	// decorators — the dressing vanilla applies after placement. Only the
	// ones ported so far; a feature with none of these grows bare.
	TrunkVine     bool    // vines on the trunk sides (jungle)
	LeaveVineProb float64 // vines hanging off the canopy (jungle, swamp, mangrove)
	CocoaProb     float64 // cocoa pods on the lower trunk (jungle)
	HeartProb     float64 // a creaking heart in a fully log-enclosed log (pale oak)
	AlterGround   bool    // podzol circles at the base (mega spruce/pine)
	// attached_to_leaves — hanging mangrove propagules under the canopy.
	PropaguleProb                   float64
	PropaguleExclXZ, PropaguleExclY int
	PropaguleEmpty                  int
	// pale_moss — a moss patch in the ground and hanging strands (pale oak).
	PaleMossLeaves, PaleMossTrunk, PaleMossGround float64
	// beehive — a bee nest hung off the trunk at the canopy's bottom row.
	BeehiveProb float64

	// dirt_provider / force_dirt — what a trunk placer sets the ground to.
	// Every placer converts the block under the trunk (setDirtAt) unless it
	// is already in #dirt; force_dirt (azalea's rooted dirt) always writes.
	DirtState uint32
	ForceDirt bool

	// place_on_ground — leaf litter scattered around the base (the forest
	// and dark-forest tree variants run two passes with different reach).
	LitterPasses []LitterPass

	// mangrove_root_placer — the trunk stands one to three blocks above the
	// origin and stilted roots fill and fan out beneath it. A feature has
	// roots when RootMaxLength is non-zero.
	RootTrunkOffMin, RootTrunkOffMax int
	RootMaxLength, RootMaxWidth      int
	RootSkewChance                   float64
	RootState, RootMuddyState        uint32
	AboveRootChance                  float64
	AboveRootState                   uint32
}

// LitterPass is one place_on_ground decorator: Tries random cells in the
// box around the lowest trunk row inflated by Radius/Height, littered with
// segment counts 1..MaxSeg on a uniformly random facing.
type LitterPass struct {
	Tries, Radius, Height, MaxSeg int
}

// foliageAttachment is a point a foliage blob grows from. A trunk placer
// returns a LIST of them, which is the whole mechanism behind branching trees —
// each branch end grows its own canopy. tachyne had no equivalent at all, which
// is why every tree here was a single blob on a stick.
type foliageAttachment struct {
	x, y, z      int
	radiusOffset int
	doubleTrunk  bool
}

// leafState draws the state for ONE placed leaf — vanilla's foliage provider
// runs per block inside tryPlaceLeaf, which is how azaleas mix flowering
// patches through the canopy rather than being one or the other.
func (c *TreeConfig) leafState(rng TreeRNG) uint32 {
	if c.Leaves2 != 0 && rng.Float64() < c.Leaves2Chance {
		return c.Leaves2
	}
	return c.Leaves
}

// sampleInt is vanilla's UniformInt.sample: min + rand(max-min+1).
func sampleInt(rng TreeRNG, lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + rng.Intn(hi-lo+1)
}

// treeHeightOf is TrunkPlacer.getTreeHeight.
func (c *TreeConfig) treeHeightOf(rng TreeRNG) int {
	return c.BaseHeight + rng.Intn(c.HeightRandA+1) + rng.Intn(c.HeightB+1)
}

// sizeAtHeight is the clearance a tree needs at one height — vanilla's
// TwoLayersFeatureSize / ThreeLayersFeatureSize. A trunk needs a column; the
// canopy needs room to spread.
func (c *TreeConfig) sizeAtHeight(treeHeight, yo int) int {
	if yo < c.SizeLimit {
		return c.SizeLower
	}
	if c.SizeThreeLayer {
		if yo >= treeHeight-c.SizeUpperLimit {
			return c.SizeUpper
		}
		return c.SizeMiddle
	}
	return c.SizeUpper
}

// maxFreeHeight is how tall this tree can grow here before something is in the
// way — vanilla's getMaxFreeTreeHeight.
//
// THIS is what stops a tree growing inside a house, under a low cave roof, or
// up through a floor. The space is measured BEFORE a single block is placed,
// and a tree that does not fit is refused outright rather than stamped
// half-height through somebody's ceiling. A placer decides what a tree looks
// like; this decides whether there is a tree at all.
func (c *TreeConfig) maxFreeHeight(x, y, z, maxHeight int, free TreeFree) int {
	for yo := 0; yo <= maxHeight+1; yo++ {
		r := c.sizeAtHeight(maxHeight, yo)
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if !free(x+dx, y+yo, z+dz) {
					return yo - 2
				}
			}
		}
	}
	return maxHeight
}

// PlaceTree grows one tree at (x,y,z), the cell the sapling stood in, and
// reports whether one actually grew: a cramped spot grows nothing.
//
// The roll order is TreeFeature.doPlace's and is load-bearing: height, then
// foliage height, then radius. Changing it changes the distribution of shapes
// even with identical formulas.
func PlaceTree(c *TreeConfig, x, y, z int, rng TreeRNG, d TreeDriver) bool {
	set, free, read := d.Set, d.Free, d.Read
	treeHeight := c.treeHeightOf(rng)
	foliageHeight := c.foliageHeightOf(rng, treeHeight)
	trunkHeight := treeHeight - foliageHeight
	leafRadius := c.foliageRadiusOf(rng, trunkHeight)
	offset := sampleInt(rng, c.OffsetMin, c.OffsetMax)

	// A root placer raises the trunk: the trunk origin sits above the
	// planted cell and the roots will fill the gap (getTrunkOrigin).
	ty := y
	if c.RootMaxLength > 0 {
		ty = y + sampleInt(rng, c.RootTrunkOffMin, c.RootTrunkOffMax)
	}

	// The fit check, measured at the TRUNK origin as vanilla measures it.
	// Most species refuse outright; the ones carrying a min_clipped_height
	// (the large oak) will settle for a shorter tree.
	clipped := c.maxFreeHeight(x, ty, z, treeHeight, free)
	if clipped < treeHeight {
		if c.MinClippedHeight <= 0 || clipped < c.MinClippedHeight {
			return false
		}
	}
	treeHeight = clipped

	// Record what this tree places so the leaf distances can be seeded — the
	// per-tree half of vanilla's updateLeaves. Leaves go in at distance 7 and
	// are rewritten with their true distance from the trunk, or the decay rule
	// would rot a freshly grown canopy on its first random ticks.
	// Ordered as well as set-indexed: the decorators walk the tree's blocks in
	// PLACEMENT order, and since they draw randomness per block, iteration
	// order is part of determinism — a map walk would grow different vines on
	// the same tree every regeneration.
	logs := map[[3]int]bool{}
	rootCells := map[[3]int]bool{}
	// Leaves record their PLACED STATE, not just membership: the azalea mixes
	// two leaf blocks through one canopy, and the distance-seeding rewrite
	// must preserve which one landed where.
	leaves := map[[3]int]uint32{}
	var logList, leafList [][3]int
	recording := func(px, py, pz int, st uint32, leaf bool) {
		q := [3]int{px, py, pz}
		if leaf {
			// tryPlaceLeaf refuses cells a tree cannot replace — the roots
			// placed moments ago are exactly that, and a leaf that was never
			// placed must not be recorded (a propagule would hang off it).
			if rootCells[q] {
				return
			}
			if !logs[q] {
				if _, seen := leaves[q]; !seen {
					leafList = append(leafList, q)
				}
				leaves[q] = st // the last write is what stands in the world
			}
		} else if !logs[q] {
			logs[q] = true
			logList = append(logList, q)
			delete(leaves, q)
		}
		set(px, py, pz, st, leaf)
	}

	// setDirtAt: every trunk placer converts the ground under the trunk to
	// the feature's dirt (grass becomes plain dirt; azalea forces rooted
	// dirt). A SUCCESSFUL write joins the trunk-position list — vanilla
	// writes through the trunk setter — which is what anchors the cocoa
	// window and the podzol circles at the dirt row; ground that is already
	// #dirt is left alone and stays OUT of the list, exactly as in vanilla.
	dirtPos := map[[3]int]bool{}
	dirt := func(px, py, pz int) {
		if c.DirtState == 0 {
			return
		}
		if !c.ForceDirt && d.DirtGround(px, py, pz) {
			return
		}
		q := [3]int{px, py, pz}
		if !logs[q] {
			logs[q] = true
			dirtPos[q] = true
			logList = append(logList, q)
			delete(leaves, q)
		}
		set(px, py, pz, c.DirtState, false)
	}

	// Roots go in BEFORE the trunk, and a walk that cannot complete refuses
	// the whole tree — vanilla returns false out of doPlace here too.
	if c.RootMaxLength > 0 {
		if !c.placeRoots(rng, x, y, z, ty, d, rootCells) {
			return false
		}
	}

	// The mangrove trunk grows THROUGH its own roots and carpets
	// (#mangrove_logs_can_grow_through) — without this the below-trunk
	// root's moss carpet blocks the base log and the trunk floats one
	// higher. Own cells only: chunk-independent, and the common case.
	trunkFree := free
	if c.RootMaxLength > 0 {
		trunkFree = func(px, py, pz int) bool {
			return free(px, py, pz) || rootCells[[3]int{px, py, pz}]
		}
	}

	atts := c.placeTrunk(rng, x, ty, z, treeHeight, recording, trunkFree, dirt)
	for _, a := range atts {
		c.createFoliage(rng, a, treeHeight, foliageHeight, leafRadius, offset, recording)
	}
	c.seedLeafDistances(logs, leaves, set)
	// Vanilla's TreeDecorator.Context hands decorators the log and leaf lists
	// STABLY SORTED by Y; the per-block probability rolls then run bottom-up.
	sortByY(logList)
	sortByY(leafList)
	// Decoration. isAir is the composite both drivers can answer: the cell is
	// free AND not part of this tree — vanilla's context.isAir sees the
	// just-placed tree in the world; here the tree knows its own blocks, and
	// tracks what its decorators add (a vine under a leaf must block the
	// propagule that would hang there, exactly as it does in vanilla's world).
	deco := map[[3]int]bool{}
	isAir := func(q [3]int) bool {
		if logs[q] || deco[q] || rootCells[q] {
			return false
		}
		if _, isLeaf := leaves[q]; isLeaf {
			return false
		}
		return free(q[0], q[1], q[2])
	}
	decoSet := func(px, py, pz int, st uint32, leaf bool) {
		deco[[3]int{px, py, pz}] = true
		set(px, py, pz, st, leaf)
	}
	c.decorate(&decoCtx{rng: rng, logs: logs, dirt: dirtPos,
		logList: logList, leafList: leafList, isAir: isAir, read: read, set: decoSet,
		surfaceTop: d.SurfaceTop})
	return true
}

// sortByY is the TreeDecorator.Context ordering: stable, lowest first.
func sortByY(list [][3]int) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j][1] < list[j-1][1]; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// shuffledCopy is Util.shuffledCopy — a Fisher-Yates over a copy, so the
// caller's placement-ordered list survives for the next decorator.
func shuffledCopy(rng TreeRNG, in [][3]int) [][3]int {
	out := append([][3]int(nil), in...)
	for i := len(out) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// decoCtx is what a decorator sees — vanilla's TreeDecorator.Context. The
// logs map and logList are TRUNK POSITIONS, which include the ground blocks
// setDirtAt converted (vanilla writes those through the trunk setter); dirt
// marks which ones they are, for the checks that need a real log.
type decoCtx struct {
	rng               TreeRNG
	logs              map[[3]int]bool
	dirt              map[[3]int]bool
	logList, leafList [][3]int
	isAir             func([3]int) bool
	read              TreeRead
	set               TreeSetter
	surfaceTop        func(x, z int) int
	logTop            map[[2]int]int // lazy: per column, first Y above this tree's logs
}

// heightTop is the MOTION_BLOCKING_NO_LEAVES heightmap for a column: the
// driver's answer for the world, raised by this tree's own logs.
func (ctx *decoCtx) heightTop(x, z int) int {
	if ctx.logTop == nil {
		ctx.logTop = map[[2]int]int{}
		for p := range ctx.logs {
			k := [2]int{p[0], p[2]}
			if t, ok := ctx.logTop[k]; !ok || p[1]+1 > t {
				ctx.logTop[k] = p[1] + 1
			}
		}
	}
	h := ctx.surfaceTop(x, z)
	if t, ok := ctx.logTop[[2]int{x, z}]; ok && t > h {
		h = t
	}
	return h
}

// Vine states: the base state is every face TRUE, faces ordered east, north,
// south, up, west — a single-face vine is the base plus the OTHER faces' bits.
const (
	vineEast  = 15 // 8+4+2+1
	vineNorth = 23 // 16+4+2+1
	vineSouth = 27 // 16+8+2+1
	vineWest  = 30 // 16+8+4+2
)

// decorate applies the ported tree decorators in vanilla's per-feature order:
// podzol, cocoa, trunk vines, hanging vines, propagules, and last the heart —
// no 1.21.11 feature orders these against each other differently.
func (c *TreeConfig) decorate(ctx *decoCtx) {
	rng := ctx.rng
	// AlterGroundDecorator (mega spruce/pine): podzol circles around the base.
	// A 5x5-minus-corners circle sits on each corner of the 2x2 trunk, and
	// five rolls may add circles on the rim of the surrounding 8x8. Each
	// column is probed from two above the base down to three below; the first
	// #dirt block found becomes podzol, and a solid non-dirt block below
	// ground level stops the probe (a cave roof is not a forest floor). The
	// ground test is vanilla's Feature.isGrassOrDirt.
	if c.AlterGround {
		minY := ctx.logList[0][1]
		circle := func(cx, cy, cz int) {
			for dx := -2; dx <= 2; dx++ {
				for dz := -2; dz <= 2; dz++ {
					if dx*dx == 4 && dz*dz == 4 {
						continue
					}
					for dy := 2; dy >= -3; dy-- {
						qx, qy, qz := cx+dx, cy+dy, cz+dz
						cur := ctx.read(qx, qy, qz)
						if IsDirtTag(cur) {
							ctx.set(qx, qy, qz, podzolState, false)
							break
						}
						if cur != Air && dy < 0 {
							break
						}
					}
				}
			}
		}
		for _, p := range ctx.logList {
			if p[1] != minY {
				continue
			}
			circle(p[0]-1, p[1], p[2]-1)
			circle(p[0]+2, p[1], p[2]-1)
			circle(p[0]-1, p[1], p[2]+2)
			circle(p[0]+2, p[1], p[2]+2)
			for i := 0; i < 5; i++ {
				n := rng.Intn(64)
				xx, zz := n%8, n/8
				if xx == 0 || xx == 7 || zz == 0 || zz == 7 {
					circle(p[0]-3+xx, p[1], p[2]-3+zz)
				}
			}
		}
	}
	// PlaceOnGroundDecorator: leaf litter scattered around the base. Each
	// pass throws Tries darts into the box around the lowest trunk row,
	// inflated by Radius/Height; a dart sticks where the cell above is free
	// (or a vine), the cell itself is solid ground, and the spot is open to
	// the sky through at most leaves. The facing and segment count are one
	// uniform draw over the pass's provider entries, PRE-DRAWN per dart so
	// the sequence never depends on what the darts hit.
	if len(c.LitterPasses) > 0 {
		litterBase := blockBase("leaf_litter")
		vlo, vhi := BlockRange("vine")
		minY := ctx.logList[0][1]
		minX, maxX := ctx.logList[0][0], ctx.logList[0][0]
		minZ, maxZ := ctx.logList[0][2], ctx.logList[0][2]
		for _, p := range ctx.logList {
			if p[1] != minY {
				continue
			}
			minX, maxX = min(minX, p[0]), max(maxX, p[0])
			minZ, maxZ = min(minZ, p[2]), max(maxZ, p[2])
		}
		for _, pass := range c.LitterPasses {
			x0, x1 := minX-pass.Radius, maxX+pass.Radius
			y0, y1 := minY-pass.Height, minY+pass.Height
			z0, z1 := minZ-pass.Radius, maxZ+pass.Radius
			for i := 0; i < pass.Tries; i++ {
				x := x0 + rng.Intn(x1-x0+1)
				y := y0 + rng.Intn(y1-y0+1)
				z := z0 + rng.Intn(z1-z0+1)
				pick := rng.Intn(4 * pass.MaxSeg)
				above := [3]int{x, y + 1, z}
				if !ctx.isAir(above) {
					if cur := ctx.read(x, y+1, z); cur < vlo || cur > vhi {
						continue
					}
				}
				if !IsSolidFull(ctx.read(x, y, z)) {
					continue
				}
				if y+1 < ctx.heightTop(x, z) {
					continue
				}
				// facing x segment_amount, facing outermost (north,south,west,east).
				ctx.set(x, y+1, z, litterBase+uint32(pick/pass.MaxSeg)*4+uint32(pick%pass.MaxSeg), true)
			}
		}
	}
	vineBase := blockBase("vine")
	cocoaBase := blockBase("cocoa")
	// CocoaDecorator: one gate roll, then pods on logs within 2 of the base,
	// each horizontal face at 25%, aged 0-2, the pod FACING its log.
	if c.CocoaProb > 0 && rng.Float64() < c.CocoaProb {
		// The window is anchored at the FIRST trunk position's Y — the dirt
		// row when the placer converted the ground, the base log otherwise.
		treeY := ctx.logList[0][1]
		for _, p := range ctx.logList {
			if p[1]-treeY > 2 {
				continue
			}
			for f, o := range [4][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}} { // north,south,west,east
				if rng.Float64() > 0.25 {
					continue
				}
				q := [3]int{p[0] - o[0], p[1], p[2] - o[1]}
				if ctx.isAir(q) {
					ctx.set(q[0], q[1], q[2], cocoaBase+uint32(rng.Intn(3)*4+f), true)
				}
			}
		}
	}
	// TrunkVineDecorator: each log side, 2 in 3.
	if c.TrunkVine {
		for _, p := range ctx.logList {
			c.vineAt(rng, p, ctx.isAir, ctx.set, vineBase, func() bool { return rng.Intn(3) > 0 }, 0)
		}
	}
	// LeaveVineDecorator: each leaf side at the probability, the vine hanging
	// up to four blocks down.
	if c.LeaveVineProb > 0 {
		for _, p := range ctx.leafList {
			c.vineAt(rng, p, ctx.isAir, ctx.set, vineBase, func() bool { return rng.Float64() < c.LeaveVineProb }, 4)
		}
	}
	// AttachedToLeavesDecorator (mangrove propagules): the leaves in shuffled
	// order; an already-excluded spot skips WITHOUT drawing, then the
	// probability roll, then the required-empty rule below the leaf. Each pod
	// excludes its neighbourhood so they don't clump. Hanging, stage 0, age
	// uniform 0-4.
	if c.PropaguleProb > 0 {
		propaguleBase := blockBase("mangrove_propagule")
		excluded := map[[3]int]bool{}
		for _, p := range shuffledCopy(rng, ctx.leafList) {
			q := [3]int{p[0], p[1] - 1, p[2]}
			if excluded[q] {
				continue
			}
			if rng.Float64() >= c.PropaguleProb {
				continue
			}
			ok := true
			for i := 1; i <= c.PropaguleEmpty; i++ {
				if !ctx.isAir([3]int{p[0], p[1] - i, p[2]}) {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			for dx := -c.PropaguleExclXZ; dx <= c.PropaguleExclXZ; dx++ {
				for dy := -c.PropaguleExclY; dy <= c.PropaguleExclY; dy++ {
					for dz := -c.PropaguleExclXZ; dz <= c.PropaguleExclXZ; dz++ {
						excluded[[3]int{q[0] + dx, q[1] + dy, q[2] + dz}] = true
					}
				}
			}
			// hanging propagule: age stride 8, hanging=true is +1.
			ctx.set(q[0], q[1], q[2], propaguleBase+uint32(rng.Intn(5))*8+1, true)
		}
	}
	// BeehiveDecorator: one roll, then a bee nest hung off a trunk side at
	// the canopy's bottom row — one below the lowest leaf, never below the
	// row above the base. Candidates are the south/west/east offsets of the
	// logs at that height (a nest faces SOUTH and needs air in front),
	// shuffled, first fit wins. The occupants are the server's business: it
	// seeds bees beside fresh nests when it first meets the chunk.
	if c.BeehiveProb > 0 && rng.Float64() < c.BeehiveProb {
		hiveY := 0
		if len(ctx.leafList) > 0 {
			hiveY = max(ctx.leafList[0][1]-1, ctx.logList[0][1]+1)
		} else {
			hiveY = min(ctx.logList[0][1]+1+rng.Intn(3), ctx.logList[len(ctx.logList)-1][1])
		}
		var cands [][3]int
		for _, p := range ctx.logList {
			if p[1] != hiveY {
				continue
			}
			cands = append(cands,
				[3]int{p[0], p[1], p[2] + 1}, // south
				[3]int{p[0] - 1, p[1], p[2]}, // west
				[3]int{p[0] + 1, p[1], p[2]}) // east
		}
		for _, q := range shuffledCopy(rng, cands) {
			if ctx.isAir(q) && ctx.isAir([3]int{q[0], q[1], q[2] + 1}) {
				// facing=south, honey_level=0 — facing strides by six levels.
				ctx.set(q[0], q[1], q[2], blockBase("bee_nest")+6, false)
				break
			}
		}
	}
	// PaleMossDecorator: the ground moss patch and the hanging strands —
	// before the heart, as pale_oak_creaking orders them.
	if c.PaleMossGround > 0 || c.PaleMossTrunk > 0 || c.PaleMossLeaves > 0 {
		c.paleMoss(ctx)
	}
	// CreakingHeartDecorator: one roll gates the tree, then the logs are
	// shuffled and the FIRST log with logs on all six faces becomes a
	// dormant, natural creaking heart. A plain 2x2 trunk has no such cell —
	// only the dark oak placer's diagonal bend folds two footprints into a
	// pocket — which is why hearts are rare in vanilla despite probability 1.
	// The neighbour test uses the tree's OWN logs: reading the world would
	// tie the chosen cell to whichever chunk happens to be drawing.
	if c.HeartProb > 0 && rng.Float64() < c.HeartProb {
		for _, p := range shuffledCopy(rng, ctx.logList) {
			enclosed := true
			for _, o := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
				q := [3]int{p[0] + o[0], p[1] + o[1], p[2] + o[2]}
				// vanilla tests the world for #logs — the converted dirt
				// under the trunk is a trunk POSITION but not a log.
				if !ctx.logs[q] || ctx.dirt[q] {
					enclosed = false
					break
				}
			}
			if enclosed {
				ctx.set(p[0], p[1], p[2], CreakingHeartDormant, false)
				break
			}
		}
	}
}

// vineAt rolls each horizontal side of a block for a vine, hanging it down.
func (c *TreeConfig) vineAt(rng TreeRNG, p [3]int, isAir func([3]int) bool, set TreeSetter,
	vineBase uint32, roll func() bool, hang int) {
	sides := [4]struct {
		dx, dz int
		face   uint32
	}{
		{-1, 0, vineEast},  // west of the block: the vine clings to its EAST face
		{1, 0, vineWest},   // east of the block
		{0, -1, vineSouth}, // north of the block
		{0, 1, vineNorth},  // south of the block
	}
	for _, sd := range sides {
		if !roll() {
			continue
		}
		q := [3]int{p[0] + sd.dx, p[1], p[2] + sd.dz}
		if !isAir(q) {
			continue
		}
		set(q[0], q[1], q[2], vineBase+sd.face, true)
		for i := 0; i < hang; i++ {
			q = [3]int{q[0], q[1] - 1, q[2]}
			if !isAir(q) {
				break
			}
			set(q[0], q[1], q[2], vineBase+sd.face, true)
		}
	}
}

// seedLeafDistances is TreeFeature.updateLeaves, per tree: a BFS out from the
// trunk assigning each placed leaf its distance, 1..6; anything further keeps
// the distance-7 state it was placed with. Leaf states pack distance x
// persistent x waterlogged with waterlogged innermost, so one distance step is
// four states.
func (c *TreeConfig) seedLeafDistances(logs map[[3]int]bool, leaves map[[3]int]uint32, set TreeSetter) {
	frontier := make([][3]int, 0, len(logs))
	for p := range logs {
		frontier = append(frontier, p)
	}
	seen := map[[3]int]bool{}
	for d := 1; d <= 6 && len(frontier) > 0; d++ {
		var next [][3]int
		for _, p := range frontier {
			for _, o := range [6][3]int{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
				q := [3]int{p[0] + o[0], p[1] + o[1], p[2] + o[2]}
				st, isLeaf := leaves[q]
				if !isLeaf || seen[q] {
					continue
				}
				seen[q] = true
				// Rewrite from the state that actually landed there — every
				// leaf family packs one distance step as four states, and the
				// azalea's flowering patches must stay flowering.
				set(q[0], q[1], q[2], st-uint32((7-d)*4), true)
				next = append(next, q)
			}
		}
		frontier = next
	}
}

// ---- trunk placers -----------------------------------------------------------

func (c *TreeConfig) placeTrunk(rng TreeRNG, x, y, z, h int, set TreeSetter, free TreeFree, dirt func(int, int, int)) []foliageAttachment {
	// A trunk log stands upright. c.Log is the BASE state, which for a log is
	// axis=x — placing it raw lays the whole forest on its side, which is
	// exactly what the first generated table did.
	log := func(px, py, pz int) bool {
		if !free(px, py, pz) {
			return false
		}
		set(px, py, pz, axisLog(c.Log, 1), false)
		return true
	}
	logAxis := func(px, py, pz int, axis int) {
		if free(px, py, pz) {
			set(px, py, pz, axisLog(c.Log, axis), false)
		}
	}
	// setDirtAt first, at every placer's vanilla call site — single-column
	// placers convert the one block under the trunk, the 2x2 giants convert
	// four. Mangrove's upwards-branching placer never touches the ground
	// (vanilla gives that job to its root placer). No randomness is drawn
	// here, so the shapes are unchanged.
	switch c.Trunk {
	case TrunkUpwardsBranching:
	case TrunkDarkOak, TrunkGiant, TrunkMegaJungle:
		dirt(x, y-1, z)
		dirt(x+1, y-1, z)
		dirt(x, y-1, z+1)
		dirt(x+1, y-1, z+1)
	default:
		dirt(x, y-1, z)
	}
	switch c.Trunk {
	case TrunkStraight:
		for i := 0; i < h; i++ {
			log(x, y+i, z)
		}
		return []foliageAttachment{{x, y + h, z, 0, false}}

	case TrunkForking:
		return c.forkingTrunk(rng, x, y, z, h, log)

	case TrunkDarkOak:
		return c.darkOakTrunk(rng, x, y, z, h, log, free)

	case TrunkGiant, TrunkMegaJungle:
		atts := giantTrunk(x, y, z, h, log)
		if c.Trunk == TrunkMegaJungle {
			atts = append(atts, megaJungleBranches(rng, x, y, z, h, log)...)
		}
		return atts

	case TrunkBending:
		return c.bendingTrunk(rng, x, y, z, h, log, free)

	case TrunkUpwardsBranching:
		return c.branchingTrunk(rng, x, y, z, h, log)

	case TrunkCherry:
		return c.cherryTrunk(rng, x, y, z, h, log, logAxis)

	case TrunkFancy:
		return c.fancyTrunk(rng, x, y, z, h, set, free)
	}
	return nil
}

// horizontal directions, in vanilla's Direction.Plane.HORIZONTAL order
// (north, south, west, east) — getRandomDirection indexes this list.
var horiz = [4][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}}

func randHoriz(rng TreeRNG) (int, int) { d := horiz[rng.Intn(4)]; return d[0], d[1] }

// forkingTrunk is acacia: a leaning trunk plus a second limb the other way.
func (c *TreeConfig) forkingTrunk(rng TreeRNG, x, y, z, h int, log func(int, int, int) bool) []foliageAttachment {
	var atts []foliageAttachment
	ldx, ldz := randHoriz(rng)
	leanHeight := h - rng.Intn(4) - 1
	leanSteps := 3 - rng.Intn(3)
	tx, tz := x, z
	topY, has := 0, false
	for yo := 0; yo < h; yo++ {
		if yo >= leanHeight && leanSteps > 0 {
			tx += ldx
			tz += ldz
			leanSteps--
		}
		if log(tx, y+yo, tz) {
			topY, has = y+yo+1, true
		}
	}
	if has {
		atts = append(atts, foliageAttachment{tx, topY, tz, 1, false})
	}
	tx, tz = x, z
	bdx, bdz := randHoriz(rng)
	if bdx != ldx || bdz != ldz {
		start := leanHeight - rng.Intn(2) - 1
		steps := 1 + rng.Intn(3)
		topY, has = 0, false
		for yo := start; yo < h && steps > 0; steps-- {
			if yo >= 1 {
				tx += bdx
				tz += bdz
				if log(tx, y+yo, tz) {
					topY, has = y+yo+1, true
				}
			}
			yo++
		}
		if has {
			atts = append(atts, foliageAttachment{tx, topY, tz, 0, false})
		}
	}
	return atts
}

// darkOakTrunk is the 2x2 leaning trunk with its ring of short branches, shared
// by dark oak and pale oak.
func (c *TreeConfig) darkOakTrunk(rng TreeRNG, x, y, z, h int, log func(int, int, int) bool, free TreeFree) []foliageAttachment {
	var atts []foliageAttachment
	ldx, ldz := randHoriz(rng)
	leanHeight := h - rng.Intn(4)
	leanSteps := 2 - rng.Intn(3)
	tx, tz := x, z
	ey := y + h - 1
	for dy := 0; dy < h; dy++ {
		if dy >= leanHeight && leanSteps > 0 {
			tx += ldx
			tz += ldz
			leanSteps--
		}
		yy := y + dy
		if free(tx, yy, tz) {
			log(tx, yy, tz)
			log(tx+1, yy, tz)
			log(tx, yy, tz+1)
			log(tx+1, yy, tz+1)
		}
	}
	atts = append(atts, foliageAttachment{tx, ey, tz, 0, true})
	for ox := -1; ox <= 2; ox++ {
		for oz := -1; oz <= 2; oz++ {
			if (ox < 0 || ox > 1 || oz < 0 || oz > 1) && rng.Intn(3) <= 0 {
				length := rng.Intn(3) + 2
				for by := 0; by < length; by++ {
					log(x+ox, ey-by-1, z+oz)
				}
				atts = append(atts, foliageAttachment{x + ox, ey, z + oz, 0, false})
			}
		}
	}
	return atts
}

// giantTrunk is the mega spruce/pine 2x2 column; the three extra cells stop one
// block short of the top.
func giantTrunk(x, y, z, h int, log func(int, int, int) bool) []foliageAttachment {
	for hh := 0; hh < h; hh++ {
		log(x, y+hh, z)
		if hh < h-1 {
			log(x+1, y+hh, z)
			log(x+1, y+hh, z+1)
			log(x, y+hh, z+1)
		}
	}
	return []foliageAttachment{{x, y + h, z, 0, true}}
}

// megaJungleBranches are the diagonal limbs the jungle giant throws out.
func megaJungleBranches(rng TreeRNG, x, y, z, h int, log func(int, int, int) bool) []foliageAttachment {
	var atts []foliageAttachment
	for bh := h - 2 - rng.Intn(4); bh > h/2; bh -= 2 + rng.Intn(4) {
		angle := rng.Float64() * 2 * math.Pi
		bx, bz := 0, 0
		for b := 0; b < 5; b++ {
			bx = int(1.5 + math.Cos(angle)*float64(b))
			bz = int(1.5 + math.Sin(angle)*float64(b))
			log(x+bx, y+bh-3+b/2, z+bz)
		}
		atts = append(atts, foliageAttachment{x + bx, y + bh, z + bz, -2, false})
	}
	return atts
}

// bendingTrunk is the azalea: it grows up, then leans over and keeps going.
func (c *TreeConfig) bendingTrunk(rng TreeRNG, x, y, z, h int, log func(int, int, int) bool, free TreeFree) []foliageAttachment {
	var atts []foliageAttachment
	dx, dz := randHoriz(rng)
	logHeight := h - 1
	px, py, pz := x, y, z
	for i := 0; i <= logHeight; i++ {
		if i+1 >= logHeight+rng.Intn(2) {
			px += dx
			pz += dz
		}
		if free(px, py, pz) {
			log(px, py, pz)
		}
		if i >= c.MinHeightForLeaves {
			atts = append(atts, foliageAttachment{px, py, pz, 0, false})
		}
		py++
	}
	bend := sampleInt(rng, c.BendLengthMin, c.BendLengthMax)
	for i := 0; i <= bend; i++ {
		if free(px, py, pz) {
			log(px, py, pz)
		}
		atts = append(atts, foliageAttachment{px, py, pz, 0, false})
		px += dx
		pz += dz
	}
	return atts
}

// branchingTrunk is the mangrove: every log may throw a sideways branch.
func (c *TreeConfig) branchingTrunk(rng TreeRNG, x, y, z, h int, log func(int, int, int) bool) []foliageAttachment {
	var atts []foliageAttachment
	for hp := 0; hp < h; hp++ {
		cy := y + hp
		placed := log(x, cy, z)
		if placed && hp < h-1 && rng.Float64() < c.BranchProbability {
			bdx, bdz := randHoriz(rng)
			blen := sampleInt(rng, c.ExtraBranchLenMin, c.ExtraBranchLenMax)
			bpos := blen - sampleInt(rng, c.ExtraBranchLenMin, c.ExtraBranchLenMax) - 1
			if bpos < 0 {
				bpos = 0
			}
			steps := sampleInt(rng, c.ExtraBranchStepsMin, c.ExtraBranchStepsMax)
			atts = append(atts, c.mangroveBranch(rng, h, log, x, z, cy, bdx, bdz, bpos, steps)...)
		}
		if hp == h-1 {
			atts = append(atts, foliageAttachment{x, cy + 1, z, 0, false})
		}
	}
	return atts
}

func (c *TreeConfig) mangroveBranch(rng TreeRNG, h int, log func(int, int, int) bool,
	x, z, currentHeight, bdx, bdz, bpos, steps int) []foliageAttachment {
	var atts []foliageAttachment
	along := currentHeight + bpos
	lx, lz := x, z
	idx := bpos
	for idx < h && steps > 0 {
		if idx >= 1 {
			ph := currentHeight + idx
			lx += bdx
			lz += bdz
			along = ph
			if log(lx, ph, lz) {
				along = ph + 1
			}
			atts = append(atts, foliageAttachment{lx, ph, lz, 0, false})
		}
		idx++
		steps--
	}
	if along-currentHeight > 1 {
		atts = append(atts,
			foliageAttachment{lx, along, lz, 0, false},
			foliageAttachment{lx, along - 2, lz, 0, false})
	}
	return atts
}

// cherryTrunk grows one to three branches that arc out and up, with the logs
// along them laid on their side.
func (c *TreeConfig) cherryTrunk(rng TreeRNG, x, y, z, h int, log func(int, int, int) bool,
	logAxis func(int, int, int, int)) []foliageAttachment {
	first := h - 1 + sampleInt(rng, c.BranchStartMin, c.BranchStartMax)
	if first < 0 {
		first = 0
	}
	second := h - 1 + sampleInt(rng, c.BranchStartMin, c.BranchStartMax-1)
	if second < 0 {
		second = 0
	}
	if second >= first {
		second++
	}
	count := sampleInt(rng, c.BranchCountMin, c.BranchCountMax)
	middle := count == 3
	both := count >= 2

	trunkHeight := first + 1
	switch {
	case middle:
		trunkHeight = h
	case both:
		trunkHeight = max(first, second) + 1
	}
	for i := 0; i < trunkHeight; i++ {
		log(x, y+i, z)
	}
	var atts []foliageAttachment
	if middle {
		atts = append(atts, foliageAttachment{x, y + trunkHeight, z, 0, false})
	}
	ddx, ddz := randHoriz(rng)
	axis := 0 // X
	if ddz != 0 {
		axis = 2 // Z
	}
	atts = append(atts, c.cherryBranch(rng, x, y, z, h, ddx, ddz, axis, first, first < trunkHeight-1, log, logAxis))
	if both {
		atts = append(atts, c.cherryBranch(rng, x, y, z, h, -ddx, -ddz, axis, second, second < trunkHeight-1, log, logAxis))
	}
	return atts
}

func (c *TreeConfig) cherryBranch(rng TreeRNG, x, y, z, h, ddx, ddz, axis, offset int, continuesUp bool,
	log func(int, int, int) bool, logAxis func(int, int, int, int)) foliageAttachment {
	lx, ly, lz := x, y+offset, z
	endOffset := h - 1 + sampleInt(rng, c.BranchEndMin, c.BranchEndMax)
	extend := continuesUp || endOffset < offset
	dist := sampleInt(rng, c.BranchHorizMin, c.BranchHorizMax)
	if extend {
		dist++
	}
	ex, ey, ez := x+ddx*dist, y+endOffset, z+ddz*dist
	steps := 1
	if extend {
		steps = 2
	}
	for i := 0; i < steps; i++ {
		lx += ddx
		lz += ddz
		logAxis(lx, ly, lz, axis)
	}
	up := 1
	if ey <= ly {
		up = -1
	}
	for {
		d := abs(lx-ex) + abs(ly-ey) + abs(lz-ez)
		if d == 0 {
			return foliageAttachment{ex, ey + 1, ez, 0, false}
		}
		chance := math.Abs(float64(ey-ly)) / float64(d)
		if rng.Float64() < chance {
			ly += up
			log(lx, ly, lz)
		} else {
			lx += ddx
			lz += ddz
			logAxis(lx, ly, lz, axis)
		}
	}
}
