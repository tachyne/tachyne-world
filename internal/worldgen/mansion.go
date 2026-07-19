package worldgen

// Woodland mansion — a faithful port of vanilla WoodlandMansionPieces. A grid
// solver (MansionGrid: an 11×11 floor plan with recursive corridors, three
// floors, and bit-flagged room sizes) drives a placer that stamps the real
// vanilla room templates (entrance, corridors, walls, doors, 1×1/1×2/2×2 rooms,
// staircases, roof) with per-piece rotation + mirror. The global orientation is
// fixed north (the mansion is not symmetric, but any orientation is valid), so
// the frame directions are identity while individual pieces still rotate/mirror.
//
// Deferred vs vanilla: the illager occupants (evokers/vindicators/allays) come
// from "Mage"/"Warrior" DATA markers that this port leaves as air — hostile
// seeding is a server concern; and the RNG is tachyne's, so a given seed's floor
// plan differs from vanilla's (as with every tachyne structure).

// ---- direction helpers (MC Direction subset) --------------------------------

type mdir struct{ sx, sz, up int }

var (
	mNorth = mdir{0, -1, 0}
	mEast  = mdir{1, 0, 0}
	mSouth = mdir{0, 1, 0}
	mWest  = mdir{-1, 0, 0}
	mUp    = mdir{0, 0, 1}
	mHoriz = []mdir{mNorth, mEast, mSouth, mWest}
)

func (d mdir) eq(o mdir) bool { return d == o }
func (d mdir) cw() mdir       { return mdir{-d.sz, d.sx, 0} }
func (d mdir) ccw() mdir      { return mdir{d.sz, -d.sx, 0} }
func (d mdir) opp() mdir      { return mdir{-d.sx, -d.sz, 0} }

// from2D maps MC's 2D data value (0=S,1=W,2=N,3=E) to a direction.
func from2D(n int) mdir { return []mdir{mSouth, mWest, mNorth, mEast}[n&3] }

// mrotDir applies a rotation (0=NONE,1=CW90,2=CW180,3=CCW90) to a horizontal dir.
func mrotDir(rot int, d mdir) mdir {
	if d.up != 0 {
		return d
	}
	switch rot & 3 {
	case 1:
		return d.cw()
	case 2:
		return d.opp()
	case 3:
		return d.ccw()
	}
	return d
}

func rotCompose(a, b int) int { return (a + b) & 3 }

// ---- SimpleGrid -------------------------------------------------------------

type simpleGrid struct {
	w, h, outside int
	g             [][]int
}

func newSimpleGrid(w, h, outside int) *simpleGrid {
	g := make([][]int, w)
	for i := range g {
		g[i] = make([]int, h)
	}
	return &simpleGrid{w, h, outside, g}
}

func (s *simpleGrid) set(x, y, v int) {
	if x >= 0 && x < s.w && y >= 0 && y < s.h {
		s.g[x][y] = v
	}
}

func (s *simpleGrid) setBox(x0, y0, x1, y1, v int) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			s.set(x, y, v)
		}
	}
}

func (s *simpleGrid) get(x, y int) int {
	if x >= 0 && x < s.w && y >= 0 && y < s.h {
		return s.g[x][y]
	}
	return s.outside
}

func (s *simpleGrid) setif(x, y, want, v int) {
	if s.get(x, y) == want {
		s.set(x, y, v)
	}
}

func (s *simpleGrid) edgesTo(x, y, v int) bool {
	return s.get(x-1, y) == v || s.get(x+1, y) == v || s.get(x, y+1) == v || s.get(x, y-1) == v
}

// ---- MansionGrid (floor-plan solver) ----------------------------------------

type mansionGrid struct {
	rng            *jigsawRNG
	entranceX      int
	entranceY      int
	baseGrid       *simpleGrid
	thirdFloorGrid *simpleGrid
	floorRooms     [3]*simpleGrid
}

func gridIsHouse(g *simpleGrid, x, y int) bool {
	v := g.get(x, y)
	return v == 1 || v == 2 || v == 3 || v == 4
}

func (m *mansionGrid) isRoomId(g *simpleGrid, x, y, floor, id int) bool {
	return (m.floorRooms[floor].get(x, y) & 0xFFFF) == id
}

func (m *mansionGrid) get1x2RoomDirection(g *simpleGrid, x, y, floor, id int) (mdir, bool) {
	for _, d := range mHoriz {
		if m.isRoomId(g, x+d.sx, y+d.sz, floor, id) {
			return d, true
		}
	}
	return mdir{}, false
}

