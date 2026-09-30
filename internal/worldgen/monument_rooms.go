package worldgen

// The ocean monument's interior: a port of OceanMonumentPieces' room graph
// and its pieces — the 5×5×3 grid of RoomDefinitions (connections, the
// source room at the entrance, the core room's 2×2×2 claim, the random
// closing of up to two openings per room that keeps every room reachable
// from the source), the fitters in vanilla's order (double XY, double YZ,
// double Z, double X, double Y, simple top, simple), the entry room, the
// core room with its gold, the two wing rooms, the penthouse, and the
// simple top rooms' wet sponges. The building takes one of the four
// horizontal orientations, as vanilla's does, and every piece is placed
// through StructurePiece's orientation transform.
//
// The plan is drawn from the monument's own position-seeded stream, so
// every chunk pass builds the same monument; the pieces' own draws (the
// centre pillars, the sponges) come from a stream per piece for the same
// reason. The elder guardians' three spots are the pieces' spawnElder
// cells, which the server seeds (guardian.go).
//
// A monument with a player's build or dig anywhere in its footprint keeps
// the layout it had before (the shell facing one way, an empty hall, the
// gold in a pillar): stampMonument decides that from the build guard.

// Direction 3D data values (Direction.get3DDataValue) and their steps.
const (
	dirDown = iota
	dirUp
	dirNorth
	dirSouth
	dirWest
	dirEast
)

var monSteps = [6][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

func monOpposite(d int) int { return d ^ 1 }

// The monument's blocks.
var (
	mGray      = blockBase("prismarine")
	mLight     = blockBase("prismarine_bricks")
	mBlack     = blockBase("dark_prismarine")
	mLamp      = blockBase("sea_lantern")
	mGold      = blockBase("gold_block")
	mWetSponge = blockBase("wet_sponge")
)

// monBox is an inclusive block box.
type monBox struct{ x0, y0, z0, x1, y1, z1 int }

func (b monBox) moved(dx, dy, dz int) monBox {
	return monBox{b.x0 + dx, b.y0 + dy, b.z0 + dz, b.x1 + dx, b.y1 + dy, b.z1 + dz}
}

// makeMonBox is StructurePiece.makeBoundingBox: width runs along x for a
// north/south piece, along z for a west/east one.
func makeMonBox(x, y, z, dir, w, h, d int) monBox {
	if dir == dirNorth || dir == dirSouth {
		return monBox{x, y, z, x + w - 1, y + h - 1, z + d - 1}
	}
	return monBox{x, y, z, x + d - 1, y + h - 1, z + w - 1}
}

// monBoxCorners is BoundingBox.fromCorners.
func monBoxCorners(a, b [3]int) monBox {
	return monBox{min(a[0], b[0]), min(a[1], b[1]), min(a[2], b[2]), max(a[0], b[0]), max(a[1], b[1]), max(a[2], b[2])}
}

// monDef is a RoomDefinition.
type monDef struct {
	index             int
	connections       [6]*monDef
	hasOpening        [6]bool
	claimed, isSource bool
	scanIndex         int
}

func (d *monDef) setConnection(dir int, o *monDef) {
	d.connections[dir] = o
	o.connections[monOpposite(dir)] = d
}

func (d *monDef) updateOpenings() {
	for i := 0; i < 6; i++ {
		d.hasOpening[i] = d.connections[i] != nil
	}
}

func (d *monDef) findSource(scan int) bool {
	if d.isSource {
		return true
	}
	d.scanIndex = scan
	for i := 0; i < 6; i++ {
		if c := d.connections[i]; c != nil && d.hasOpening[i] && c.scanIndex != scan && c.findSource(scan) {
			return true
		}
	}
	return false
}

func (d *monDef) isSpecial() bool { return d.index >= 75 }

func (d *monDef) countOpenings() int {
	n := 0
	for i := 0; i < 6; i++ {
		if d.hasOpening[i] {
			n++
		}
	}
	return n
}

func monRoomIndex(x, y, z int) int { return y*25 + z*5 + x }

// The piece kinds.
const (
	monEntry = iota
	monCore
	monDoubleXY
	monDoubleYZ
	monDoubleZ
	monDoubleX
	monDoubleY
	monSimpleTop
	monSimple
	monWing
	monPenthouse
)

// monPiece is one OceanMonumentPiece: its box, orientation and room, and
// while it is being stamped the chunk it writes into.
type monPiece struct {
	kind   int
	box    monBox
	dir    int
	def    *monDef
	design int // the simple rooms' and the wings' mainDesign

	rng    TreeRNG   // the piece's own draws while stamping
	s      *monStamp // the chunk being stamped (nil while planning)
	elders *[][3]int // spawnElder cells, collected while planning
}

// monRoomBox is the room pieces' makeBoundingBox, before the building's
// offset moves it.
func monRoomBox(dir int, def *monDef, w, h, d int) monBox {
	rx, rz, ry := def.index%5, def.index/5%5, def.index/25
	b := makeMonBox(0, 0, 0, dir, w*8, h*4, d*8)
	switch dir {
	case dirNorth:
		return b.moved(rx*8, ry*4, -(rz+d)*8+1)
	case dirSouth:
		return b.moved(rx*8, ry*4, rz*8)
	case dirWest:
		return b.moved(-(rz+d)*8+1, ry*4, rx*8)
	default:
		return b.moved(rz*8, ry*4, rx*8)
	}
}

// StructurePiece's orientation transform.
func (p *monPiece) worldX(x, z int) int {
	switch p.dir {
	case dirNorth, dirSouth:
		return p.box.x0 + x
	case dirWest:
		return p.box.x1 - z
	default:
		return p.box.x0 + z
	}
}

func (p *monPiece) worldY(y int) int { return p.box.y0 + y }

func (p *monPiece) worldZ(x, z int) int {
	switch p.dir {
	case dirNorth:
		return p.box.z1 - z
	case dirSouth:
		return p.box.z0 + z
	default:
		return p.box.z0 + x
	}
}

func (p *monPiece) worldPos(x, y, z int) [3]int {
	return [3]int{p.worldX(x, z), p.worldY(y), p.worldZ(x, z)}
}

// get reads a cell of the chunk being stamped; outside it reads air, as
// StructurePiece.getBlock does.
func (p *monPiece) get(x, y, z int) uint32 {
	if p.s == nil {
		return Air
	}
	w := p.worldPos(x, y, z)
	return p.s.getW(w[0], w[1], w[2])
}

// set is placeBlock: the block at local (x, y, z), clipped to the chunk.
func (p *monPiece) set(x, y, z int, b uint32) {
	if p.s == nil {
		return
	}
	w := p.worldPos(x, y, z)
	setSectionBlock(p.s.ch, w[0]-p.s.baseX, w[1], w[2]-p.s.baseZ, b, true)
}

// box is generateBox with one block for edge and fill, overwriting.
func (p *monPiece) box(x0, y0, z0, x1, y1, z1 int, b uint32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				p.set(x, y, z, b)
			}
		}
	}
}

