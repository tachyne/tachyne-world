package worldgen

// Desert wells — the small sandstone well that pokes out of the sand, and the
// first thing in tachyne to bury SUSPICIOUS SAND. Ported from vanilla's
// DesertWellFeature: a 5x5x3 sandstone plug under the surface, a water cross on
// top of a sand cross, a low wall with a slab on each face, four corner pillars
// and a slab roof — and, in two of the five water cells, suspicious sand.
//
// Without it a brush had nothing to brush: the item and the blocks existed, but
// nothing in the world ever placed one.

const (
	desertWellCell = 512 // vanilla places these at rarity 1/1000 chunks in desert
)

var (
	SandstoneSlab  = blockBase("sandstone_slab") // default is bottom, unwaterlogged
	SuspiciousSand = blockBase("suspicious_sand")
)

// DesertWell is one well: Y is the sand cell the feature is founded on, the
// same block vanilla calls `origin`.
type DesertWell struct {
	X, Y, Z int
	Sus     [2][3]int // the two suspicious-sand cells, in world coordinates
	Exists  bool
}

// DesertWellIn returns the well owning (wx,wz)'s cell, if one landed on desert.
func (g *Generator) DesertWellIn(wx, wz int) DesertWell {
	ox, oz := cellOrigin(wx, desertWellCell), cellOrigin(wz, desertWellCell)
	x := ox + 16 + int(hash01(g.seed, ox, oz, 0xD001)*float64(desertWellCell-32))
	z := oz + 16 + int(hash01(g.seed, ox, oz, 0xD002)*float64(desertWellCell-32))
	if g.BiomeName(x, z) != "minecraft:desert" {
		return DesertWell{}
	}
	y := g.Height(x, z)
	if y < SeaLevel { // the feature refuses a site that is not dry sand
		return DesertWell{}
	}
	w := DesertWell{X: x, Y: y, Z: z, Exists: true}
	// Vanilla picks two of the five water cells (centre + the four sides) and
	// buries suspicious sand one and two blocks under them respectively. The
	// same cell can be picked twice, in which case both depths get one.
	cells := [5][2]int{{0, 0}, {1, 0}, {0, 1}, {-1, 0}, {0, -1}}
	a := cells[int(hash01(g.seed, x, z, 0xD003)*5)%5]
	b := cells[int(hash01(g.seed, x, z, 0xD004)*5)%5]
	w.Sus[0] = [3]int{x + a[0], y - 1, z + a[1]}
	w.Sus[1] = [3]int{x + b[0], y - 2, z + b[1]}
	return w
}

// stampDesertWell builds the well overlapping this chunk.
func (g *Generator) stampDesertWell(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(desertWellCell) {
		w := g.DesertWellIn(baseX+8+off[0], baseZ+8+off[1])
		if !w.Exists {
			continue
		}
		put := func(dx, dy, dz int, state uint32) {
			lx, lz := w.X+dx-baseX, w.Z+dz-baseZ
			setSectionBlock(ch, lx, w.Y+dy, lz, state, true)
		}
		// The sandstone plug the whole thing sits in: 5x5, three deep.
		for dy := -2; dy <= 0; dy++ {
			for dx := -2; dx <= 2; dx++ {
				for dz := -2; dz <= 2; dz++ {
					put(dx, dy, dz, Sandstone)
				}
			}
		}
		// Water cross at the surface, on a sand cross one below.
		cross := [5][2]int{{0, 0}, {1, 0}, {0, 1}, {-1, 0}, {0, -1}}
		for _, c := range cross {
			put(c[0], 0, c[1], Water)
			put(c[0], -1, c[1], Sand)
		}
		// The two buried caches. Placed AFTER the sand so they survive it.
		for _, s := range w.Sus {
			put(s[0]-w.X, s[1]-w.Y, s[2]-w.Z, SuspiciousSand)
		}
		// A low wall one above the rim, with a slab centred on each face.
		for dx := -2; dx <= 2; dx++ {
			for dz := -2; dz <= 2; dz++ {
				if dx == -2 || dx == 2 || dz == -2 || dz == 2 {
					put(dx, 1, dz, Sandstone)
				}
			}
		}
		for _, c := range [4][2]int{{2, 0}, {-2, 0}, {0, 2}, {0, -2}} {
			put(c[0], 1, c[1], SandstoneSlab)
		}
		// Four corner pillars up to the roof…
		for dy := 1; dy <= 3; dy++ {
			put(-1, dy, -1, Sandstone)
			put(-1, dy, 1, Sandstone)
			put(1, dy, -1, Sandstone)
			put(1, dy, 1, Sandstone)
		}
		// …and the 3x3 slab roof with a solid block at its centre.
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					put(dx, 4, dz, Sandstone)
				} else {
					put(dx, 4, dz, SandstoneSlab)
				}
			}
		}
	}
}
