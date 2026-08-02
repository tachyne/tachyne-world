package worldgen

// Fallen trees (FallenTreeFeature): a stump where the tree once stood, and a
// sideways log run a couple of blocks away — mushrooms sprouting on top,
// trunk vines on the stumps of the species that carry them. The log lies on
// the ground: its whole run must be free with at most two consecutive cells
// of missing floor beneath it, or only the stump is left.
//
// The per-log mushroom rolls are PRE-DRAWN (the usual discipline): the run's
// validity comes from world reads, and a read must never bend the sequence a
// chunk-straddling stamp draws.

// FallenTree is one fallen-log feature's numbers, generated from the JSONs.
type FallenTree struct {
	Log            uint32 // the species log's BASE state (axis handled here)
	LenMin, LenMax int
	StumpVine      bool
	MushProb       float64
}

// PlaceFallenTree places one fallen tree with its stump at (x,y,z).
func PlaceFallenTree(c *FallenTree, x, y, z int, rng TreeRNG, d TreeDriver) {
	// The stump goes in unconditionally, as vanilla's does, plus its
	// trunk-vine dressing (two in three per side, into free cells).
	d.Set(x, y, z, axisLog(c.Log, 1), false)
	if c.StumpVine {
		vineBase := blockBase("vine")
		for _, sd := range [4]struct {
			dx, dz int
			face   uint32
		}{{-1, 0, vineEast}, {1, 0, vineWest}, {0, -1, vineSouth}, {0, 1, vineNorth}} {
			if rng.Intn(3) > 0 && d.Free(x+sd.dx, y, z+sd.dz) {
				d.Set(x+sd.dx, y, z+sd.dz, vineBase+sd.face, true)
			}
		}
	}
	dir := horiz[rng.Intn(4)]
	length := sampleInt(rng, c.LenMin, c.LenMax) - 2
	off := 2 + rng.Intn(2)
	sx, sz := x+dir[0]*off, z+dir[1]*off
	// The ground probe: one step up, then down at most six, to the first
	// cell that is free and stands over solid ground.
	sy := y + 1
	found := false
	for i := 0; i < 6; i++ {
		if d.Free(sx, sy, sz) && !d.Free(sx, sy-1, sz) {
			found = true
			break
		}
		sy--
	}
	// Every roll the run could need, drawn now.
	type roll struct {
		mush float64
		pick int
	}
	rolls := make([]roll, max(length, 0))
	for i := range rolls {
		rolls[i] = roll{rng.Float64(), rng.Intn(3)}
	}
	if !found || length <= 0 {
		return
	}
	gap := 0
	for i := 0; i < length; i++ {
		cx, cz := sx+dir[0]*i, sz+dir[1]*i
		if !d.Free(cx, sy, cz) {
			return
		}
		if d.Free(cx, sy-1, cz) {
			if gap++; gap > 2 {
				return
			}
		} else {
			gap = 0
		}
	}
	axis := 0 // the log lies along x…
	if dir[0] == 0 {
		axis = 2 // …or along z
	}
	mushRed := blockBase("red_mushroom")
	mushBrown := blockBase("brown_mushroom")
	for i := 0; i < length; i++ {
		cx, cz := sx+dir[0]*i, sz+dir[1]*i
		d.Set(cx, sy, cz, axisLog(c.Log, axis), false)
		// attached_to_logs, upward: red twice as often as brown.
		if r := rolls[i]; r.mush <= c.MushProb && d.Free(cx, sy+1, cz) {
			m := mushRed
			if r.pick == 2 {
				m = mushBrown
			}
			d.Set(cx, sy+1, cz, m, true)
		}
	}
}