// water is generateWaterBox: water below the sea's surface and air above,
// leaving ice and water that is already there.
func (p *monPiece) water(x0, y0, z0, x1, y1, z1 int) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				cur := p.get(x, y, z)
				if cur == Ice || cur == PackedIce || cur == BlueIce || cur == Water {
					continue
				}
				if p.worldY(y) >= SeaLevel {
					p.set(x, y, z, Air)
				} else {
					p.set(x, y, z, Water)
				}
			}
		}
	}
}

// boxOnFill is generateBoxOnFillOnly: the block only where water stands.
func (p *monPiece) boxOnFill(x0, y0, z0, x1, y1, z1 int, b uint32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if p.get(x, y, z) == Water {
					p.set(x, y, z, b)
				}
			}
		}
	}
}

// defaultFloor is generateDefaultFloor: a plain floor, or one with a
// two-by-two hole ringed in bricks over a room below.
func (p *monPiece) defaultFloor(xOff, zOff int, downOpening bool) {
	if downOpening {
		p.box(xOff+0, 0, zOff+0, xOff+2, 0, zOff+8-1, mGray)
		p.box(xOff+5, 0, zOff+0, xOff+8-1, 0, zOff+8-1, mGray)
		p.box(xOff+3, 0, zOff+0, xOff+4, 0, zOff+2, mGray)
		p.box(xOff+3, 0, zOff+5, xOff+4, 0, zOff+8-1, mGray)
		p.box(xOff+3, 0, zOff+2, xOff+4, 0, zOff+2, mLight)
		p.box(xOff+3, 0, zOff+5, xOff+4, 0, zOff+5, mLight)
		p.box(xOff+2, 0, zOff+3, xOff+2, 0, zOff+4, mLight)
		p.box(xOff+5, 0, zOff+3, xOff+5, 0, zOff+4, mLight)
	} else {
		p.box(xOff+0, 0, zOff+0, xOff+8-1, 0, zOff+8-1, mGray)
	}
}

// elder is spawnElder: while planning, the cell is noted for the server.
func (p *monPiece) elder(x, y, z int) {
	if p.elders != nil {
		*p.elders = append(*p.elders, p.worldPos(x, y, z))
	}
}

// postProcess stamps the piece.
func (p *monPiece) postProcess() {
	switch p.kind {
	case monEntry:
		p.entryRoom()
	case monCore:
		p.coreRoom()
	case monDoubleXY:
		p.doubleXYRoom()
	case monDoubleYZ:
		p.doubleYZRoom()
	case monDoubleZ:
		p.doubleZRoom()
	case monDoubleX:
		p.doubleXRoom()
	case monDoubleY:
		p.doubleYRoom()
	case monSimpleTop:
		p.simpleTopRoom()
	case monSimple:
		p.simpleRoom()
	case monWing:
		p.wingRoom()
	case monPenthouse:
		p.penthouse()
	}
}

// monPlan is one monument's layout: the building (orientation and box)
// and its child pieces in vanilla's postProcess order.
type monPlan struct {
	bld    monPiece
	pieces []*monPiece
	elders [][3]int
}

const monPlanSalt = 0x30A04E7E

// monumentPlan lays out a monument as MonumentBuilding's constructor does.
func (g *Generator) monumentPlan(m Monument) *monPlan {
	r := newTreeRNG(g.seed^monPlanSalt, m.X, m.Z)
	dir := [4]int{dirNorth, dirEast, dirSouth, dirWest}[r.Intn(4)] // Direction.Plane.HORIZONTAL
	pl := &monPlan{}
	pl.bld = monPiece{box: makeMonBox(m.X-monumentHalf, m.Y, m.Z-monumentHalf, dir, 58, 23, 58), dir: dir}
	defs, source, core := monRoomGraph(r)
	source.claimed = true
	add := func(kind int, def *monDef, w, h, d int) *monPiece {
		p := &monPiece{kind: kind, dir: dir, def: def, box: monRoomBox(dir, def, w, h, d)}
		pl.pieces = append(pl.pieces, p)
		return p
	}
	add(monEntry, source, 1, 1, 1)
	add(monCore, core, 2, 2, 2)
	for _, d := range defs {
		if d.claimed || d.isSpecial() {
			continue
		}
		e, u, n := d.connections[dirEast], d.connections[dirUp], d.connections[dirNorth]
		switch {
		case d.hasOpening[dirEast] && !e.claimed && d.hasOpening[dirUp] && !u.claimed && e.hasOpening[dirUp] && !e.connections[dirUp].claimed:
			d.claimed, e.claimed, u.claimed, e.connections[dirUp].claimed = true, true, true, true
			add(monDoubleXY, d, 2, 2, 1)
		case d.hasOpening[dirNorth] && !n.claimed && d.hasOpening[dirUp] && !u.claimed && n.hasOpening[dirUp] && !n.connections[dirUp].claimed:
			d.claimed, n.claimed, u.claimed, n.connections[dirUp].claimed = true, true, true, true
			add(monDoubleYZ, d, 1, 2, 2)
		case d.hasOpening[dirNorth] && !n.claimed:
			d.claimed, n.claimed = true, true
			add(monDoubleZ, d, 1, 1, 2)
		case d.hasOpening[dirEast] && !e.claimed:
			d.claimed, e.claimed = true, true
			add(monDoubleX, d, 2, 1, 1)
		case d.hasOpening[dirUp] && !u.claimed:
			d.claimed, u.claimed = true, true
			add(monDoubleY, d, 1, 2, 1)
		case !d.hasOpening[dirWest] && !d.hasOpening[dirEast] && !d.hasOpening[dirNorth] && !d.hasOpening[dirSouth] && !d.hasOpening[dirUp]:
			d.claimed = true
			add(monSimpleTop, d, 1, 1, 1)
		default:
			d.claimed = true
			add(monSimple, d, 1, 1, 1).design = r.Intn(3)
		}
	}
	off := pl.bld.worldPos(9, 0, 22)
	for _, p := range pl.pieces {
		p.box = p.box.moved(off[0], off[1], off[2])
	}
	wing := r.Intn(2) // wingRandom & 1, and the next value's for the right wing
	b := &pl.bld
	pl.pieces = append(pl.pieces,
		&monPiece{kind: monWing, dir: dir, design: wing, box: monBoxCorners(b.worldPos(1, 1, 1), b.worldPos(23, 8, 21))},
		&monPiece{kind: monWing, dir: dir, design: (wing + 1) & 1, box: monBoxCorners(b.worldPos(34, 1, 1), b.worldPos(56, 8, 21))},
		&monPiece{kind: monPenthouse, dir: dir, box: monBoxCorners(b.worldPos(22, 13, 22), b.worldPos(35, 17, 35))})
	// Run the wings and the penthouse once with no chunk to collect their
	// spawnElder cells (they draw nothing).
	for _, p := range pl.pieces {
		if p.kind == monWing || p.kind == monPenthouse {
			p.elders = &pl.elders
			p.postProcess()
			p.elders = nil
		}
	}
	return pl
}

