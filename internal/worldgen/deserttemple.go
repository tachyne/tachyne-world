package worldgen

import "sync"

// Desert temple: a port of vanilla's DesertPyramidPiece — the 21×21 stepped
// sandstone pyramid with its two corner towers, the orange/blue terracotta
// motifs, the pillared hall, the TNT-trapped treasure well under the centre
// with its four chests, and (since 1.20) the sand-filled cellar under the
// east side reached by a broken stair, where five to seven of the cellar's
// sand blocks are suspicious and hold the desert_pyramid archaeology table.
// The piece faces a random direction, the way vanilla's does, and settles on
// the lowest ground under its footprint, up to two blocks lower.

const templeCell = 336 // one temple per ~336-block cell (when it lands on desert)

const (
	templeWidth = 21
	templeDepth = 21
)

var (
	TNTBlock           = blockBase("tnt")                      // minecraft:tnt (unstable=false)
	StonePressurePlate = blockBase("stone_pressure_plate") + 1 // stone_pressure_plate (unpowered)
	BlueTerracotta     = blockBase("blue_terracotta")
	ChiseledSandstone  = blockBase("chiseled_sandstone")
	CutSandstone       = blockBase("cut_sandstone")
)

// DesertTemple is a placed temple (or the zero value): the piece's min
// corner, its facing (0 north, 1 south, 2 west, 3 east) and the cellar's
// suspicious-sand cells.
type DesertTemple struct {
	X, Y, Z int // bounding-box min corner; Y is the piece's local y=0 row
	Dir     int
	Exists  bool
	Sus     [][3]int // suspicious sand, world coordinates
}

// Chests returns the four loot-chest positions round the treasure well.
func (d DesertTemple) Chests() [4][3]int {
	var out [4][3]int
	for i, off := range [4][2]int{{0, -2}, {0, 2}, {-2, 0}, {2, 0}} { // N S W E (Direction.Plane.HORIZONTAL order)
		x, y, z := d.world(10+off[0], -11, 10+off[1])
		out[i] = [3]int{x, y, z}
	}
	return out
}

// world maps a piece-local cell to the world (StructurePiece.getWorldX/Y/Z).
func (d DesertTemple) world(x, y, z int) (int, int, int) {
	maxX, maxZ := d.X+templeWidth-1, d.Z+templeDepth-1
	switch d.Dir {
	case 0: // north
		return d.X + x, d.Y + y, maxZ - z
	case 1: // south
		return d.X + x, d.Y + y, d.Z + z
	case 2: // west
		return maxX - z, d.Y + y, d.Z + x
	}
	return d.X + z, d.Y + y, d.Z + x // east
}

type dtKey struct {
	seed int64
	x, z int
}

var (
	dtCache = map[dtKey]DesertTemple{}
	dtMu    sync.Mutex
)

// DesertTempleIn returns the temple whose cell contains (wx,wz), if the roll
// succeeds and the site is dry desert.
func (g *Generator) DesertTempleIn(wx, wz int) DesertTemple {
	if g.nether || g.end {
		return DesertTemple{}
	}
	ox, oz := cellOrigin(wx, templeCell), cellOrigin(wz, templeCell)
	if hash01(g.seed, ox, oz, 0x7E01) >= 0.5 {
		return DesertTemple{}
	}
	x := ox + 24 + int(hash01(g.seed, ox, oz, 0x7E02)*float64(templeCell-48))
	z := oz + 24 + int(hash01(g.seed, ox, oz, 0x7E03)*float64(templeCell-48))
	if name := g.BiomeName(x+10, z+10); name != "minecraft:desert" {
		return DesertTemple{}
	}
	k := dtKey{g.seed, x, z}
	dtMu.Lock()
	d, ok := dtCache[k]
	dtMu.Unlock()
	if ok {
		return d
	}
	// updateHeightPositionToLowestGroundHeight: the lowest surface under the
	// footprint, then the piece sits 0–2 blocks lower still.
	lowest := 1 << 30
	for dx := 0; dx < templeWidth; dx++ {
		for dz := 0; dz < templeDepth; dz++ {
			if h := g.Height(x+dx, z+dz); h < lowest {
				lowest = h
			}
		}
	}
	if lowest < SeaLevel {
		return DesertTemple{} // SinglePieceStructure refuses a site below sea level
	}
	r := newJigsawRNG(g.seed, x^0x7E000000, z)
	d = DesertTemple{X: x, Z: z, Dir: r.intn(4), Exists: true}
	d.Y = lowest - r.intn(3)
	d.Sus = g.templeSuspiciousSand(d, r)
	dtMu.Lock()
	dtCache[k] = d
	dtMu.Unlock()
	return d
}

