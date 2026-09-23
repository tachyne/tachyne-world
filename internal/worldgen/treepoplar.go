package worldgen

// The poplar (26.3): a tall straight trunk with a ring of short sideways
// branches under the canopy, and a tall rhombus canopy whose widest rows are
// braced by log spokes. Ports of PoplarTrunkPlacer and PoplarFoliagePlacer.

// weightedInt samples a weighted list of ints (WeightedListInt of constants):
// one draw over the total weight, walked in list order.
func weightedInt(rng TreeRNG, vals, weights []int) int {
	total := 0
	for _, w := range weights {
		total += w
	}
	i := rng.Intn(total)
	for k, w := range weights {
		if i -= w; i < 0 {
			return vals[k]
		}
	}
	return vals[len(vals)-1]
}

// directions6 is Direction.values() order: down, up, north, south, west,
// east — the order Direction.allShuffled shuffles.
var directions6 = [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// shuffledHorizontals is getShuffledBranchDirections: all six directions
// shuffled (Util.shuffle's draws), the vertical two dropped.
func shuffledHorizontals(rng TreeRNG) [][3]int {
	all := make([][3]int, 6)
	copy(all, directions6[:])
	for i := len(all) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		all[i], all[j] = all[j], all[i]
	}
	out := all[:0]
	for _, d := range all {
		if d[1] == 0 {
			out = append(out, d)
		}
	}
	return out
}

// poplarTrunk: logs straight up; at the branch layer (trunk_height_above_
// branches below the top) one to four one-block branches, laid sideways. The
// direction shuffle is drawn on EVERY layer, branch or not, as vanilla does.
func (c *TreeConfig) poplarTrunk(rng TreeRNG, x, y, z, h int, log func(int, int, int) bool,
	logAxis func(int, int, int, int)) []foliageAttachment {
	upTo := h - sampleInt(rng, c.AboveBranchesMin, c.AboveBranchesMax)
	for i := 0; i < h; i++ {
		log(x, y+i, z)
		dirs := shuffledHorizontals(rng)
		if upTo-1 == i {
			n := sampleInt(rng, c.BranchCountMin, c.BranchCountMax)
			for k := 0; k < n; k++ {
				d := dirs[k]
				axis := 0 // X
				if d[2] != 0 {
					axis = 2 // Z
				}
				logAxis(x+d[0], y+i, z+d[2], axis)
			}
		}
	}
	return []foliageAttachment{{x, y + upTo, z, 0, false}}
}

// poplarFoliage is PoplarFoliagePlacer.createFoliage: rows of a rhombus that
// narrows at the top two rows (cut to a partial rhombus) and at the bottom,
// one diagonal pair of corners cut deeper than the other (flip picks which),
// random holes on the sides, and log spokes bracing the widest row.
func (c *TreeConfig) poplarFoliage(rng TreeRNG, a foliageAttachment, foliageHeight, leafRadius, offset int,
	set TreeSetter, ownLeaf func(x, y, z int) bool) {
	ox, oy, oz := a.x, a.y+offset, a.z
	r := leafRadius + a.radiusOffset - 1
	flip := rng.Intn(2) == 1
	fh := foliageHeight // the attachment's foliage height offset is 0 for poplar
	partial := func(y int) bool { return fh-1 == y || fh-2 == y }
	corner := func(dx, dz, radius int, part bool) int {
		small := (dx > 0 && dz < 0) || (dz > 0 && dx < 0)
		if flip {
			small = (dx > 0 && dz > 0) || (dz < 0 && dx < 0)
		}
		switch {
		case small:
			return radius - 1
		case part:
			return radius + 1
		}
		return radius
	}
	within := func(radius, adx, adz, cut, extra int) bool {
		return adx+adz <= radius*2-(cut+extra)
	}
	off := 0
	if a.doubleTrunk {
		off = 1
	}
	row := func(radius, y int) {
		for dx := -radius; dx <= radius+off; dx++ {
			for dz := -radius; dz <= radius+off; dz++ {
				part := partial(y)
				cut := corner(dx, dz, radius, part)
				adx, adz := abs(dx), abs(dz)
				if part && (adx == radius || adz == radius) {
					continue
				}
				extra := 0
				if rng.Float64() <= c.SideHoleChance {
					extra = 1
				}
				if !within(radius, adx, adz, cut, extra) {
					continue
				}
				set(ox+dx, oy+y, oz+dz, c.leafState(rng), true)
			}
		}
	}
	row(r-2, fh-1)
	row(r-1, fh-2)
	row(r-1, fh-3)
	for y := fh - 4; y >= 1; y-- {
		row(r, y)
	}
	// Log spokes along the axes of the row at fh-4, where the canopy is at
	// least four wide from the trunk; each replaces only this tree's leaf.
	sy := fh - 4
	for dx := -r; dx <= r+off; dx++ {
		for dz := -r; dz <= r+off; dz++ {
			adx, adz := abs(dx), abs(dz)
			if !within(r, adx, adz, corner(dx, dz, r, partial(sy)), 2) {
				continue
			}
			if !((adz == 0 && r-adx >= 4) || (adx == 0 && r-adz >= 4)) {
				continue
			}
			px, py, pz := ox+dx, oy+sy, oz+dz
			if !ownLeaf(px, py, pz) {
				continue
			}
			axis := 2 // Z
			if adz == 0 {
				axis = 0 // X
			}
			set(px, py, pz, axisLog(c.Log, axis), false)
		}
	}
	row(r-1, 0)
	bottom := r - 2
	if bottom < 1 {
		bottom = 1
	}
	if bottom > 2 {
		bottom = 2
	}
	row(bottom, -1)
}