// pieceRNG is piece i's own stream for its draws while stamping.
func (pl *monPlan) pieceRNG(g *Generator, m Monument, i int) TreeRNG {
	return newTreeRNG(g.seed^monPlanSalt^int64(i+1)*0x2545F4914F6CDD1D, m.X, m.Z)
}

// monRoomGraph is generateRoomGraph: the grid, its connections (vanilla's
// grid links a room's z neighbours under the opposite direction), the
// roof and wing links, the core room's claim, and up to two openings
// closed per room (in a shuffled order) where the rooms stay reachable
// from the source. It returns the shuffled rooms then the three specials.
func monRoomGraph(r TreeRNG) (defs []*monDef, source, core *monDef) {
	var grid [75]*monDef
	for x := 0; x < 5; x++ {
		for z := 0; z < 4; z++ {
			grid[monRoomIndex(x, 0, z)] = &monDef{index: monRoomIndex(x, 0, z)}
		}
	}
	for x := 0; x < 5; x++ {
		for z := 0; z < 4; z++ {
			grid[monRoomIndex(x, 1, z)] = &monDef{index: monRoomIndex(x, 1, z)}
		}
	}
	for x := 1; x < 4; x++ {
		for z := 0; z < 2; z++ {
			grid[monRoomIndex(x, 2, z)] = &monDef{index: monRoomIndex(x, 2, z)}
		}
	}
	source = grid[monRoomIndex(2, 0, 0)]
	for x := 0; x < 5; x++ {
		for z := 0; z < 5; z++ {
			for y := 0; y < 3; y++ {
				pos := monRoomIndex(x, y, z)
				if grid[pos] == nil {
					continue
				}
				for d := 0; d < 6; d++ {
					nx, ny, nz := x+monSteps[d][0], y+monSteps[d][1], z+monSteps[d][2]
					if nx < 0 || nx >= 5 || nz < 0 || nz >= 5 || ny < 0 || ny >= 3 {
						continue
					}
					n := grid[monRoomIndex(nx, ny, nz)]
					if n == nil {
						continue
					}
					if nz == z {
						grid[pos].setConnection(d, n)
					} else {
						grid[pos].setConnection(monOpposite(d), n)
					}
				}
			}
		}
	}
	roof, left, right := &monDef{index: 1003}, &monDef{index: 1001}, &monDef{index: 1002}
	grid[monRoomIndex(2, 2, 0)].setConnection(dirUp, roof)
	grid[monRoomIndex(0, 1, 0)].setConnection(dirSouth, left)
	grid[monRoomIndex(4, 1, 0)].setConnection(dirSouth, right)
	roof.claimed, left.claimed, right.claimed = true, true, true
	source.isSource = true
	core = grid[monRoomIndex(r.Intn(4), 0, 2)]
	e, n, u := core.connections[dirEast], core.connections[dirNorth], core.connections[dirUp]
	core.claimed, e.claimed, n.claimed, e.connections[dirNorth].claimed = true, true, true, true
	u.claimed, e.connections[dirUp].claimed, n.connections[dirUp].claimed = true, true, true
	e.connections[dirNorth].connections[dirUp].claimed = true
	for _, d := range grid {
		if d != nil {
			d.updateOpenings()
			defs = append(defs, d)
		}
	}
	roof.updateOpenings()
	for i := len(defs); i > 1; i-- { // Util.shuffle
		j := r.Intn(i)
		defs[i-1], defs[j] = defs[j], defs[i-1]
	}
	scan := 1
	for _, d := range defs {
		closed, attempts := 0, 0
		for closed < 2 && attempts < 5 {
			attempts++
			f := r.Intn(6)
			if !d.hasOpening[f] {
				continue
			}
			of := monOpposite(f)
			d.hasOpening[f] = false
			d.connections[f].hasOpening[of] = false
			ok := d.findSource(scan)
			scan++
			if ok {
				ok = d.connections[f].findSource(scan)
				scan++
			}
			if ok {
				closed++
			} else {
				d.hasOpening[f] = true
				d.connections[f].hasOpening[of] = true
			}
		}
	}
	defs = append(defs, roof, left, right)
	return defs, source, core
}

// ---- the room pieces' postProcess bodies ------------------------------------

func (p *monPiece) coreRoom() {
	p.boxOnFill(1, 8, 0, 14, 8, 14, mGray)
	block := mLight
	p.box(0, 7, 0, 0, 7, 15, block)
	p.box(15, 7, 0, 15, 7, 15, block)
	p.box(1, 7, 0, 15, 7, 0, block)
	p.box(1, 7, 15, 14, 7, 15, block)

	for yx := 1; yx <= 6; yx++ {
		block = mLight
		if yx == 2 || yx == 6 {
			block = mGray
		}

		for x := 0; x <= 15; x += 15 {
			p.box(x, yx, 0, x, yx, 1, block)
			p.box(x, yx, 6, x, yx, 9, block)
			p.box(x, yx, 14, x, yx, 15, block)
		}

		p.box(1, yx, 0, 1, yx, 0, block)
		p.box(6, yx, 0, 9, yx, 0, block)
		p.box(14, yx, 0, 14, yx, 0, block)
		p.box(1, yx, 15, 14, yx, 15, block)
	}

	p.box(6, 3, 6, 9, 6, 9, mBlack)
	p.box(7, 4, 7, 8, 5, 8, mGold)

	for yx := 3; yx <= 6; yx += 3 {
		for x := 6; x <= 9; x += 3 {
			p.set(x, yx, 6, mLamp)
			p.set(x, yx, 9, mLamp)
		}
	}

	p.box(5, 1, 6, 5, 2, 6, mLight)
	p.box(5, 1, 9, 5, 2, 9, mLight)
	p.box(10, 1, 6, 10, 2, 6, mLight)
	p.box(10, 1, 9, 10, 2, 9, mLight)
	p.box(6, 1, 5, 6, 2, 5, mLight)
	p.box(9, 1, 5, 9, 2, 5, mLight)
	p.box(6, 1, 10, 6, 2, 10, mLight)
	p.box(9, 1, 10, 9, 2, 10, mLight)
	p.box(5, 2, 5, 5, 6, 5, mLight)
	p.box(5, 2, 10, 5, 6, 10, mLight)
	p.box(10, 2, 5, 10, 6, 5, mLight)
	p.box(10, 2, 10, 10, 6, 10, mLight)
	p.box(5, 7, 1, 5, 7, 6, mLight)
	p.box(10, 7, 1, 10, 7, 6, mLight)
	p.box(5, 7, 9, 5, 7, 14, mLight)
	p.box(10, 7, 9, 10, 7, 14, mLight)
	p.box(1, 7, 5, 6, 7, 5, mLight)
	p.box(1, 7, 10, 6, 7, 10, mLight)
	p.box(9, 7, 5, 14, 7, 5, mLight)
	p.box(9, 7, 10, 14, 7, 10, mLight)
	p.box(2, 1, 2, 2, 1, 3, mLight)
	p.box(3, 1, 2, 3, 1, 2, mLight)
	p.box(13, 1, 2, 13, 1, 3, mLight)
	p.box(12, 1, 2, 12, 1, 2, mLight)
	p.box(2, 1, 12, 2, 1, 13, mLight)
	p.box(3, 1, 13, 3, 1, 13, mLight)
	p.box(13, 1, 12, 13, 1, 13, mLight)
	p.box(12, 1, 13, 12, 1, 13, mLight)
}