// templeSuspiciousSand is DesertPyramidStructure.afterPlace: of the cellar's
// potential sand cells (the 5×3×5 fill plus the eight cells round the
// terracotta cross) five to seven become suspicious, plus one cell of the
// collapsed roof; the rest stay plain sand.
func (g *Generator) templeSuspiciousSand(d DesertTemple, r *jigsawRNG) [][3]int {
	cells := templePotentialSand(d)
	type cand struct {
		pos [3]int
		h   float64
	}
	cands := make([]cand, 0, len(cells))
	for _, c := range cells {
		cands = append(cands, cand{c, hash01(g.seed, c[0], c[2]*8192+c[1], 0x7E10)})
	}
	n := 5 + r.intn(3)
	var out [][3]int
	for k := 0; k < n && len(cands) > 0; k++ {
		best := 0
		for i := range cands {
			if cands[i].h < cands[best].h {
				best = i
			}
		}
		out = append(out, cands[best].pos)
		cands = append(cands[:best], cands[best+1:]...)
	}
	// placeCollapsedRoof: one roof cell, chosen by a positional roll at the
	// roof's min corner.
	const cx, cy, cz = 16, -4, 13
	rx := cx - 2 + int(hash01(g.seed, d.X, d.Z, 0x7E11)*5)
	rz := cz - 2 + int(hash01(g.seed, d.X, d.Z, 0x7E12)*5)
	x, y, z := d.world(rx, cy+4, rz)
	return append(out, [3]int{x, y, z})
}

// templePotentialSand lists the cellar cells vanilla records with placeSand.
func templePotentialSand(d DesertTemple) [][3]int {
	const cx, cy, cz = 16, -4, 13
	var out [][3]int
	add := func(x, y, z int) {
		wx, wy, wz := d.world(x, y, z)
		out = append(out, [3]int{wx, wy, wz})
	}
	for y := cy + 1; y <= cy+3; y++ {
		for x := cx - 2; x <= cx+2; x++ {
			for z := cz - 2; z <= cz+2; z++ {
				add(x, y, z)
			}
		}
	}
	add(cx+3, cy+1, cz)
	add(cx+3, cy+2, cz)
	add(cx-3, cy+1, cz)
	add(cx-3, cy+2, cz)
	add(cx, cy+1, cz+3)
	add(cx, cy+2, cz+3)
	add(cx, cy+1, cz-3)
	add(cx, cy+2, cz-3)
	return out
}

// templeStamp is the per-chunk writer: a StructurePiece with the chunk as its
// clip box, placing piece-local cells with the orientation's mirror and
// rotation applied to directional states.
type templeStamp struct {
	g            *Generator
	d            DesertTemple
	ch           *Chunk
	baseX, baseZ int
	sus          map[[3]int]bool
}

func (s *templeStamp) place(state uint32, x, y, z int) {
	wx, wy, wz := s.d.world(x, y, z)
	if wx-s.baseX < 0 || wx-s.baseX >= 16 || wz-s.baseZ < 0 || wz-s.baseZ >= 16 {
		return
	}
	setSectionBlock(s.ch, wx-s.baseX, wy, wz-s.baseZ, s.orient(state), true)
}

