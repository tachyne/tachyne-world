package worldgen

// The pale-moss decorator (PaleMossDecorator) and the pale_moss_patch
// vegetation feature it triggers (VegetationPatchFeature with the
// PALE_MOSS_PATCH configuration): a rounded patch of pale moss laid into the
// ground at the tree's foot with carpet and grasses scattered on top, and
// strands of pale hanging moss trailing from the trunk and canopy.
//
// DETERMINISM NOTE. Unlike the other decorators, this one's vanilla shape
// draws a variable NUMBER of rolls depending on what the world says: edge
// columns roll only on edges, vegetation rolls only on placed surface, a
// strand rolls per step only while the space below is free. A chunk-straddling
// tree is drawn once per overlapping chunk, and reads near the border can
// answer differently per pass, so a read-dependent draw COUNT would
// desynchronise the passes and split one tree into two disagreeing halves.
// The port therefore PRE-DRAWS every roll in a fixed order and lets the world
// reads gate only the writes. The placed outcome carries vanilla's
// distribution exactly — an unused roll is independent of every used one —
// and the draw sequence is fixed by construction.

// mossReplaceable is the #moss_replaceable block tag:
// #base_stone_overworld + #cave_vines + #dirt.
func mossReplaceable(state uint32) bool {
	if IsDirtTag(state) {
		return true
	}
	for _, n := range mossReplaceableExtra {
		lo, hi := BlockRange(n)
		if state >= lo && state <= hi {
			return true
		}
	}
	return false
}

var mossReplaceableExtra = []string{
	"stone", "granite", "diorite", "andesite", "tuff", "deepslate",
	"cave_vines", "cave_vines_plant",
}

// paleMoss is PaleMossDecorator.place: the ground patch at the lowest log,
// then a strand roll per log, then per leaf — in that order.
func (c *TreeConfig) paleMoss(ctx *decoCtx) {
	rng := ctx.rng
	if len(ctx.logList) == 0 {
		return
	}
	if rng.Float64() < c.PaleMossGround {
		low := ctx.logList[0] // stably Y-sorted: the first lowest log
		paleMossPatch(ctx, low[0], low[1]+1, low[2])
	}
	for _, p := range ctx.logList {
		mossHanger(ctx, p, rng.Float64() < c.PaleMossTrunk)
	}
	for _, p := range ctx.leafList {
		mossHanger(ctx, p, rng.Float64() < c.PaleMossLeaves)
	}
}

// mossHanger grows a strand of pale hanging moss downward from below the
// block: stem cells (tip=false) while the length and the free space allow,
// then the tip. The length is vanilla's coin-per-step geometric draw, taken
// up front (see the determinism note above).
func mossHanger(ctx *decoCtx, p [3]int, gate bool) {
	length := 0
	for ctx.rng.Float64() >= 0.5 {
		length++
	}
	if !gate {
		return
	}
	q := [3]int{p[0], p[1] - 1, p[2]}
	if !ctx.isAir(q) {
		return
	}
	moss := blockBase("pale_hanging_moss") // tip=true; +1 is the stem
	for i := 0; i < length && ctx.isAir([3]int{q[0], q[1] - 1, q[2]}); i++ {
		ctx.set(q[0], q[1], q[2], moss+1, true)
		q[1]--
	}
	ctx.set(q[0], q[1], q[2], moss, true)
}