func (p *monPiece) doubleXRoom() {
	east := p.def.connections[dirEast]
	west := p.def
	if p.def.index/25 > 0 {
		p.defaultFloor(8, 0, east.hasOpening[dirDown])
		p.defaultFloor(0, 0, west.hasOpening[dirDown])
	}

	if west.connections[dirUp] == nil {
		p.boxOnFill(1, 4, 1, 7, 4, 6, mGray)
	}

	if east.connections[dirUp] == nil {
		p.boxOnFill(8, 4, 1, 14, 4, 6, mGray)
	}

	p.box(0, 3, 0, 0, 3, 7, mLight)
	p.box(15, 3, 0, 15, 3, 7, mLight)
	p.box(1, 3, 0, 15, 3, 0, mLight)
	p.box(1, 3, 7, 14, 3, 7, mLight)
	p.box(0, 2, 0, 0, 2, 7, mGray)
	p.box(15, 2, 0, 15, 2, 7, mGray)
	p.box(1, 2, 0, 15, 2, 0, mGray)
	p.box(1, 2, 7, 14, 2, 7, mGray)
	p.box(0, 1, 0, 0, 1, 7, mLight)
	p.box(15, 1, 0, 15, 1, 7, mLight)
	p.box(1, 1, 0, 15, 1, 0, mLight)
	p.box(1, 1, 7, 14, 1, 7, mLight)
	p.box(5, 1, 0, 10, 1, 4, mLight)
	p.box(6, 2, 0, 9, 2, 3, mGray)
	p.box(5, 3, 0, 10, 3, 4, mLight)
	p.set(6, 2, 3, mLamp)
	p.set(9, 2, 3, mLamp)
	if west.hasOpening[dirSouth] {
		p.water(3, 1, 0, 4, 2, 0)
	}

	if west.hasOpening[dirNorth] {
		p.water(3, 1, 7, 4, 2, 7)
	}

	if west.hasOpening[dirWest] {
		p.water(0, 1, 3, 0, 2, 4)
	}

	if east.hasOpening[dirSouth] {
		p.water(11, 1, 0, 12, 2, 0)
	}

	if east.hasOpening[dirNorth] {
		p.water(11, 1, 7, 12, 2, 7)
	}

	if east.hasOpening[dirEast] {
		p.water(15, 1, 3, 15, 2, 4)
	}
}

func (p *monPiece) doubleXYRoom() {
	east := p.def.connections[dirEast]
	west := p.def
	westUp := west.connections[dirUp]
	eastUp := east.connections[dirUp]
	if p.def.index/25 > 0 {
		p.defaultFloor(8, 0, east.hasOpening[dirDown])
		p.defaultFloor(0, 0, west.hasOpening[dirDown])
	}

	if westUp.connections[dirUp] == nil {
		p.boxOnFill(1, 8, 1, 7, 8, 6, mGray)
	}

	if eastUp.connections[dirUp] == nil {
		p.boxOnFill(8, 8, 1, 14, 8, 6, mGray)
	}

	for y := 1; y <= 7; y++ {
		block := mLight
		if y == 2 || y == 6 {
			block = mGray
		}

		p.box(0, y, 0, 0, y, 7, block)
		p.box(15, y, 0, 15, y, 7, block)
		p.box(1, y, 0, 15, y, 0, block)
		p.box(1, y, 7, 14, y, 7, block)
	}

	p.box(2, 1, 3, 2, 7, 4, mLight)
	p.box(3, 1, 2, 4, 7, 2, mLight)
	p.box(3, 1, 5, 4, 7, 5, mLight)
	p.box(13, 1, 3, 13, 7, 4, mLight)
	p.box(11, 1, 2, 12, 7, 2, mLight)
	p.box(11, 1, 5, 12, 7, 5, mLight)
	p.box(5, 1, 3, 5, 3, 4, mLight)
	p.box(10, 1, 3, 10, 3, 4, mLight)
	p.box(5, 7, 2, 10, 7, 5, mLight)
	p.box(5, 5, 2, 5, 7, 2, mLight)
	p.box(10, 5, 2, 10, 7, 2, mLight)
	p.box(5, 5, 5, 5, 7, 5, mLight)
	p.box(10, 5, 5, 10, 7, 5, mLight)
	p.set(6, 6, 2, mLight)
	p.set(9, 6, 2, mLight)
	p.set(6, 6, 5, mLight)
	p.set(9, 6, 5, mLight)
	p.box(5, 4, 3, 6, 4, 4, mLight)
	p.box(9, 4, 3, 10, 4, 4, mLight)
	p.set(5, 4, 2, mLamp)
	p.set(5, 4, 5, mLamp)
	p.set(10, 4, 2, mLamp)
	p.set(10, 4, 5, mLamp)
	if west.hasOpening[dirSouth] {
		p.water(3, 1, 0, 4, 2, 0)
	}

	if west.hasOpening[dirNorth] {
		p.water(3, 1, 7, 4, 2, 7)
	}

	if west.hasOpening[dirWest] {
		p.water(0, 1, 3, 0, 2, 4)
	}

	if east.hasOpening[dirSouth] {
		p.water(11, 1, 0, 12, 2, 0)
	}

	if east.hasOpening[dirNorth] {
		p.water(11, 1, 7, 12, 2, 7)
	}

	if east.hasOpening[dirEast] {
		p.water(15, 1, 3, 15, 2, 4)
	}

	if westUp.hasOpening[dirSouth] {
		p.water(3, 5, 0, 4, 6, 0)
	}

	if westUp.hasOpening[dirNorth] {
		p.water(3, 5, 7, 4, 6, 7)
	}

	if westUp.hasOpening[dirWest] {
		p.water(0, 5, 3, 0, 6, 4)
	}

	if eastUp.hasOpening[dirSouth] {
		p.water(11, 5, 0, 12, 6, 0)
	}

	if eastUp.hasOpening[dirNorth] {
		p.water(11, 5, 7, 12, 6, 7)
	}

	if eastUp.hasOpening[dirEast] {
		p.water(15, 5, 3, 15, 6, 4)
	}
}