// shelfFacing is Direction.Plane.HORIZONTAL order (north, east, south, west)
// with each direction's step; clockwise is the next entry.
var shelfFacing = [4]struct {
	name   string
	dx, dz int
}{{"north", 0, -1}, {"east", 1, 0}, {"south", 0, 1}, {"west", -1, 0}}

func isShelfMushroom(s uint32) bool {
	lo, hi, ok := BlockRangeOK("shelf_mushroom")
	return ok && s >= lo && s <= hi
}

// shelfMushrooms is ShelfMushroomDecorator: one roll gates the tree. On a
// standing tree, each trunk position one to four above the lowest may take a
// bracket on one of two perpendicular sides (a quarter chance each, the first
// that takes ending that log's turn), never directly over another. On a
// fallen log, each log may take one on either flank, never beside another.
// A bracket needs a replaceable cell with no water in or beside it.
func shelfMushrooms(ctx *decoCtx, prob float64) {
	rng := ctx.rng
	if rng.Float64() >= prob || len(ctx.logList) == 0 {
		return
	}
	logs := ctx.logList
	fits := func(q [3]int) bool {
		if !ctx.isAir(q) {
			return false
		}
		for _, o := range [5][2]int{{0, 0}, {1, 0}, {-1, 0}, {0, -1}, {0, 1}} {
			if ctx.read(q[0]+o[0], q[1], q[2]+o[1]) == Water {
				return false
			}
		}
		return true
	}
	shelfAt := func(q [3]int) bool { return isShelfMushroom(ctx.read(q[0], q[1], q[2])) }
	nextToShelf := func(q [3]int) bool {
		for _, f := range shelfFacing {
			if shelfAt([3]int{q[0] + f.dx, q[1], q[2] + f.dz}) {
				return true
			}
		}
		return false
	}
	place := func(q [3]int, facing string) {
		age := rng.Intn(2)
		ctx.set(q[0], q[1], q[2], StateWith("shelf_mushroom",
			map[string]string{"facing": facing, "age": string(rune('0' + age))}), false)
	}
	if logs[0][1] == logs[len(logs)-1][1] { // fallen: along the log, both flanks
		sides := [2]int{0, 2} // north, south for a log along X
		if logs[0][0] == logs[len(logs)-1][0] {
			sides = [2]int{1, 3} // east, west for a log along Z
		}
		for _, p := range logs {
			for _, si := range sides {
				if rng.Float64() > 0.25 {
					continue
				}
				f := shelfFacing[si]
				q := [3]int{p[0] + f.dx, p[1], p[2] + f.dz}
				if fits(q) && !nextToShelf(q) && !nextToShelf(p) {
					place(q, f.name)
				}
			}
		}
		return
	}
	first := rng.Intn(4)
	dirs := [2]int{first, (first + 1) % 4}
	base := logs[0][1]
	for _, p := range logs {
		if dy := p[1] - base; dy < 1 || dy > 4 {
			continue
		}
		for _, di := range dirs {
			if rng.Float64() > 0.25 {
				continue
			}
			f := shelfFacing[di]
			q := [3]int{p[0] + f.dx, p[1], p[2] + f.dz}
			if !fits(q) || shelfAt([3]int{q[0], q[1] - 1, q[2]}) {
				continue
			}
			place(q, f.name)
			break
		}
	}
}
