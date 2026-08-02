package worldgen

// The mangrove root placer (MangroveRootPlacer): the trunk stands one to
// three blocks above the origin, a root column fills the gap, and a
// simulated walk fans roots out and down in each horizontal direction. Roots
// placed inside mud become muddy mangrove roots, roots in water waterlog,
// and an exposed root has a coin-flip chance of a moss carpet on top.
//
// The walk's recursion is gated by what the world says is passable, and its
// draws follow the recursion — so, as with the other decorators, the chunk
// stamper answers passability from the terrain model (TreeDriver.RootThrough)
// and the moss-carpet roll is PRE-DRAWN per placed position, keeping every
// pass over a chunk-straddling mangrove on one sequence.

// IsRootGrowThrough is the #mangrove_roots_can_grow_through tag.
func IsRootGrowThrough(state uint32) bool {
	for _, n := range rootGrowThrough {
		lo, hi := BlockRange(n)
		if state >= lo && state <= hi {
			return true
		}
	}
	return false
}

var rootGrowThrough = []string{
	"mud", "muddy_mangrove_roots", "mangrove_roots", "moss_carpet", "vine",
	"mangrove_propagule", "snow",
}

// placeRoots is MangroveRootPlacer.placeRoots. The column from the origin up
// to the raised trunk must be passable or the WHOLE TREE refuses; then the
// root under the trunk plus a walk outward in each horizontal direction, and
// every found position is placed. Returns false to refuse the tree.
func (c *TreeConfig) placeRoots(rng TreeRNG, x, y, z, ty int, d TreeDriver, roots map[[3]int]bool) bool {
	canRoot := func(px, py, pz int) bool {
		return d.Free(px, py, pz) || d.RootThrough(px, py, pz)
	}
	for cy := y; cy < ty; cy++ {
		if !canRoot(x, cy, z) {
			return false
		}
	}
	list := [][3]int{{x, ty - 1, z}}
	for _, dir := range horiz {
		start := [3]int{x + dir[0], ty, z + dir[1]}
		var inDir [][3]int
		if !c.simulateRoots(rng, canRoot, start, dir, [3]int{x, ty, z}, &inDir, 0) {
			return false
		}
		list = append(list, inDir...)
		list = append(list, start)
	}
	for _, p := range list {
		c.placeRoot(rng, canRoot, p, d, roots)
	}
	return true
}

// simulateRoots walks one direction's root, collecting positions; a walk that
// exhausts its length budget refuses the tree, exactly as vanilla's does.
func (c *TreeConfig) simulateRoots(rng TreeRNG, canRoot func(int, int, int) bool,
	pos [3]int, dir [2]int, origin [3]int, out *[][3]int, layer int) bool {
	if layer == c.RootMaxLength || len(*out) > c.RootMaxLength {
		return false
	}
	for _, q := range c.potentialRootPositions(rng, pos, dir, origin) {
		if canRoot(q[0], q[1], q[2]) {
			*out = append(*out, q)
			if !c.simulateRoots(rng, canRoot, q, dir, origin, out, layer+1) {
				return false
			}
		}
	}
	return true
}

// potentialRootPositions is the walk's step rule: near the width limit the
// root drops (sometimes skewing out one more), past it it only drops, and
// inside it a coin decides between stepping outward and dropping.
func (c *TreeConfig) potentialRootPositions(rng TreeRNG, pos [3]int, dir [2]int, origin [3]int) [][3]int {
	below := [3]int{pos[0], pos[1] - 1, pos[2]}
	nextTo := [3]int{pos[0] + dir[0], pos[1], pos[2] + dir[1]}
	width := abs(pos[0]-origin[0]) + abs(pos[1]-origin[1]) + abs(pos[2]-origin[2])
	switch {
	case width > c.RootMaxWidth-3 && width <= c.RootMaxWidth:
		if rng.Float64() < c.RootSkewChance {
			return [][3]int{below, {nextTo[0], nextTo[1] - 1, nextTo[2]}}
		}
		return [][3]int{below}
	case width > c.RootMaxWidth:
		return [][3]int{below}
	case rng.Float64() < c.RootSkewChance:
		return [][3]int{below}
	default:
		if rng.Intn(2) == 0 {
			return [][3]int{nextTo}
		}
		return [][3]int{below}
	}
}

// placeRoot writes one root position: muddy inside mud, waterlogged in
// water, and — on the exposed branch only — a pre-drawn coin for a moss
// carpet on top. The roll is drawn for EVERY position so the sequence never
// depends on what the reads classified (see the file-head note).
func (c *TreeConfig) placeRoot(rng TreeRNG, canRoot func(int, int, int) bool, p [3]int, d TreeDriver, roots map[[3]int]bool) {
	carpetRoll := rng.Float64()
	cur := d.Read(p[0], p[1], p[2])
	if cur == Mud || (cur >= c.RootMuddyState-1 && cur <= c.RootMuddyState+1) {
		d.Set(p[0], p[1], p[2], c.RootMuddyState, false)
		roots[p] = true
		return
	}
	if !canRoot(p[0], p[1], p[2]) {
		return
	}
	st := c.RootState
	if IsWater(cur) {
		st-- // waterlogged=true is the state before the dry one
	}
	d.Set(p[0], p[1], p[2], st, false)
	roots[p] = true
	if c.AboveRootChance > 0 && carpetRoll < c.AboveRootChance {
		above := [3]int{p[0], p[1] + 1, p[2]}
		if d.Free(above[0], above[1], above[2]) && !roots[above] {
			d.Set(above[0], above[1], above[2], c.AboveRootState, true)
			roots[above] = true
		}
	}
}

// IsMangroveRoots is the mangrove root family — exempt from the cave-debris
// sweep for the same reason logs are: a stilted root is not floating rubble.
func IsMangroveRoots(state uint32) bool {
	lo, _ := BlockRange("mangrove_roots")
	_, hi := BlockRange("muddy_mangrove_roots")
	return state >= lo && state <= hi
}