func (p *monPiece) doubleYRoom() {
	if p.def.index/25 > 0 {
		p.defaultFloor(0, 0, p.def.hasOpening[dirDown])
	}

	above := p.def.connections[dirUp]
	if above.connections[dirUp] == nil {
		p.boxOnFill(1, 8, 1, 6, 8, 6, mGray)
	}

	p.box(0, 4, 0, 0, 4, 7, mLight)
	p.box(7, 4, 0, 7, 4, 7, mLight)
	p.box(1, 4, 0, 6, 4, 0, mLight)
	p.box(1, 4, 7, 6, 4, 7, mLight)
	p.box(2, 4, 1, 2, 4, 2, mLight)
	p.box(1, 4, 2, 1, 4, 2, mLight)
	p.box(5, 4, 1, 5, 4, 2, mLight)
	p.box(6, 4, 2, 6, 4, 2, mLight)
	p.box(2, 4, 5, 2, 4, 6, mLight)
	p.box(1, 4, 5, 1, 4, 5, mLight)
	p.box(5, 4, 5, 5, 4, 6, mLight)
	p.box(6, 4, 5, 6, 4, 5, mLight)
	definition := p.def

	for y := 1; y <= 5; y += 4 {
		z := 0
		if definition.hasOpening[dirSouth] {
			p.box(2, y, z, 2, y+2, z, mLight)
			p.box(5, y, z, 5, y+2, z, mLight)
			p.box(3, y+2, z, 4, y+2, z, mLight)
		} else {
			p.box(0, y, z, 7, y+2, z, mLight)
			p.box(0, y+1, z, 7, y+1, z, mGray)
		}

		var13 := 7
		if definition.hasOpening[dirNorth] {
			p.box(2, y, var13, 2, y+2, var13, mLight)
			p.box(5, y, var13, 5, y+2, var13, mLight)
			p.box(3, y+2, var13, 4, y+2, var13, mLight)
		} else {
			p.box(0, y, var13, 7, y+2, var13, mLight)
			p.box(0, y+1, var13, 7, y+1, var13, mGray)
		}

		x := 0
		if definition.hasOpening[dirWest] {
			p.box(x, y, 2, x, y+2, 2, mLight)
			p.box(x, y, 5, x, y+2, 5, mLight)
			p.box(x, y+2, 3, x, y+2, 4, mLight)
		} else {
			p.box(x, y, 0, x, y+2, 7, mLight)
			p.box(x, y+1, 0, x, y+1, 7, mGray)
		}

		var14 := 7
		if definition.hasOpening[dirEast] {
			p.box(var14, y, 2, var14, y+2, 2, mLight)
			p.box(var14, y, 5, var14, y+2, 5, mLight)
			p.box(var14, y+2, 3, var14, y+2, 4, mLight)
		} else {
			p.box(var14, y, 0, var14, y+2, 7, mLight)
			p.box(var14, y+1, 0, var14, y+1, 7, mGray)
		}

		definition = above
	}
}

func (p *monPiece) doubleYZRoom() {
	north := p.def.connections[dirNorth]
	south := p.def
	northUp := north.connections[dirUp]
	southUp := south.connections[dirUp]
	if p.def.index/25 > 0 {
		p.defaultFloor(0, 8, north.hasOpening[dirDown])
		p.defaultFloor(0, 0, south.hasOpening[dirDown])
	}

	if southUp.connections[dirUp] == nil {
		p.boxOnFill(1, 8, 1, 6, 8, 7, mGray)
	}

	if northUp.connections[dirUp] == nil {
		p.boxOnFill(1, 8, 8, 6, 8, 14, mGray)
	}

	for y := 1; y <= 7; y++ {
		block := mLight
		if y == 2 || y == 6 {
			block = mGray
		}

		p.box(0, y, 0, 0, y, 15, block)
		p.box(7, y, 0, 7, y, 15, block)
		p.box(1, y, 0, 6, y, 0, block)
		p.box(1, y, 15, 6, y, 15, block)
	}

	for y := 1; y <= 7; y++ {
		block := mBlack
		if y == 2 || y == 6 {
			block = mLamp
		}

		p.box(3, y, 7, 4, y, 8, block)
	}

	if south.hasOpening[dirSouth] {
		p.water(3, 1, 0, 4, 2, 0)
	}

	if south.hasOpening[dirEast] {
		p.water(7, 1, 3, 7, 2, 4)
	}

	if south.hasOpening[dirWest] {
		p.water(0, 1, 3, 0, 2, 4)
	}

	if north.hasOpening[dirNorth] {
		p.water(3, 1, 15, 4, 2, 15)
	}

	if north.hasOpening[dirWest] {
		p.water(0, 1, 11, 0, 2, 12)
	}

	if north.hasOpening[dirEast] {
		p.water(7, 1, 11, 7, 2, 12)
	}

	if southUp.hasOpening[dirSouth] {
		p.water(3, 5, 0, 4, 6, 0)
	}

	if southUp.hasOpening[dirEast] {
		p.water(7, 5, 3, 7, 6, 4)
		p.box(5, 4, 2, 6, 4, 5, mLight)
		p.box(6, 1, 2, 6, 3, 2, mLight)
		p.box(6, 1, 5, 6, 3, 5, mLight)
	}

	if southUp.hasOpening[dirWest] {
		p.water(0, 5, 3, 0, 6, 4)
		p.box(1, 4, 2, 2, 4, 5, mLight)
		p.box(1, 1, 2, 1, 3, 2, mLight)
		p.box(1, 1, 5, 1, 3, 5, mLight)
	}

	if northUp.hasOpening[dirNorth] {
		p.water(3, 5, 15, 4, 6, 15)
	}

	if northUp.hasOpening[dirWest] {
		p.water(0, 5, 11, 0, 6, 12)
		p.box(1, 4, 10, 2, 4, 13, mLight)
		p.box(1, 1, 10, 1, 3, 10, mLight)
		p.box(1, 1, 13, 1, 3, 13, mLight)
	}

	if northUp.hasOpening[dirEast] {
		p.water(7, 5, 11, 7, 6, 12)
		p.box(5, 4, 10, 6, 4, 13, mLight)
		p.box(6, 1, 10, 6, 3, 10, mLight)
		p.box(6, 1, 13, 6, 3, 13, mLight)
	}
}