func newMansionGrid(rng *jigsawRNG) *mansionGrid {
	m := &mansionGrid{rng: rng, entranceX: 7, entranceY: 4}
	ex, ey := m.entranceX, m.entranceY
	m.baseGrid = newSimpleGrid(11, 11, 5)
	m.baseGrid.setBox(ex, ey, ex+1, ey+1, 3)
	m.baseGrid.setBox(ex-1, ey, ex-1, ey+1, 2)
	m.baseGrid.setBox(ex+2, ey-2, ex+3, ey+3, 5)
	m.baseGrid.setBox(ex+1, ey-2, ex+1, ey-1, 1)
	m.baseGrid.setBox(ex+1, ey+2, ex+1, ey+3, 1)
	m.baseGrid.set(ex-1, ey-1, 1)
	m.baseGrid.set(ex-1, ey+2, 1)
	m.baseGrid.setBox(0, 0, 11, 1, 5)
	m.baseGrid.setBox(0, 9, 11, 11, 5)
	m.recursiveCorridor(m.baseGrid, ex, ey-2, mWest, 6)
	m.recursiveCorridor(m.baseGrid, ex, ey+3, mWest, 6)
	m.recursiveCorridor(m.baseGrid, ex-2, ey-1, mWest, 3)
	m.recursiveCorridor(m.baseGrid, ex-2, ey+2, mWest, 3)
	for m.cleanEdges(m.baseGrid) {
	}
	m.floorRooms[0] = newSimpleGrid(11, 11, 5)
	m.floorRooms[1] = newSimpleGrid(11, 11, 5)
	m.floorRooms[2] = newSimpleGrid(11, 11, 5)
	m.identifyRooms(m.baseGrid, m.floorRooms[0])
	m.identifyRooms(m.baseGrid, m.floorRooms[1])
	m.floorRooms[0].setBox(ex+1, ey, ex+1, ey+1, 0x800000)
	m.floorRooms[1].setBox(ex+1, ey, ex+1, ey+1, 0x800000)
	m.thirdFloorGrid = newSimpleGrid(m.baseGrid.w, m.baseGrid.h, 5)
	m.setupThirdFloor()
	m.identifyRooms(m.thirdFloorGrid, m.floorRooms[2])
	return m
}

func (m *mansionGrid) recursiveCorridor(g *simpleGrid, x, y int, d mdir, depth int) {
	if depth <= 0 {
		return
	}
	g.set(x, y, 1)
	g.setif(x+d.sx, y+d.sz, 0, 1)
	for i := 0; i < 8; i++ {
		d2 := from2D(m.rng.intn(4))
		if d2.eq(d.opp()) || (d2.eq(mEast) && m.rng.intn(2) == 0) {
			continue
		}
		nx := x + d.sx
		ny := y + d.sz
		if g.get(nx+d2.sx, ny+d2.sz) != 0 || g.get(nx+d2.sx*2, ny+d2.sz*2) != 0 {
			continue
		}
		m.recursiveCorridor(g, x+d.sx+d2.sx, y+d.sz+d2.sz, d2, depth-1)
		break
	}
	dc := d.cw()
	dcc := d.ccw()
	g.setif(x+dc.sx, y+dc.sz, 0, 2)
	g.setif(x+dcc.sx, y+dcc.sz, 0, 2)
	g.setif(x+d.sx+dc.sx, y+d.sz+dc.sz, 0, 2)
	g.setif(x+d.sx+dcc.sx, y+d.sz+dcc.sz, 0, 2)
	g.setif(x+d.sx*2, y+d.sz*2, 0, 2)
	g.setif(x+dc.sx*2, y+dc.sz*2, 0, 2)
	g.setif(x+dcc.sx*2, y+dcc.sz*2, 0, 2)
}

func (m *mansionGrid) cleanEdges(g *simpleGrid) bool {
	changed := false
	for y := 0; y < g.h; y++ {
		for x := 0; x < g.w; x++ {
			if g.get(x, y) != 0 {
				continue
			}
			n := 0
			if gridIsHouse(g, x+1, y) {
				n++
			}
			if gridIsHouse(g, x-1, y) {
				n++
			}
			if gridIsHouse(g, x, y+1) {
				n++
			}
			if gridIsHouse(g, x, y-1) {
				n++
			}
			if n >= 3 {
				g.set(x, y, 2)
				changed = true
				continue
			}
			if n != 2 {
				continue
			}
			n2 := 0
			if gridIsHouse(g, x+1, y+1) {
				n2++
			}
			if gridIsHouse(g, x-1, y+1) {
				n2++
			}
			if gridIsHouse(g, x+1, y-1) {
				n2++
			}
			if gridIsHouse(g, x-1, y-1) {
				n2++
			}
			if n2 > 1 {
				continue
			}
			g.set(x, y, 2)
			changed = true
		}
	}
	return changed
}