// paleMossPatch is the pale_moss_patch vegetation feature. Config baked from
// the 1.21.11 JSON: xz radius uniform 2-4 (+1 in code), corners cut, edge
// columns kept at 0.75, floor surface probed five steps down then up, depth
// one block of pale moss into #moss_replaceable ground, vegetation at 0.3 —
// weighted carpet 25 / short grass 25 / tall grass 10.
func paleMossPatch(ctx *decoCtx, ox, oy, oz int) {
	rng := ctx.rng
	xR := sampleInt(rng, 2, 4) + 1
	zR := sampleInt(rng, 2, 4) + 1
	mossBlock := blockBase("pale_moss_block")
	for dx := -xR; dx <= xR; dx++ {
		xEdge := dx == -xR || dx == xR
		for dz := -zR; dz <= zR; dz++ {
			zEdge := dz == -zR || dz == zR
			// Every roll this column could need, drawn unconditionally in
			// scan order (the determinism note).
			edgeRoll := rng.Float64()
			vegRoll := rng.Float64()
			vegPick := rng.Intn(60)
			var flips [4]bool
			for i := range flips {
				flips[i] = rng.Intn(2) == 0
			}
			if xEdge && zEdge {
				continue // corners are cut
			}
			if (xEdge || zEdge) && edgeRoll > 0.75 {
				continue
			}
			// The floor probe: down through air, then back up out of the
			// ground, five steps each way at most.
			x, z := ox+dx, oz+dz
			y := oy
			for i := 0; i < 5 && ctx.isAir([3]int{x, y, z}); i++ {
				y--
			}
			for i := 0; i < 5 && !ctx.isAir([3]int{x, y, z}); i++ {
				y++
			}
			if !ctx.isAir([3]int{x, y, z}) {
				continue
			}
			ground := ctx.read(x, y-1, z)
			if !IsSolidFull(ground) {
				continue
			}
			if ground != mossBlock {
				if !mossReplaceable(ground) {
					continue
				}
				ctx.set(x, y-1, z, mossBlock, false)
			}
			if vegRoll < 0.3 {
				paleMossVegetation(ctx, x, y, z, vegPick, flips)
			}
		}
	}
}

// paleMossVegetation is one draw of the pale_moss_vegetation provider on a
// freshly mossed surface cell: carpet 25, short grass 25, tall grass 10.
func paleMossVegetation(ctx *decoCtx, x, y, z int, pick int, flips [4]bool) {
	switch {
	case pick < 25:
		paleCarpetAt(ctx, x, y, z, flips)
	case pick < 50:
		ctx.set(x, y, z, blockBase("short_grass"), true)
	default:
		// Tall grass needs its upper half free (DoublePlantBlock.placeAt).
		if ctx.isAir([3]int{x, y + 1, z}) {
			tall := blockBase("tall_grass")
			ctx.set(x, y, z, tall+1, true) // half=lower
			ctx.set(x, y+1, z, tall, true) // half=upper
		}
	}
}

// paleCarpetAt is MossyCarpetBlock.placeAt: the base carpet layer with a low
// wall side wherever a sturdy face adjoins, and a chance of a second "topper"
// layer whose sides each survive a coin flip — the moss creeping up the pale
// oak's trunk. A topper side turns the base's side tall beneath it.
//
// Walls are the tree's OWN logs or solid world blocks; near a chunk border
// the stamper answers the latter from its terrain guess, which can miss a
// neighbouring structure's wall — a side bit at worst, never a divergence,
// since the flips are pre-drawn and each cell is written by exactly one pass.
func paleCarpetAt(ctx *decoCtx, x, y, z int, flips [4]bool) {
	carpet := blockBase("pale_moss_carpet")
	wall := func(qx, qy, qz int) bool {
		if ctx.logs[[3]int{qx, qy, qz}] {
			return true
		}
		return IsSolidFull(ctx.read(qx, qy, qz))
	}
	// State packing: bottom(true,false) x east x north x south x west, each
	// side none/low/tall — verified against the block report.
	pack := func(bottom bool, sides [4]int) uint32 {
		s := uint32(sides[0])*27 + uint32(sides[1])*9 + uint32(sides[2])*3 + uint32(sides[3])
		if !bottom {
			s += 81
		}
		return carpet + s
	}
	dirs := [4][2]int{{1, 0}, {0, -1}, {0, 1}, {-1, 0}} // east, north, south, west
	var base, topper [4]int
	anyTopper := false
	for i, d := range dirs {
		if !wall(x+d[0], y, z+d[1]) {
			continue
		}
		base[i] = 1 // low
		if wall(x+d[0], y+1, z+d[1]) && flips[i] {
			topper[i] = 1
			anyTopper = true
		}
	}
	if anyTopper && ctx.isAir([3]int{x, y + 1, z}) {
		ctx.set(x, y+1, z, pack(false, topper), true)
		for i := range base {
			if topper[i] == 1 {
				base[i] = 2 // tall beneath the topper's side
			}
		}
	}
	ctx.set(x, y, z, pack(true, base), true)
}