func (p *monPiece) doubleZRoom() {
	north := p.def.connections[dirNorth]
	south := p.def
	if p.def.index/25 > 0 {
		p.defaultFloor(0, 8, north.hasOpening[dirDown])
		p.defaultFloor(0, 0, south.hasOpening[dirDown])
	}

	if south.connections[dirUp] == nil {
		p.boxOnFill(1, 4, 1, 6, 4, 7, mGray)
	}

	if north.connections[dirUp] == nil {
		p.boxOnFill(1, 4, 8, 6, 4, 14, mGray)
	}

	p.box(0, 3, 0, 0, 3, 15, mLight)
	p.box(7, 3, 0, 7, 3, 15, mLight)
	p.box(1, 3, 0, 7, 3, 0, mLight)
	p.box(1, 3, 15, 6, 3, 15, mLight)
	p.box(0, 2, 0, 0, 2, 15, mGray)
	p.box(7, 2, 0, 7, 2, 15, mGray)
	p.box(1, 2, 0, 7, 2, 0, mGray)
	p.box(1, 2, 15, 6, 2, 15, mGray)
	p.box(0, 1, 0, 0, 1, 15, mLight)
	p.box(7, 1, 0, 7, 1, 15, mLight)
	p.box(1, 1, 0, 7, 1, 0, mLight)
	p.box(1, 1, 15, 6, 1, 15, mLight)
	p.box(1, 1, 1, 1, 1, 2, mLight)
	p.box(6, 1, 1, 6, 1, 2, mLight)
	p.box(1, 3, 1, 1, 3, 2, mLight)
	p.box(6, 3, 1, 6, 3, 2, mLight)
	p.box(1, 1, 13, 1, 1, 14, mLight)
	p.box(6, 1, 13, 6, 1, 14, mLight)
	p.box(1, 3, 13, 1, 3, 14, mLight)
	p.box(6, 3, 13, 6, 3, 14, mLight)
	p.box(2, 1, 6, 2, 3, 6, mLight)
	p.box(5, 1, 6, 5, 3, 6, mLight)
	p.box(2, 1, 9, 2, 3, 9, mLight)
	p.box(5, 1, 9, 5, 3, 9, mLight)
	p.box(3, 2, 6, 4, 2, 6, mLight)
	p.box(3, 2, 9, 4, 2, 9, mLight)
	p.box(2, 2, 7, 2, 2, 8, mLight)
	p.box(5, 2, 7, 5, 2, 8, mLight)
	p.set(2, 2, 5, mLamp)
	p.set(5, 2, 5, mLamp)
	p.set(2, 2, 10, mLamp)
	p.set(5, 2, 10, mLamp)
	p.set(2, 3, 5, mLight)
	p.set(5, 3, 5, mLight)
	p.set(2, 3, 10, mLight)
	p.set(5, 3, 10, mLight)
	if south.hasOpening[dirSouth] {
		p.water(3, 1, 0, 4, 2, 0)
	}

	if south.hasOpening[dirEast] {
		p.water(7, 1, 3, 7, 2, 4)
	}

	if south.hasOpening[dirWest] {
		p.water(0, 1, 3, 0, 2, 4)
	}

	if north.hasOpening[dirNorth] {
		p.water(3, 1, 15, 4, 2, 15)
	}

	if north.hasOpening[dirWest] {
		p.water(0, 1, 11, 0, 2, 12)
	}

	if north.hasOpening[dirEast] {
		p.water(7, 1, 11, 7, 2, 12)
	}
}

func (p *monPiece) entryRoom() {
	p.box(0, 3, 0, 2, 3, 7, mLight)
	p.box(5, 3, 0, 7, 3, 7, mLight)
	p.box(0, 2, 0, 1, 2, 7, mLight)
	p.box(6, 2, 0, 7, 2, 7, mLight)
	p.box(0, 1, 0, 0, 1, 7, mLight)
	p.box(7, 1, 0, 7, 1, 7, mLight)
	p.box(0, 1, 7, 7, 3, 7, mLight)
	p.box(1, 1, 0, 2, 3, 0, mLight)
	p.box(5, 1, 0, 6, 3, 0, mLight)
	if p.def.hasOpening[dirNorth] {
		p.water(3, 1, 7, 4, 2, 7)
	}

	if p.def.hasOpening[dirWest] {
		p.water(0, 1, 3, 1, 2, 4)
	}

	if p.def.hasOpening[dirEast] {
		p.water(6, 1, 3, 7, 2, 4)
	}
}

func (p *monPiece) penthouse() {
	p.box(2, -1, 2, 11, -1, 11, mLight)
	p.box(0, -1, 0, 1, -1, 11, mGray)
	p.box(12, -1, 0, 13, -1, 11, mGray)
	p.box(2, -1, 0, 11, -1, 1, mGray)
	p.box(2, -1, 12, 11, -1, 13, mGray)
	p.box(0, 0, 0, 0, 0, 13, mLight)
	p.box(13, 0, 0, 13, 0, 13, mLight)
	p.box(1, 0, 0, 12, 0, 0, mLight)
	p.box(1, 0, 13, 12, 0, 13, mLight)

	for i := 2; i <= 11; i += 3 {
		p.set(0, 0, i, mLamp)
		p.set(13, 0, i, mLamp)
		p.set(i, 0, 0, mLamp)
	}

	p.box(2, 0, 3, 4, 0, 9, mLight)
	p.box(9, 0, 3, 11, 0, 9, mLight)
	p.box(4, 0, 9, 9, 0, 11, mLight)
	p.set(5, 0, 8, mLight)
	p.set(8, 0, 8, mLight)
	p.set(10, 0, 10, mLight)
	p.set(3, 0, 10, mLight)
	p.box(3, 0, 3, 3, 0, 7, mBlack)
	p.box(10, 0, 3, 10, 0, 7, mBlack)
	p.box(6, 0, 10, 7, 0, 10, mBlack)
	x := 3

	for i := 0; i < 2; i++ {
		for z := 2; z <= 8; z += 3 {
			p.box(x, 0, z, x, 2, z, mLight)
		}

		x = 10
	}

	p.box(5, 0, 10, 5, 2, 10, mLight)
	p.box(8, 0, 10, 8, 2, 10, mLight)
	p.box(6, -1, 7, 7, -1, 8, mBlack)
	p.water(6, -1, 3, 7, -1, 4)
	p.elder(6, 1, 6)
}