func (s *templeStamp) at(x, y, z int) uint32 {
	wx, wy, wz := s.d.world(x, y, z)
	return sectionBlockAt(s.ch, wx-s.baseX, wy, wz-s.baseZ)
}

// box is StructurePiece.generateBox: the shell in outer, the inside in
// inner; with skipAir, cells that currently hold air are left alone.
func (s *templeStamp) box(x0, y0, z0, x1, y1, z1 int, outer, inner uint32, skipAir bool) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if skipAir && s.at(x, y, z) == Air {
					continue
				}
				if y == y0 || y == y1 || x == x0 || x == x1 || z == z0 || z == z1 {
					s.place(outer, x, y, z)
				} else {
					s.place(inner, x, y, z)
				}
			}
		}
	}
}

// fillDown is fillColumnDown: the state from the cell down through air and
// water until ground.
func (s *templeStamp) fillDown(state uint32, x, y, z int) {
	wx, wy, wz := s.d.world(x, y, z)
	if wx-s.baseX < 0 || wx-s.baseX >= 16 || wz-s.baseZ < 0 || wz-s.baseZ >= 16 {
		return
	}
	for ; wy > MinY+1; wy-- {
		b := sectionBlockAt(s.ch, wx-s.baseX, wy, wz-s.baseZ)
		if b != Air && (b < Water || b > Water+15) {
			return
		}
		setSectionBlock(s.ch, wx-s.baseX, wy, wz-s.baseZ, state, true)
	}
}

// orient applies the piece's mirror + rotation to a directional state
// (StructurePiece.placeBlock): south mirrors left-right, west mirrors then
// turns clockwise, east turns clockwise.
func (s *templeStamp) orient(state uint32) uint32 {
	info, ok := InfoForState(state)
	if !ok {
		return state
	}
	facing := GetProperty(info, state, "facing")
	if facing == "" {
		return state
	}
	mirror := func(f string) string {
		switch f {
		case "north":
			return "south"
		case "south":
			return "north"
		}
		return f
	}
	cw := func(f string) string {
		switch f {
		case "north":
			return "east"
		case "east":
			return "south"
		case "south":
			return "west"
		case "west":
			return "north"
		}
		return f
	}
	switch s.d.Dir {
	case 1:
		facing = mirror(facing)
	case 2:
		facing = cw(mirror(facing))
	case 3:
		facing = cw(facing)
	}
	return SetProperty(info, state, "facing", facing)
}

// stairs is a sandstone stair facing f, bottom half, dry.
func sandstoneStairs(f string) uint32 {
	base := blockBase("sandstone_stairs")
	info, _ := InfoForState(base)
	st := SetProperty(info, base, "half", "bottom")
	st = SetProperty(info, st, "waterlogged", "false")
	return SetProperty(info, st, "facing", f)
}

// stampDesertTemples writes the parts of any temple overlapping this chunk.
func (g *Generator) stampDesertTemples(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(templeCell) {
		d := g.DesertTempleIn(baseX+8+off[0], baseZ+8+off[1])
		if !d.Exists {
			continue
		}
		if d.X >= baseX+16 || d.X+templeWidth <= baseX || d.Z >= baseZ+16 || d.Z+templeDepth <= baseZ {
			continue
		}
		s := &templeStamp{g: g, d: d, ch: ch, baseX: baseX, baseZ: baseZ, sus: map[[3]int]bool{}}
		for _, c := range d.Sus {
			s.sus[c] = true
		}
		s.build()
	}
}