func (m *mansionGrid) setupThirdFloor() {
	type cell struct{ x, y int }
	var cands []cell
	src := m.floorRooms[1]
	for y := 0; y < m.thirdFloorGrid.h; y++ {
		for x := 0; x < m.thirdFloorGrid.w; x++ {
			v := src.get(x, y)
			if v&0xF0000 != 131072 || v&0x200000 != 0x200000 {
				continue
			}
			cands = append(cands, cell{x, y})
		}
	}
	if len(cands) == 0 {
		m.thirdFloorGrid.setBox(0, 0, m.thirdFloorGrid.w, m.thirdFloorGrid.h, 5)
		return
	}
	c := cands[m.rng.intn(len(cands))]
	v := src.get(c.x, c.y)
	src.set(c.x, c.y, v|0x400000)
	d, _ := m.get1x2RoomDirection(m.baseGrid, c.x, c.y, 1, v&0xFFFF)
	nx := c.x + d.sx
	ny := c.y + d.sz
	for y := 0; y < m.thirdFloorGrid.h; y++ {
		for x := 0; x < m.thirdFloorGrid.w; x++ {
			if !gridIsHouse(m.baseGrid, x, y) {
				m.thirdFloorGrid.set(x, y, 5)
				continue
			}
			if x == c.x && y == c.y {
				m.thirdFloorGrid.set(x, y, 3)
				continue
			}
			if x != nx || y != ny {
				continue
			}
			m.thirdFloorGrid.set(x, y, 3)
			m.floorRooms[2].set(x, y, 0x800000)
		}
	}
	var dirs []mdir
	for _, d2 := range mHoriz {
		if m.thirdFloorGrid.get(nx+d2.sx, ny+d2.sz) == 0 {
			dirs = append(dirs, d2)
		}
	}
	if len(dirs) == 0 {
		m.thirdFloorGrid.setBox(0, 0, m.thirdFloorGrid.w, m.thirdFloorGrid.h, 5)
		src.set(c.x, c.y, v)
		return
	}
	d3 := dirs[m.rng.intn(len(dirs))]
	m.recursiveCorridor(m.thirdFloorGrid, nx+d3.sx, ny+d3.sz, d3, 4)
	for m.cleanEdges(m.thirdFloorGrid) {
	}
}

func (m *mansionGrid) identifyRooms(src, dst *simpleGrid) {
	type cell struct{ x, y int }
	var cells []cell
	for y := 0; y < src.h; y++ {
		for x := 0; x < src.w; x++ {
			if src.get(x, y) == 2 {
				cells = append(cells, cell{x, y})
			}
		}
	}
	mansionShuffle(cells, m.rng)
	id := 10
	for _, cc := range cells {
		x, y := cc.x, cc.y
		if dst.get(x, y) != 0 {
			continue
		}
		x0, x1, y0, y1 := x, x, y, y
		size := 65536
		switch {
		case dst.get(x+1, y) == 0 && dst.get(x, y+1) == 0 && dst.get(x+1, y+1) == 0 &&
			src.get(x+1, y) == 2 && src.get(x, y+1) == 2 && src.get(x+1, y+1) == 2:
			x1++
			y1++
			size = 262144
		case dst.get(x-1, y) == 0 && dst.get(x, y+1) == 0 && dst.get(x-1, y+1) == 0 &&
			src.get(x-1, y) == 2 && src.get(x, y+1) == 2 && src.get(x-1, y+1) == 2:
			x0--
			y1++
			size = 262144
		case dst.get(x-1, y) == 0 && dst.get(x, y-1) == 0 && dst.get(x-1, y-1) == 0 &&
			src.get(x-1, y) == 2 && src.get(x, y-1) == 2 && src.get(x-1, y-1) == 2:
			x0--
			y0--
			size = 262144
		case dst.get(x+1, y) == 0 && src.get(x+1, y) == 2:
			x1++
			size = 131072
		case dst.get(x, y+1) == 0 && src.get(x, y+1) == 2:
			y1++
			size = 131072
		case dst.get(x-1, y) == 0 && src.get(x-1, y) == 2:
			x0--
			size = 131072
		case dst.get(x, y-1) == 0 && src.get(x, y-1) == 2:
			y0--
			size = 131072
		}
		dx := x0
		if m.rng.intn(2) == 0 {
			dx = x1
		}
		dy := y0
		if m.rng.intn(2) == 0 {
			dy = y1
		}
		doorFlag := 0x200000
		if !src.edgesTo(dx, dy, 1) {
			if dx == x0 {
				dx = x1
			} else {
				dx = x0
			}
			if dy == y0 {
				dy = y1
			} else {
				dy = y0
			}
			if !src.edgesTo(dx, dy, 1) {
				if dy == y0 {
					dy = y1
				} else {
					dy = y0
				}
				if !src.edgesTo(dx, dy, 1) {
					if dx == x0 {
						dx = x1
					} else {
						dx = x0
					}
					if dy == y0 {
						dy = y1
					} else {
						dy = y0
					}
					if !src.edgesTo(dx, dy, 1) {
						doorFlag = 0
						dx = x0
						dy = y0
					}
				}
			}
		}
		for i := y0; i <= y1; i++ {
			for j := x0; j <= x1; j++ {
				if j == dx && i == dy {
					dst.set(j, i, 0x100000|doorFlag|size|id)
				} else {
					dst.set(j, i, size|id)
				}
			}
		}
		id++
	}
}

// mansionShuffle is a Fisher-Yates over cells with the mansion RNG.
func mansionShuffle[T any](a []T, rng *jigsawRNG) {
	for i := len(a) - 1; i > 0; i-- {
		j := rng.intn(i + 1)
		a[i], a[j] = a[j], a[i]
	}
}

func mrandBool(rng *jigsawRNG) bool { return rng.intn(2) == 0 }