func (p *monPiece) simpleRoom() {
	if p.def.index/25 > 0 {
		p.defaultFloor(0, 0, p.def.hasOpening[dirDown])
	}

	if p.def.connections[dirUp] == nil {
		p.boxOnFill(1, 4, 1, 6, 4, 6, mGray)
	}

	centerPillar := p.design != 0 &&
		p.rng.Intn(2) == 0 &&
		!p.def.hasOpening[dirDown] &&
		!p.def.hasOpening[dirUp] &&
		p.def.countOpenings() > 1
	if p.design == 0 {
		p.box(0, 1, 0, 2, 1, 2, mLight)
		p.box(0, 3, 0, 2, 3, 2, mLight)
		p.box(0, 2, 0, 0, 2, 2, mGray)
		p.box(1, 2, 0, 2, 2, 0, mGray)
		p.set(1, 2, 1, mLamp)
		p.box(5, 1, 0, 7, 1, 2, mLight)
		p.box(5, 3, 0, 7, 3, 2, mLight)
		p.box(7, 2, 0, 7, 2, 2, mGray)
		p.box(5, 2, 0, 6, 2, 0, mGray)
		p.set(6, 2, 1, mLamp)
		p.box(0, 1, 5, 2, 1, 7, mLight)
		p.box(0, 3, 5, 2, 3, 7, mLight)
		p.box(0, 2, 5, 0, 2, 7, mGray)
		p.box(1, 2, 7, 2, 2, 7, mGray)
		p.set(1, 2, 6, mLamp)
		p.box(5, 1, 5, 7, 1, 7, mLight)
		p.box(5, 3, 5, 7, 3, 7, mLight)
		p.box(7, 2, 5, 7, 2, 7, mGray)
		p.box(5, 2, 7, 6, 2, 7, mGray)
		p.set(6, 2, 6, mLamp)
		if p.def.hasOpening[dirSouth] {
			p.box(3, 3, 0, 4, 3, 0, mLight)
		} else {
			p.box(3, 3, 0, 4, 3, 1, mLight)
			p.box(3, 2, 0, 4, 2, 0, mGray)
			p.box(3, 1, 0, 4, 1, 1, mLight)
		}

		if p.def.hasOpening[dirNorth] {
			p.box(3, 3, 7, 4, 3, 7, mLight)
		} else {
			p.box(3, 3, 6, 4, 3, 7, mLight)
			p.box(3, 2, 7, 4, 2, 7, mGray)
			p.box(3, 1, 6, 4, 1, 7, mLight)
		}

		if p.def.hasOpening[dirWest] {
			p.box(0, 3, 3, 0, 3, 4, mLight)
		} else {
			p.box(0, 3, 3, 1, 3, 4, mLight)
			p.box(0, 2, 3, 0, 2, 4, mGray)
			p.box(0, 1, 3, 1, 1, 4, mLight)
		}

		if p.def.hasOpening[dirEast] {
			p.box(7, 3, 3, 7, 3, 4, mLight)
		} else {
			p.box(6, 3, 3, 7, 3, 4, mLight)
			p.box(7, 2, 3, 7, 2, 4, mGray)
			p.box(6, 1, 3, 7, 1, 4, mLight)
		}
	} else if p.design == 1 {
		p.box(2, 1, 2, 2, 3, 2, mLight)
		p.box(2, 1, 5, 2, 3, 5, mLight)
		p.box(5, 1, 5, 5, 3, 5, mLight)
		p.box(5, 1, 2, 5, 3, 2, mLight)
		p.set(2, 2, 2, mLamp)
		p.set(2, 2, 5, mLamp)
		p.set(5, 2, 5, mLamp)
		p.set(5, 2, 2, mLamp)
		p.box(0, 1, 0, 1, 3, 0, mLight)
		p.box(0, 1, 1, 0, 3, 1, mLight)
		p.box(0, 1, 7, 1, 3, 7, mLight)
		p.box(0, 1, 6, 0, 3, 6, mLight)
		p.box(6, 1, 7, 7, 3, 7, mLight)
		p.box(7, 1, 6, 7, 3, 6, mLight)
		p.box(6, 1, 0, 7, 3, 0, mLight)
		p.box(7, 1, 1, 7, 3, 1, mLight)
		p.set(1, 2, 0, mGray)
		p.set(0, 2, 1, mGray)
		p.set(1, 2, 7, mGray)
		p.set(0, 2, 6, mGray)
		p.set(6, 2, 7, mGray)
		p.set(7, 2, 6, mGray)
		p.set(6, 2, 0, mGray)
		p.set(7, 2, 1, mGray)
		if !p.def.hasOpening[dirSouth] {
			p.box(1, 3, 0, 6, 3, 0, mLight)
			p.box(1, 2, 0, 6, 2, 0, mGray)
			p.box(1, 1, 0, 6, 1, 0, mLight)
		}

		if !p.def.hasOpening[dirNorth] {
			p.box(1, 3, 7, 6, 3, 7, mLight)
			p.box(1, 2, 7, 6, 2, 7, mGray)
			p.box(1, 1, 7, 6, 1, 7, mLight)
		}

		if !p.def.hasOpening[dirWest] {
			p.box(0, 3, 1, 0, 3, 6, mLight)
			p.box(0, 2, 1, 0, 2, 6, mGray)
			p.box(0, 1, 1, 0, 1, 6, mLight)
		}

		if !p.def.hasOpening[dirEast] {
			p.box(7, 3, 1, 7, 3, 6, mLight)
			p.box(7, 2, 1, 7, 2, 6, mGray)
			p.box(7, 1, 1, 7, 1, 6, mLight)
		}
	} else if p.design == 2 {
		p.box(0, 1, 0, 0, 1, 7, mLight)
		p.box(7, 1, 0, 7, 1, 7, mLight)
		p.box(1, 1, 0, 6, 1, 0, mLight)
		p.box(1, 1, 7, 6, 1, 7, mLight)
		p.box(0, 2, 0, 0, 2, 7, mBlack)
		p.box(7, 2, 0, 7, 2, 7, mBlack)
		p.box(1, 2, 0, 6, 2, 0, mBlack)
		p.box(1, 2, 7, 6, 2, 7, mBlack)
		p.box(0, 3, 0, 0, 3, 7, mLight)
		p.box(7, 3, 0, 7, 3, 7, mLight)
		p.box(1, 3, 0, 6, 3, 0, mLight)
		p.box(1, 3, 7, 6, 3, 7, mLight)
		p.box(0, 1, 3, 0, 2, 4, mBlack)
		p.box(7, 1, 3, 7, 2, 4, mBlack)
		p.box(3, 1, 0, 4, 2, 0, mBlack)
		p.box(3, 1, 7, 4, 2, 7, mBlack)
		if p.def.hasOpening[dirSouth] {
			p.water(3, 1, 0, 4, 2, 0)
		}

		if p.def.hasOpening[dirNorth] {
			p.water(3, 1, 7, 4, 2, 7)
		}

		if p.def.hasOpening[dirWest] {
			p.water(0, 1, 3, 0, 2, 4)
		}

		if p.def.hasOpening[dirEast] {
			p.water(7, 1, 3, 7, 2, 4)
		}
	}

	if centerPillar {
		p.box(3, 1, 3, 4, 1, 4, mLight)
		p.box(3, 2, 3, 4, 2, 4, mGray)
		p.box(3, 3, 3, 4, 3, 4, mLight)
	}
}