// build is DesertPyramidPiece.postProcess, write for write.
func (s *templeStamp) build() {
	const w, dp = templeWidth, templeDepth
	sand, ss := Sand, Sandstone
	s.box(0, -4, 0, w-1, 0, dp-1, ss, ss, false)
	for i := 1; i <= 9; i++ {
		s.box(i, i, i, w-1-i, i, dp-1-i, ss, ss, false)
		s.box(i+1, i, i+1, w-2-i, i, dp-2-i, Air, Air, false)
	}
	for x := 0; x < w; x++ {
		for z := 0; z < dp; z++ {
			s.fillDown(ss, x, -5, z)
		}
	}
	stN, stS, stE, stW := sandstoneStairs("north"), sandstoneStairs("south"), sandstoneStairs("east"), sandstoneStairs("west")
	// The two towers.
	s.box(0, 0, 0, 4, 9, 4, ss, Air, false)
	s.box(1, 10, 1, 3, 10, 3, ss, ss, false)
	s.place(stN, 2, 10, 0)
	s.place(stS, 2, 10, 4)
	s.place(stE, 0, 10, 2)
	s.place(stW, 4, 10, 2)
	s.box(w-5, 0, 0, w-1, 9, 4, ss, Air, false)
	s.box(w-4, 10, 1, w-2, 10, 3, ss, ss, false)
	s.place(stN, w-3, 10, 0)
	s.place(stS, w-3, 10, 4)
	s.place(stE, w-5, 10, 2)
	s.place(stW, w-1, 10, 2)
	// The entrance.
	s.box(8, 0, 0, 12, 4, 4, ss, Air, false)
	s.box(9, 1, 0, 11, 3, 4, Air, Air, false)
	s.place(CutSandstone, 9, 1, 1)
	s.place(CutSandstone, 9, 2, 1)
	s.place(CutSandstone, 9, 3, 1)
	s.place(CutSandstone, 10, 3, 1)
	s.place(CutSandstone, 11, 3, 1)
	s.place(CutSandstone, 11, 2, 1)
	s.place(CutSandstone, 11, 1, 1)
	s.box(4, 1, 1, 8, 3, 3, ss, Air, false)
	s.box(4, 1, 2, 8, 2, 2, Air, Air, false)
	s.box(12, 1, 1, 16, 3, 3, ss, Air, false)
	s.box(12, 1, 2, 16, 2, 2, Air, Air, false)
	s.box(5, 4, 5, w-6, 4, dp-6, ss, ss, false)
	s.box(9, 4, 9, 11, 4, 11, Air, Air, false)
	s.box(8, 1, 8, 8, 3, 8, CutSandstone, CutSandstone, false)
	s.box(12, 1, 8, 12, 3, 8, CutSandstone, CutSandstone, false)
	s.box(8, 1, 12, 8, 3, 12, CutSandstone, CutSandstone, false)
	s.box(12, 1, 12, 12, 3, 12, CutSandstone, CutSandstone, false)
	s.box(1, 1, 5, 4, 4, 11, ss, ss, false)
	s.box(w-5, 1, 5, w-2, 4, 11, ss, ss, false)
	s.box(6, 7, 9, 6, 7, 11, ss, ss, false)
	s.box(w-7, 7, 9, w-7, 7, 11, ss, ss, false)
	s.box(5, 5, 9, 5, 7, 11, CutSandstone, CutSandstone, false)
	s.box(w-6, 5, 9, w-6, 7, 11, CutSandstone, CutSandstone, false)
	s.place(Air, 5, 5, 10)
	s.place(Air, 5, 6, 10)
	s.place(Air, 6, 6, 10)
	s.place(Air, w-6, 5, 10)
	s.place(Air, w-6, 6, 10)
	s.place(Air, w-7, 6, 10)
	s.box(2, 4, 4, 2, 6, 4, Air, Air, false)
	s.box(w-3, 4, 4, w-3, 6, 4, Air, Air, false)
	s.place(stN, 2, 4, 5)
	s.place(stN, 2, 3, 4)
	s.place(stN, w-3, 4, 5)
	s.place(stN, w-3, 3, 4)
	s.box(1, 1, 3, 2, 2, 3, ss, ss, false)
	s.box(w-3, 1, 3, w-2, 2, 3, ss, ss, false)
	s.place(ss, 1, 1, 2)
	s.place(ss, w-2, 1, 2)
	s.place(SandstoneSlab, 1, 2, 2)
	s.place(SandstoneSlab, w-2, 2, 2)
	s.place(stW, 2, 1, 2)
	s.place(stE, w-3, 1, 2)
	s.box(4, 3, 5, 4, 3, 17, ss, ss, false)
	s.box(w-5, 3, 5, w-5, 3, 17, ss, ss, false)
	s.box(3, 1, 5, 4, 2, 16, Air, Air, false)
	s.box(w-6, 1, 5, w-5, 2, 16, Air, Air, false)
	for z := 5; z <= 17; z += 2 {
		s.place(CutSandstone, 4, 1, z)
		s.place(ChiseledSandstone, 4, 2, z)
		s.place(CutSandstone, w-5, 1, z)
		s.place(ChiseledSandstone, w-5, 2, z)
	}
	// The floor motif over the treasure well.
	for _, c := range [][2]int{{10, 7}, {10, 8}, {9, 9}, {11, 9}, {8, 10}, {12, 10}, {7, 10}, {13, 10}, {9, 11}, {11, 11}, {10, 12}, {10, 13}} {
		s.place(OrangeTerracotta, c[0], 0, c[1])
	}
	s.place(BlueTerracotta, 10, 0, 10)
	// The side motifs.
	for x := 0; x <= w-1; x += w - 1 {
		s.place(CutSandstone, x, 2, 1)
		s.place(OrangeTerracotta, x, 2, 2)
		s.place(CutSandstone, x, 2, 3)
		s.place(CutSandstone, x, 3, 1)
		s.place(OrangeTerracotta, x, 3, 2)
		s.place(CutSandstone, x, 3, 3)
		s.place(OrangeTerracotta, x, 4, 1)
		s.place(ChiseledSandstone, x, 4, 2)
		s.place(OrangeTerracotta, x, 4, 3)
		s.place(CutSandstone, x, 5, 1)
		s.place(OrangeTerracotta, x, 5, 2)
		s.place(CutSandstone, x, 5, 3)
		s.place(OrangeTerracotta, x, 6, 1)
		s.place(ChiseledSandstone, x, 6, 2)
		s.place(OrangeTerracotta, x, 6, 3)
		s.place(OrangeTerracotta, x, 7, 1)
		s.place(OrangeTerracotta, x, 7, 2)
		s.place(OrangeTerracotta, x, 7, 3)
		s.place(CutSandstone, x, 8, 1)
		s.place(CutSandstone, x, 8, 2)
		s.place(CutSandstone, x, 8, 3)
	}
	// The front motifs.
	for x := 2; x <= w-3; x += w - 3 - 2 {
		s.place(CutSandstone, x-1, 2, 0)
		s.place(OrangeTerracotta, x, 2, 0)
		s.place(CutSandstone, x+1, 2, 0)
		s.place(CutSandstone, x-1, 3, 0)
		s.place(OrangeTerracotta, x, 3, 0)
		s.place(CutSandstone, x+1, 3, 0)
		s.place(OrangeTerracotta, x-1, 4, 0)
		s.place(ChiseledSandstone, x, 4, 0)
		s.place(OrangeTerracotta, x+1, 4, 0)
		s.place(CutSandstone, x-1, 5, 0)
		s.place(OrangeTerracotta, x, 5, 0)
		s.place(CutSandstone, x+1, 5, 0)
		s.place(OrangeTerracotta, x-1, 6, 0)
		s.place(ChiseledSandstone, x, 6, 0)
		s.place(OrangeTerracotta, x+1, 6, 0)
		s.place(OrangeTerracotta, x-1, 7, 0)
		s.place(OrangeTerracotta, x, 7, 0)
		s.place(OrangeTerracotta, x+1, 7, 0)
		s.place(CutSandstone, x-1, 8, 0)
		s.place(CutSandstone, x, 8, 0)
		s.place(CutSandstone, x+1, 8, 0)
	}
	s.box(8, 4, 0, 12, 6, 0, CutSandstone, CutSandstone, false)
	s.place(Air, 8, 6, 0)
	s.place(Air, 12, 6, 0)
	s.place(OrangeTerracotta, 9, 5, 0)
	s.place(ChiseledSandstone, 10, 5, 0)
	s.place(OrangeTerracotta, 11, 5, 0)
	// The treasure well and its trap.
	s.box(8, -14, 8, 12, -11, 12, CutSandstone, CutSandstone, false)
	s.box(8, -10, 8, 12, -10, 12, ChiseledSandstone, ChiseledSandstone, false)
	s.box(8, -9, 8, 12, -9, 12, CutSandstone, CutSandstone, false)
	s.box(8, -8, 8, 12, -1, 12, ss, ss, false)
	s.box(9, -11, 9, 11, -1, 11, Air, Air, false)
	s.place(StonePressurePlate, 10, -11, 10)
	s.box(9, -13, 9, 11, -13, 11, TNTBlock, Air, false)
	s.place(Air, 8, -11, 10)
	s.place(Air, 8, -10, 10)
	s.place(ChiseledSandstone, 7, -10, 10)
	s.place(CutSandstone, 7, -11, 10)
	s.place(Air, 12, -11, 10)
	s.place(Air, 12, -10, 10)
	s.place(ChiseledSandstone, 13, -10, 10)
	s.place(CutSandstone, 13, -11, 10)
	s.place(Air, 10, -11, 8)
	s.place(Air, 10, -10, 8)
	s.place(ChiseledSandstone, 10, -10, 7)
	s.place(CutSandstone, 10, -11, 7)
	s.place(Air, 10, -11, 12)
	s.place(Air, 10, -10, 12)
	s.place(ChiseledSandstone, 10, -10, 13)
	s.place(CutSandstone, 10, -11, 13)
	// createChest → reorient: a chest with one open side faces it, so each
	// well chest faces the pressure plate whichever way the piece points.
	chestInfo, _ := InfoForState(ChestNorth)
	for _, off := range [4][2]int{{0, -2}, {0, 2}, {-2, 0}, {2, 0}} {
		wx, wy, wz := s.d.world(10+off[0], -11, 10+off[1])
		mx, _, mz := s.d.world(10, -11, 10)
		facing := "north"
		switch {
		case mz > wz:
			facing = "south"
		case mx < wx:
			facing = "west"
		case mx > wx:
			facing = "east"
		}
		if wx-s.baseX >= 0 && wx-s.baseX < 16 && wz-s.baseZ >= 0 && wz-s.baseZ < 16 {
			setSectionBlock(s.ch, wx-s.baseX, wy, wz-s.baseZ, SetProperty(chestInfo, ChestNorth, "facing", facing), true)
		}
	}
	// The cellar (addCellar): the broken stair down and the sand-filled room.
	const cx, cy, cz = 16, -4, 13
	s.place(stW, 13, -1, 17) // default stairs (north) turned counter-clockwise
	s.place(stW, 14, -2, 17)
	s.place(stW, 15, -3, 17)
	bl := hash01(s.g.seed, s.d.X, s.d.Z, 0x7E13) < 0.5
	s.place(sand, cx-4, cy+4, cz+4)
	s.place(sand, cx-3, cy+4, cz+4)
	s.place(sand, cx-2, cy+4, cz+4)
	s.place(sand, cx-1, cy+4, cz+4)
	s.place(sand, cx, cy+4, cz+4)
	s.place(sand, cx-2, cy+3, cz+4)
	if bl {
		s.place(sand, cx-1, cy+3, cz+4)
		s.place(ss, cx, cy+3, cz+4)
	} else {
		s.place(ss, cx-1, cy+3, cz+4)
		s.place(sand, cx, cy+3, cz+4)
	}
	s.place(sand, cx-1, cy+2, cz+4)
	s.place(ss, cx, cy+2, cz+4)
	s.place(sand, cx, cy+1, cz+4)
	// addCellarRoom.
	cut, chis := CutSandstone, ChiseledSandstone
	s.box(cx-3, cy+1, cz-3, cx-3, cy+1, cz+2, cut, cut, true)
	s.box(cx+3, cy+1, cz-3, cx+3, cy+1, cz+2, cut, cut, true)
	s.box(cx-3, cy+1, cz-3, cx+3, cy+1, cz-2, cut, cut, true)
	s.box(cx-3, cy+1, cz+3, cx+3, cy+1, cz+3, cut, cut, true)
	s.box(cx-3, cy+2, cz-3, cx-3, cy+2, cz+2, chis, chis, true)
	s.box(cx+3, cy+2, cz-3, cx+3, cy+2, cz+2, chis, chis, true)
	s.box(cx-3, cy+2, cz-3, cx+3, cy+2, cz-2, chis, chis, true)
	s.box(cx-3, cy+2, cz+3, cx+3, cy+2, cz+3, chis, chis, true)
	s.box(cx-3, -1, cz-3, cx-3, -1, cz+2, cut, cut, true)
	s.box(cx+3, -1, cz-3, cx+3, -1, cz+2, cut, cut, true)
	s.box(cx-3, -1, cz-3, cx+3, -1, cz-2, cut, cut, true)
	s.box(cx-3, -1, cz+3, cx+3, -1, cz+3, cut, cut, true)
	// The collapsed roof: sandstone one in three, sand otherwise.
	for x := cx - 2; x <= cx+2; x++ {
		for z := cz - 2; z <= cz+2; z++ {
			wx, _, wz := s.d.world(x, cy+4, z)
			if hash01(s.g.seed, wx, wz, 0x7E14) < 0.33 {
				s.place(ss, x, cy+4, z)
			} else {
				s.place(sand, x, cy+4, z)
			}
		}
	}
	s.place(BlueTerracotta, cx, cy, cz)
	s.place(OrangeTerracotta, cx+1, cy, cz-1)
	s.place(OrangeTerracotta, cx+1, cy, cz+1)
	s.place(OrangeTerracotta, cx-1, cy, cz-1)
	s.place(OrangeTerracotta, cx-1, cy, cz+1)
	s.place(OrangeTerracotta, cx+2, cy, cz)
	s.place(OrangeTerracotta, cx-2, cy, cz)
	s.place(OrangeTerracotta, cx, cy, cz+2)
	s.place(OrangeTerracotta, cx, cy, cz-2)
	s.place(OrangeTerracotta, cx+3, cy, cz)
	s.place(cut, cx+4, cy+1, cz)
	s.place(chis, cx+4, cy+2, cz)
	s.place(OrangeTerracotta, cx-3, cy, cz)
	s.place(cut, cx-4, cy+1, cz)
	s.place(chis, cx-4, cy+2, cz)
	s.place(OrangeTerracotta, cx, cy, cz+3)
	s.place(OrangeTerracotta, cx, cy, cz-3)
	s.place(cut, cx, cy+1, cz-4)
	s.place(chis, cx, -2, cz-4)
	// afterPlace: every potential sand cell becomes sand, the chosen ones and
	// the collapsed-roof pick suspicious sand.
	for _, c := range templePotentialSand(s.d) {
		st := Sand
		if s.sus[c] {
			st = SuspiciousSand
		}
		if c[0]-s.baseX >= 0 && c[0]-s.baseX < 16 && c[2]-s.baseZ >= 0 && c[2]-s.baseZ < 16 {
			setSectionBlock(s.ch, c[0]-s.baseX, c[1], c[2]-s.baseZ, st, true)
		}
	}
	for _, c := range s.d.Sus {
		if c[0]-s.baseX >= 0 && c[0]-s.baseX < 16 && c[2]-s.baseZ >= 0 && c[2]-s.baseZ < 16 {
			setSectionBlock(s.ch, c[0]-s.baseX, c[1], c[2]-s.baseZ, SuspiciousSand, true)
		}
	}
}