func (p *monPiece) simpleTopRoom() {
	if p.def.index/25 > 0 {
		p.defaultFloor(0, 0, p.def.hasOpening[dirDown])
	}

	if p.def.connections[dirUp] == nil {
		p.boxOnFill(1, 4, 1, 6, 4, 6, mGray)
	}

	for x := 1; x <= 6; x++ {
		for z := 1; z <= 6; z++ {
			if p.rng.Intn(3) != 0 {
				y0 := 2
				if p.rng.Intn(4) != 0 {
					y0 = 3
				}
				wetSponge := mWetSponge
				p.box(x, y0, z, x, 3, z, wetSponge)
			}
		}
	}

	p.box(0, 1, 0, 0, 1, 7, mLight)
	p.box(7, 1, 0, 7, 1, 7, mLight)
	p.box(1, 1, 0, 6, 1, 0, mLight)
	p.box(1, 1, 7, 6, 1, 7, mLight)
	p.box(0, 2, 0, 0, 2, 7, mBlack)
	p.box(7, 2, 0, 7, 2, 7, mBlack)
	p.box(1, 2, 0, 6, 2, 0, mBlack)
	p.box(1, 2, 7, 6, 2, 7, mBlack)
	p.box(0, 3, 0, 0, 3, 7, mLight)
	p.box(7, 3, 0, 7, 3, 7, mLight)
	p.box(1, 3, 0, 6, 3, 0, mLight)
	p.box(1, 3, 7, 6, 3, 7, mLight)
	p.box(0, 1, 3, 0, 2, 4, mBlack)
	p.box(7, 1, 3, 7, 2, 4, mBlack)
	p.box(3, 1, 0, 4, 2, 0, mBlack)
	p.box(3, 1, 7, 4, 2, 7, mBlack)
	if p.def.hasOpening[dirSouth] {
		p.water(3, 1, 0, 4, 2, 0)
	}
}

func (p *monPiece) wingRoom() {
	if p.design == 0 {
		for i := 0; i < 4; i++ {
			p.box(10-i, 3-i, 20-i, 12+i, 3-i, 20, mLight)
		}

		p.box(7, 0, 6, 15, 0, 16, mLight)
		p.box(6, 0, 6, 6, 3, 20, mLight)
		p.box(16, 0, 6, 16, 3, 20, mLight)
		p.box(7, 1, 7, 7, 1, 20, mLight)
		p.box(15, 1, 7, 15, 1, 20, mLight)
		p.box(7, 1, 6, 9, 3, 6, mLight)
		p.box(13, 1, 6, 15, 3, 6, mLight)
		p.box(8, 1, 7, 9, 1, 7, mLight)
		p.box(13, 1, 7, 14, 1, 7, mLight)
		p.box(9, 0, 5, 13, 0, 5, mLight)
		p.box(10, 0, 7, 12, 0, 7, mBlack)
		p.box(8, 0, 10, 8, 0, 12, mBlack)
		p.box(14, 0, 10, 14, 0, 12, mBlack)

		for z := 18; z >= 7; z -= 3 {
			p.set(6, 3, z, mLamp)
			p.set(16, 3, z, mLamp)
		}

		p.set(10, 0, 10, mLamp)
		p.set(12, 0, 10, mLamp)
		p.set(10, 0, 12, mLamp)
		p.set(12, 0, 12, mLamp)
		p.set(8, 3, 6, mLamp)
		p.set(14, 3, 6, mLamp)
		p.set(4, 2, 4, mLight)
		p.set(4, 1, 4, mLamp)
		p.set(4, 0, 4, mLight)
		p.set(18, 2, 4, mLight)
		p.set(18, 1, 4, mLamp)
		p.set(18, 0, 4, mLight)
		p.set(4, 2, 18, mLight)
		p.set(4, 1, 18, mLamp)
		p.set(4, 0, 18, mLight)
		p.set(18, 2, 18, mLight)
		p.set(18, 1, 18, mLamp)
		p.set(18, 0, 18, mLight)
		p.set(9, 7, 20, mLight)
		p.set(13, 7, 20, mLight)
		p.box(6, 0, 21, 7, 4, 21, mLight)
		p.box(15, 0, 21, 16, 4, 21, mLight)
		p.elder(11, 2, 16)
	} else if p.design == 1 {
		p.box(9, 3, 18, 13, 3, 20, mLight)
		p.box(9, 0, 18, 9, 2, 18, mLight)
		p.box(13, 0, 18, 13, 2, 18, mLight)
		x := 9

		for i := 0; i < 2; i++ {
			p.set(x, 6, 20, mLight)
			p.set(x, 5, 20, mLamp)
			p.set(x, 4, 20, mLight)
			x = 13
		}

		p.box(7, 3, 7, 15, 3, 14, mLight)
		var14 := 10

		for i := 0; i < 2; i++ {
			p.box(var14, 0, 10, var14, 6, 10, mLight)
			p.box(var14, 0, 12, var14, 6, 12, mLight)
			p.set(var14, 0, 10, mLamp)
			p.set(var14, 0, 12, mLamp)
			p.set(var14, 4, 10, mLamp)
			p.set(var14, 4, 12, mLamp)
			var14 = 12
		}

		var14 = 8

		for i := 0; i < 2; i++ {
			p.box(var14, 0, 7, var14, 2, 7, mLight)
			p.box(var14, 0, 14, var14, 2, 14, mLight)
			var14 = 14
		}

		p.box(8, 3, 8, 8, 3, 13, mBlack)
		p.box(14, 3, 8, 14, 3, 13, mBlack)
		p.elder(11, 5, 13)
	}
}
