package worldgen

import "sync"

// Mineshafts — a port of vanilla's mineshaft generator (MineshaftPieces).
// A room at the site sends corridors out of its four walls; each corridor
// ends in another corridor, a crossing (one floor or two) or a flight of
// stairs down, and branches sideways every five blocks. Growth stops eight
// pieces deep or 80 blocks from the room. Corridors carry support frames
// (fence posts under a plank beam, now and then a torch), cobwebs in the
// corners, planks bridging any gap in the floor, log pillars down to solid
// ground or chains up to the ceiling under their first and last supports,
// sometimes a rail line, one in a hundred sections a chest minecart, and
// one corridor in about thirty-five is a cave spider nest with a spawner in
// a blanket of webs. A piece drawn where water or lava touches its box, or
// in the deep dark, is left out of that chunk.
//
// Placement is vanilla's: any chunk may start one, with a one-in-250
// chance. A normal mineshaft is sunk below sea level; in the badlands it is
// the mesa variant, built of dark oak and lifted to between sea level and
// the surface, where it breaks out of the terrain.

const (
	shaftOdds    = 0.004 // per chunk (the mineshafts structure set's frequency)
	msMaxDepth   = 8
	msReach      = 80
	msStartY     = 50
	msChunkReach = 7 // chunks: the reach plus the longest piece
	msMaxPillar  = 20
	msMaxChain   = 50
	msCartTable  = "chests/abandoned_mineshaft"
)

// Structure block states (the old corridor carver's names, still used by
// the dungeon and a few tests).
var (
	OakPlanks          = blockBase("oak_planks")
	Lava               = blockBase("lava")
	Cobblestone        = blockBase("cobblestone")
	Cobweb             = blockBase("cobweb")
	MossyCobblestone   = blockBase("mossy_cobblestone")
	Spawner            = blockBase("spawner")
	ChestNorth         = blockBase("chest") + 1
	RailFlat           = blockBase("rail") + 1 // north_south, dry
	OakFence           = blockBase("oak_fence") + 31
	StoneBricks        = blockBase("stone_bricks")
	MossyStoneBricks   = blockBase("mossy_stone_bricks")
	CrackedStoneBricks = blockBase("cracked_stone_bricks")
)

type msKind int

const (
	msRoom msKind = iota
	msCorridor
	msCrossing
	msStairs
	msKinds
)

var msKindNames = [msKinds]string{"room", "corridor", "crossing", "stairs"}

// msWood is MineshaftStructure.Type's three blocks.
type msWood struct{ log, planks, fence string }

var (
	msNormal = msWood{"oak_log", "oak_planks", "oak_fence"}
	msMesa   = msWood{"dark_oak_log", "dark_oak_planks", "dark_oak_fence"}
)

// msPiece is one placed mineshaft piece. Rooms and crossings have no facing
// and work in world coordinates.
type msPiece struct {
	opiece
	kind      msKind
	depth     int
	rails     bool   // corridor: a rail line
	spider    bool   // corridor: a cave spider nest
	sections  int    // corridor: five-block sections
	twoFloor  bool   // crossing
	xdir      fdir   // crossing: the direction it was entered by
	entrances []fbox // room: where its corridors leave
	salt      uint64
}

type msGen struct {
	rng    *jigsawRNG
	pieces []*msPiece
	start  *msPiece
}

func (g *msGen) collides(b fbox) bool {
	for _, p := range g.pieces {
		if p.box.intersects(b) {
			return true
		}
	}
	return false
}

// dirBox is a box given for a north-facing piece at a foot, turned to dir
// the way the mineshaft pieces spell it out (their four switch arms).
func dirBox(fx, fy, fz int, dir fdir, north, south, west, east fbox) fbox {
	b := north
	switch dir {
	case fSouth:
		b = south
	case fWest:
		b = west
	case fEast:
		b = east
	}
	return fbox{b.x0 + fx, b.y0 + fy, b.z0 + fz, b.x1 + fx, b.y1 + fy, b.z1 + fz}
}

// create is createRandomShaftPiece.
func (g *msGen) create(fx, fy, fz int, dir fdir, depth int) *msPiece {
	sel := g.rng.intn(100)
	var p *msPiece
	switch {
	case sel >= 80:
		y1 := 2
		if g.rng.intn(4) == 0 {
			y1 = 6
		}
		box := dirBox(fx, fy, fz, dir,
			fbox{-1, 0, -4, 3, y1, 0}, fbox{-1, 0, 0, 3, y1, 4}, fbox{-4, 0, -1, 0, y1, 3}, fbox{0, 0, -1, 4, y1, 3})
		if g.collides(box) {
			return nil
		}
		p = &msPiece{opiece: opiece{box: box}, kind: msCrossing, xdir: dir, twoFloor: y1 > 2}
	case sel >= 70:
		box := dirBox(fx, fy, fz, dir,
			fbox{0, -5, -8, 2, 2, 0}, fbox{0, -5, 0, 2, 2, 8}, fbox{-8, -5, 0, 0, 2, 2}, fbox{0, -5, 0, 8, 2, 2})
		if g.collides(box) {
			return nil
		}
		p = &msPiece{opiece: newOPiece(box, dir), kind: msStairs}
	default:
		var box fbox
		ok := false
		for n := g.rng.intn(3) + 2; n > 0; n-- {
			l := n * 5
			box = dirBox(fx, fy, fz, dir,
				fbox{0, 0, -(l - 1), 2, 2, 0}, fbox{0, 0, 0, 2, 2, l - 1}, fbox{-(l - 1), 0, 0, 0, 2, 2}, fbox{0, 0, 0, l - 1, 2, 2})
			if !g.collides(box) {
				ok = true
				break
			}
		}
		if !ok {
			return nil
		}
		p = &msPiece{opiece: newOPiece(box, dir), kind: msCorridor}
		p.rails = g.rng.intn(3) == 0
		p.spider = !p.rails && g.rng.intn(23) == 0
		if dir == fNorth || dir == fSouth {
			p.sections = (box.z1 - box.z0 + 1) / 5
		} else {
			p.sections = (box.x1 - box.x0 + 1) / 5
		}
	}
	p.depth = depth
	p.salt = g.rng.next()
	return p
}

// add is generateAndAddPiece: the piece grows its own children at once.
func (g *msGen) add(fx, fy, fz int, dir fdir, depth int) *msPiece {
	if depth > msMaxDepth {
		return nil
	}
	if abs(fx-g.start.box.x0) > msReach || abs(fz-g.start.box.z0) > msReach {
		return nil
	}
	p := g.create(fx, fy, fz, dir, depth+1)
	if p != nil {
		g.pieces = append(g.pieces, p)
		g.addChildren(p)
	}
	return p
}

func (g *msGen) addChildren(p *msPiece) {
	b, d := p.box, p.depth
	switch p.kind {
	case msRoom:
		g.roomChildren(p)
	case msCorridor:
		end := g.rng.intn(4)
		y := func() int { return b.y0 - 1 + g.rng.intn(3) }
		switch p.dir {
		case fNorth:
			switch {
			case end <= 1:
				g.add(b.x0, y(), b.z0-1, fNorth, d)
			case end == 2:
				g.add(b.x0-1, y(), b.z0, fWest, d)
			default:
				g.add(b.x1+1, y(), b.z0, fEast, d)
			}
		case fSouth:
			switch {
			case end <= 1:
				g.add(b.x0, y(), b.z1+1, fSouth, d)
			case end == 2:
				g.add(b.x0-1, y(), b.z1-3, fWest, d)
			default:
				g.add(b.x1+1, y(), b.z1-3, fEast, d)
			}
		case fWest:
			switch {
			case end <= 1:
				g.add(b.x0-1, y(), b.z0, fWest, d)
			case end == 2:
				g.add(b.x0, y(), b.z0-1, fNorth, d)
			default:
				g.add(b.x0, y(), b.z1+1, fSouth, d)
			}
		case fEast:
			switch {
			case end <= 1:
				g.add(b.x1+1, y(), b.z0, fEast, d)
			case end == 2:
				g.add(b.x1-3, y(), b.z0-1, fNorth, d)
			default:
				g.add(b.x1-3, y(), b.z1+1, fSouth, d)
			}
		}
		if d < msMaxDepth {
			if p.dir != fNorth && p.dir != fSouth {
				for x := b.x0 + 3; x+3 <= b.x1; x += 5 {
					switch g.rng.intn(5) {
					case 0:
						g.add(x, b.y0, b.z0-1, fNorth, d+1)
					case 1:
						g.add(x, b.y0, b.z1+1, fSouth, d+1)
					}
				}
			} else {
				for z := b.z0 + 3; z+3 <= b.z1; z += 5 {
					switch g.rng.intn(5) {
					case 0:
						g.add(b.x0-1, b.y0, z, fWest, d+1)
					case 1:
						g.add(b.x1+1, b.y0, z, fEast, d+1)
					}
				}
			}
		}
	case msCrossing:
		switch p.xdir {
		case fSouth:
			g.add(b.x0+1, b.y0, b.z1+1, fSouth, d)
			g.add(b.x0-1, b.y0, b.z0+1, fWest, d)
			g.add(b.x1+1, b.y0, b.z0+1, fEast, d)
		case fWest:
			g.add(b.x0+1, b.y0, b.z0-1, fNorth, d)
			g.add(b.x0+1, b.y0, b.z1+1, fSouth, d)
			g.add(b.x0-1, b.y0, b.z0+1, fWest, d)
		case fEast:
			g.add(b.x0+1, b.y0, b.z0-1, fNorth, d)
			g.add(b.x0+1, b.y0, b.z1+1, fSouth, d)
			g.add(b.x1+1, b.y0, b.z0+1, fEast, d)
		default:
			g.add(b.x0+1, b.y0, b.z0-1, fNorth, d)
			g.add(b.x0-1, b.y0, b.z0+1, fWest, d)
			g.add(b.x1+1, b.y0, b.z0+1, fEast, d)
		}
		if p.twoFloor {
			if g.rng.intn(2) == 0 {
				g.add(b.x0+1, b.y0+4, b.z0-1, fNorth, d)
			}
			if g.rng.intn(2) == 0 {
				g.add(b.x0-1, b.y0+4, b.z0+1, fWest, d)
			}
			if g.rng.intn(2) == 0 {
				g.add(b.x1+1, b.y0+4, b.z0+1, fEast, d)
			}
			if g.rng.intn(2) == 0 {
				g.add(b.x0+1, b.y0+4, b.z1+1, fSouth, d)
			}
		}
	case msStairs:
		switch p.dir {
		case fSouth:
			g.add(b.x0, b.y0, b.z1+1, fSouth, d)
		case fWest:
			g.add(b.x0-1, b.y0, b.z0, fWest, d)
		case fEast:
			g.add(b.x1+1, b.y0, b.z0, fEast, d)
		default:
			g.add(b.x0, b.y0, b.z0-1, fNorth, d)
		}
	}
}

// roomChildren walks each wall of the room, opening corridors at random
// spacing and remembering where each one leaves.
func (g *msGen) roomChildren(p *msPiece) {
	b := p.box
	hs := b.y1 - b.y0 + 1 - 3 - 1
	if hs <= 0 {
		hs = 1
	}
	xs, zs := b.x1-b.x0+1, b.z1-b.z0+1
	for pos := 0; pos < xs; {
		pos += g.rng.intn(xs)
		if pos+3 > xs {
			break
		}
		if c := g.add(b.x0+pos, b.y0+g.rng.intn(hs)+1, b.z0-1, fNorth, p.depth); c != nil {
			p.entrances = append(p.entrances, fbox{c.box.x0, c.box.y0, b.z0, c.box.x1, c.box.y1, b.z0 + 1})
		}
		pos += 4
	}
	for pos := 0; pos < xs; {
		pos += g.rng.intn(xs)
		if pos+3 > xs {
			break
		}
		if c := g.add(b.x0+pos, b.y0+g.rng.intn(hs)+1, b.z1+1, fSouth, p.depth); c != nil {
			p.entrances = append(p.entrances, fbox{c.box.x0, c.box.y0, b.z1 - 1, c.box.x1, c.box.y1, b.z1})
		}
		pos += 4
	}
	for pos := 0; pos < zs; {
		pos += g.rng.intn(zs)
		if pos+3 > zs {
			break
		}
		if c := g.add(b.x0-1, b.y0+g.rng.intn(hs)+1, b.z0+pos, fWest, p.depth); c != nil {
			p.entrances = append(p.entrances, fbox{b.x0, c.box.y0, c.box.z0, b.x0 + 1, c.box.y1, c.box.z1})
		}
		pos += 4
	}
	for pos := 0; pos < zs; {
		pos += g.rng.intn(zs)
		if pos+3 > zs {
			break
		}
		if c := g.add(b.x1+1, b.y0+g.rng.intn(hs)+1, b.z0+pos, fEast, p.depth); c != nil {
			p.entrances = append(p.entrances, fbox{b.x1 - 1, c.box.y0, c.box.z0, b.x1, c.box.y1, c.box.z1})
		}
		pos += 4
	}
}

// assembleMineshaft lays out the mineshaft starting in chunk (cx,cz).
func (gen *Generator) assembleMineshaft(cx, cz int, mesa bool) []*msPiece {
	g := &msGen{rng: newJigsawRNG(gen.seed^0x4D494E45, cx, cz)}
	x, z := cx*16+2, cz*16+2
	x1 := x + 7 + g.rng.intn(6)
	y1 := msStartY + 4 + g.rng.intn(6)
	z1 := z + 7 + g.rng.intn(6)
	room := &msPiece{opiece: opiece{box: fbox{x, msStartY, z, x1, y1, z1}}, kind: msRoom, salt: 0x4D52}
	g.start = room
	g.pieces = append(g.pieces, room)
	g.addChildren(room)

	bb := room.box
	for _, p := range g.pieces {
		bb = bb.union(p.box)
	}
	var dy int
	if mesa {
		// Lifted so the centre lies between sea level and the surface.
		mx, my, mz := bb.center()
		surface := gen.SurfaceWG(mx, mz)
		target := SeaLevel
		if surface > SeaLevel {
			target = SeaLevel + g.rng.intn(surface-SeaLevel+1)
		}
		dy = target - my
	} else {
		// moveBelowSeaLevel(sea level, min y, 10).
		maxY := SeaLevel - 10
		top := bb.y1 - bb.y0 + 1 + MinY + 1
		if top < maxY {
			top += g.rng.intn(maxY - top)
		}
		dy = top - bb.y1
	}
	for _, p := range g.pieces {
		p.box.y0 += dy
		p.box.y1 += dy
		for i := range p.entrances {
			p.entrances[i].y0 += dy
			p.entrances[i].y1 += dy
		}
	}
	return g.pieces
}

// ---- siting + cache ---------------------------------------------------------

// Mineshaft is one generated mineshaft (or the zero value).
type Mineshaft struct {
	X, Y, Z int // the room's corner (the start chunk's block 2,2) and floor
	Mesa    bool
	Exists  bool
	pieces  []*msPiece
}

type msKey struct {
	seed   int64
	cx, cz int
}

var (
	msCache = map[msKey][]*msPiece{}
	msMu    sync.Mutex
)

func isBadlands(biome string) bool {
	switch biome {
	case "minecraft:badlands", "minecraft:wooded_badlands", "minecraft:eroded_badlands":
		return true
	}
	return false
}

// MineshaftAt is the mineshaft that starts in chunk (cx,cz), if any.
func (g *Generator) MineshaftAt(cx, cz int) Mineshaft {
	if g.nether || g.end || hash01(g.seed, cx, cz, 0x111E) >= shaftOdds {
		return Mineshaft{}
	}
	// The structure set's two mineshafts split by biome: the badlands take
	// the mesa one, every other biome but the deep dark the normal one.
	mesa := isBadlands(g.resolveBiome(cx*16+8, cz*16).Name)
	k := msKey{g.seed, cx, cz}
	msMu.Lock()
	pieces, ok := msCache[k]
	msMu.Unlock()
	if !ok {
		pieces = g.assembleMineshaft(cx, cz, mesa)
		msMu.Lock()
		if len(msCache) > 1024 {
			msCache = map[msKey][]*msPiece{}
		}
		msCache[k] = pieces
		msMu.Unlock()
	}
	room := pieces[0]
	if !mesa && g.caveBiomeAt(cx*16+8, room.box.y0, cz*16) == "minecraft:deep_dark" {
		return Mineshaft{}
	}
	return Mineshaft{X: room.box.x0, Y: room.box.y0, Z: room.box.z0, Mesa: mesa, Exists: true, pieces: pieces}
}

// MineshaftIn is the mineshaft starting in the chunk that holds (wx,wz).
func (g *Generator) MineshaftIn(wx, wz int) Mineshaft { return g.MineshaftAt(wx>>4, wz>>4) }

// MineshaftsNear lists the mineshafts whose pieces may reach the chunk
// holding (wx,wz).
func (g *Generator) MineshaftsNear(wx, wz int) []Mineshaft {
	var out []Mineshaft
	cx, cz := wx>>4, wz>>4
	for dx := -msChunkReach; dx <= msChunkReach; dx++ {
		for dz := -msChunkReach; dz <= msChunkReach; dz++ {
			if m := g.MineshaftAt(cx+dx, cz+dz); m.Exists {
				out = append(out, m)
			}
		}
	}
	return out
}

// PieceCounts reports how many pieces of each kind the mineshaft holds.
func (m Mineshaft) PieceCounts() map[string]int {
	out := map[string]int{}
	for _, p := range m.pieces {
		out[msKindNames[p.kind]]++
	}
	return out
}

// SpiderCorridors is how many of its corridors are cave spider nests.
func (m Mineshaft) SpiderCorridors() int {
	n := 0
	for _, p := range m.pieces {
		if p.spider {
			n++
		}
	}
	return n
}

// Contains reports whether a cell lies inside one of the mineshaft's pieces.
func (m Mineshaft) Contains(x, y, z int) bool {
	for _, p := range m.pieces {
		if p.box.inside(x, y, z) {
			return true
		}
	}
	return false
}

// msRoll is a corridor's roll for one of its sections.
func (g *Generator) msRoll(p *msPiece, section int, what uint64) float64 {
	return hash01(g.seed^int64(p.salt), section, int(what), 0x4D53)
}

// spiderSpawner is where a nest corridor's spawner goes: vanilla tries one
// cell in the middle lane of each section in turn and takes the first that
// lies under the surface.
func (g *Generator) spiderSpawner(p *msPiece) ([3]int, int, bool) {
	if !p.spider {
		return [3]int{}, -1, false
	}
	for sec := 0; sec < p.sections; sec++ {
		z := 2 + sec*5
		nz := z - 1 + int(g.msRoll(p, sec, 5)*3)
		w := p.world(1, 0, nz)
		if w[1]+1 < g.Height(w[0], w[2]) {
			return w, sec, true
		}
	}
	return [3]int{}, -1, false
}

// Spawners lists the mineshaft's cave spider spawner cells.
func (g *Generator) MineshaftSpawners(m Mineshaft) [][3]int {
	var out [][3]int
	for _, p := range m.pieces {
		if w, _, ok := g.spiderSpawner(p); ok {
			out = append(out, w)
		}
	}
	return out
}

// cartSlots are a corridor section's two chest-minecart cells (local).
func cartSlots(sec int) [2][3]int {
	z := 2 + sec*5
	return [2][3]int{{2, 0, z - 1}, {0, 0, z + 1}}
}

// MineshaftCarts lists the cells where the mineshaft rolled a chest
// minecart (chests/abandoned_mineshaft). A cart stands there only if the
// corridor laid its rail — the cell was open with a floor under it — so
// the caller checks for the rail.
func (g *Generator) MineshaftCarts(m Mineshaft) [][3]int {
	var out [][3]int
	for _, p := range m.pieces {
		if p.kind != msCorridor {
			continue
		}
		for sec := 0; sec < p.sections; sec++ {
			for i, c := range cartSlots(sec) {
				if g.msRoll(p, sec, uint64(1+i)) < 0.01 {
					out = append(out, p.world(c[0], c[1], c[2]))
				}
			}
		}
	}
	return out
}

// MineshaftCartTable is the loot table a mineshaft's chest minecarts carry.
const MineshaftCartTable = msCartTable

// ---- stamping -----------------------------------------------------------------

func (g *Generator) stampMineshafts(ch *Chunk, cx, cz int32) {
	s := newChunkStamp(g, ch, cx, cz)
	for _, m := range g.MineshaftsNear(int(cx)*16+8, int(cz)*16+8) {
		wood := msNormal
		if m.Mesa {
			wood = msMesa
		}
		ms := &msStamp{pstamp: s, wood: wood}
		logLo, logHi := BlockRange(wood.log)
		plLo, plHi := BlockRange(wood.planks)
		fLo, fHi := BlockRange(wood.fence)
		chLo, chHi := BlockRange("iron_chain")
		s.keep = func(st uint32) bool {
			return inRange(st, logLo, logHi) || inRange(st, plLo, plHi) || inRange(st, fLo, fHi) || inRange(st, chLo, chHi)
		}
		for _, p := range m.pieces {
			if !p.box.intersects(s.cb) {
				continue
			}
			s.p, s.salt = &p.opiece, p.salt
			if ms.invalid(p) {
				continue
			}
			ms.draw(p)
		}
	}
	s.keep = nil
	s.reshape()
}

type msStamp struct {
	*pstamp
	wood msWood
}

// invalid is isInInvalidLocation: the deep dark, or any liquid on the faces
// of the piece's box grown by one (within this chunk).
func (s *msStamp) invalid(p *msPiece) bool {
	x0, y0, z0 := max(p.box.x0-1, s.cb.x0), max(p.box.y0-1, s.cb.y0), max(p.box.z0-1, s.cb.z0)
	x1, y1, z1 := min(p.box.x1+1, s.cb.x1), min(p.box.y1+1, s.cb.y1), min(p.box.z1+1, s.cb.z1)
	if s.g.caveBiomeAt((x0+x1)/2, (y0+y1)/2, (z0+z1)/2) == "minecraft:deep_dark" {
		return true
	}
	liquid := func(x, y, z int) bool { return IsFluid(s.getW(x, y, z)) }
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			if liquid(x, y0, z) || liquid(x, y1, z) {
				return true
			}
		}
	}
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			if liquid(x, y, z0) || liquid(x, y, z1) {
				return true
			}
		}
	}
	for z := z0; z <= z1; z++ {
		for y := y0; y <= y1; y++ {
			if liquid(x0, y, z) || liquid(x1, y, z) {
				return true
			}
		}
	}
	return false
}

func (s *msStamp) draw(p *msPiece) {
	switch p.kind {
	case msRoom:
		s.room(p)
	case msCorridor:
		s.corridor(p)
	case msCrossing:
		s.crossing(p)
	case msStairs:
		s.air(0, 5, 0, 2, 7, 1)
		s.air(0, 0, 7, 2, 2, 8)
		for i := 0; i < 5; i++ {
			lo := 5 - i
			if i < 4 {
				lo--
			}
			s.air(0, lo, 2+i, 2, 7-i, 2+i)
		}
	}
}

func (s *msStamp) room(p *msPiece) {
	b := p.box
	s.air(b.x0, b.y0+1, b.z0, b.x1, min(b.y0+3, b.y1), b.z1)
	for _, e := range p.entrances {
		s.air(e.x0, e.y1-2, e.z0, e.x1, e.y1, e.z1)
	}
	// generateUpperHalfSphere: the domed roof.
	x0, y0, z0, x1, y1, z1 := b.x0, b.y0+4, b.z0, b.x1, b.y1, b.z1
	dx, dy, dz := float32(x1-x0+1), float32(y1-y0+1), float32(z1-z0+1)
	cx, cz := float32(x0)+dx/2, float32(z0)+dz/2
	for y := y0; y <= y1; y++ {
		ny := float32(y-y0) / dy
		for x := x0; x <= x1; x++ {
			nx := (float32(x) - cx) / (dx * 0.5)
			for z := z0; z <= z1; z++ {
				nz := (float32(z) - cz) / (dz * 0.5)
				if nx*nx+ny*ny+nz*nz <= 1.05 {
					s.placeState(Air, x, y, z)
				}
			}
		}
	}
}

func (s *msStamp) crossing(p *msPiece) {
	b := p.box
	if p.twoFloor {
		s.air(b.x0+1, b.y0, b.z0, b.x1-1, b.y0+2, b.z1)
		s.air(b.x0, b.y0, b.z0+1, b.x1, b.y0+2, b.z1-1)
		s.air(b.x0+1, b.y1-2, b.z0, b.x1-1, b.y1, b.z1)
		s.air(b.x0, b.y1-2, b.z0+1, b.x1, b.y1, b.z1-1)
		s.air(b.x0+1, b.y0+3, b.z0+1, b.x1-1, b.y0+3, b.z1-1)
	} else {
		s.air(b.x0+1, b.y0, b.z0, b.x1-1, b.y1, b.z1)
		s.air(b.x0, b.y0, b.z0+1, b.x1, b.y1, b.z1-1)
	}
	planks := bs(s.wood.planks)
	for _, c := range [][2]int{{b.x0 + 1, b.z0 + 1}, {b.x0 + 1, b.z1 - 1}, {b.x1 - 1, b.z0 + 1}, {b.x1 - 1, b.z1 - 1}} {
		if s.get(c[0], b.y1+1, c[1]) != Air { // a pillar where the ceiling holds one
			s.fill(c[0], b.y0, c[1], c[0], b.y1, c[1], planks)
		}
	}
	for x := b.x0; x <= b.x1; x++ {
		for z := b.z0; z <= b.z1; z++ {
			s.planksAt(x, b.y0-1, z)
		}
	}
}

// planksAt is setPlanksBlock: a plank under the floor where nothing solid
// holds it up (only beneath the surface).
func (s *msStamp) planksAt(x, y, z int) {
	if !s.interior(x, y, z) {
		return
	}
	w := s.p.world(x, y, z)
	if !IsSturdyTop(s.getW(w[0], w[1], w[2])) {
		s.setW(w[0], w[1], w[2], bs(s.wood.planks).state(0, 0))
	}
}

func (s *msStamp) corridor(p *msPiece) {
	length := p.sections*5 - 1
	web := bs("cobweb")
	s.air(0, 0, 0, 2, 1, length)
	s.maybeBox(0.8, 0, 2, 0, 2, 2, length, bs("air"), bs("air"), false, false, 0x21)
	if p.spider {
		s.maybeBox(0.6, 0, 0, 0, 2, 1, length, web, bs("air"), false, true, 0x22)
	}
	spawner, spawnerSec, _ := s.g.spiderSpawner(p)
	for sec := 0; sec < p.sections; sec++ {
		z := 2 + sec*5
		s.support(p, sec, z)
		for i, c := range []struct {
			prob float64
			x, z int
		}{{0.1, 0, z - 1}, {0.1, 2, z - 1}, {0.1, 0, z + 1}, {0.1, 2, z + 1}, {0.05, 0, z - 2}, {0.05, 2, z - 2}, {0.05, 0, z + 2}, {0.05, 2, z + 2}} {
			if s.interior(c.x, 2, c.z) && s.roll(c.x, 2, c.z, 0x30+uint64(i)) < c.prob && s.sturdyNeighbours(c.x, 2, c.z) >= 2 {
				s.place(web, c.x, 2, c.z)
			}
		}
		for i, c := range cartSlots(sec) {
			if s.g.msRoll(p, sec, uint64(1+i)) < 0.01 {
				s.cartRail(p, sec, c)
			}
		}
		if sec == spawnerSec && s.cb.inside(spawner[0], spawner[1], spawner[2]) {
			s.setW(spawner[0], spawner[1], spawner[2], Spawner)
		}
	}
	for x := 0; x <= 2; x++ {
		for z := 0; z <= length; z++ {
			s.planksAt(x, -1, z)
		}
	}
	s.pillarOrChain(0, -1, 2)
	if p.sections > 1 {
		s.pillarOrChain(0, -1, length-2)
	}
	if p.rails {
		rail := bsp("rail", "shape=north_south")
		for z := 0; z <= length; z++ {
			if f := s.get(1, -1, z); f != Air && IsFullCube(f) {
				prob := 0.9
				if s.interior(1, 0, z) {
					prob = 0.7
				}
				s.maybeBlock(prob, 1, 0, z, rail, 0x40)
			}
		}
	}
}

// cartRail is the corridor's createChest: where the cell is open with a
// floor under it, a rail for the chest minecart the server seeds there.
func (s *msStamp) cartRail(p *msPiece, sec int, c [3]int) {
	w := p.world(c[0], c[1], c[2])
	if !s.cb.inside(w[0], w[1], w[2]) || s.getW(w[0], w[1], w[2]) != Air || s.getW(w[0], w[1]-1, w[2]) == Air {
		return
	}
	shape := "north_south"
	if s.g.msRoll(p, sec, uint64(10+c[2])) < 0.5 {
		shape = "east_west"
	}
	s.place(bsp("rail", "shape="+shape), c[0], c[1], c[2])
}

// support is placeSupport: fence posts under a plank beam where the roof
// above the beam is solid all the way across.
func (s *msStamp) support(p *msPiece, sec, z int) {
	for x := 0; x <= 2; x++ {
		if s.get(x, 3, z) == Air {
			return
		}
	}
	fence := bs(s.wood.fence)
	planks := bs(s.wood.planks)
	s.fill(0, 0, z, 0, 1, z, fence.with("west=true"))
	s.fill(2, 0, z, 2, 1, z, fence.with("east=true"))
	if s.g.msRoll(p, sec, 20) < 0.25 {
		s.place(planks, 0, 2, z)
		s.place(planks, 2, 2, z)
		return
	}
	s.fill(0, 2, z, 2, 2, z, planks)
	s.maybeBlock(0.05, 1, 2, z-1, sbWallTorch.with("facing=south"), 0x50)
	s.maybeBlock(0.05, 1, 2, z+1, sbWallTorch.with("facing=north"), 0x51)
}

// sturdyNeighbours counts the full blocks around a cell (in this chunk).
func (s *msStamp) sturdyNeighbours(x, y, z int) int {
	w := s.p.world(x, y, z)
	n := 0
	for _, d := range [][3]int{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
		nx, ny, nz := w[0]+d[0], w[1]+d[1], w[2]+d[2]
		if s.cb.inside(nx, ny, nz) && IsFullCube(s.getW(nx, ny, nz)) {
			n++
		}
	}
	return n
}

// pillarOrChain is placeDoubleLowerOrUpperSupport: under a support whose
// floor plank was laid over a gap, a log pillar down to solid ground — or,
// failing that, a fence and chain hung from the ceiling.
func (s *msStamp) pillarOrChain(x, y, z int) {
	plLo, plHi := BlockRange(s.wood.planks)
	for _, px := range []int{x, x + 2} {
		if inRange(s.get(px, y, z), plLo, plHi) {
			s.fillPillarDownOrChainUp(px, y, z)
		}
	}
}

func (s *msStamp) fillPillarDownOrChainUp(x, y, z int) {
	w := s.p.world(x, y, z)
	if !s.cb.inside(w[0], w[1], w[2]) {
		return
	}
	wx, wy, wz := w[0], w[1], w[2]
	log := bs(s.wood.log).state(0, 0)
	below, above := true, true
	for d := 1; below || above; d++ {
		if below {
			st := s.getW(wx, wy-d, wz)
			empty := replaceableByStructures(st) && !IsLava(st)
			if !empty && IsSturdyTop(st) {
				for yy := wy - d + 1; yy < wy; yy++ {
					s.setW(wx, yy, wz, log)
				}
				return
			}
			below = d <= msMaxPillar && empty && wy-d > MinY+1
		}
		if above {
			st := s.getW(wx, wy+d, wz)
			empty := replaceableByStructures(st)
			if !empty && IsFullCube(st) && !IsFalling(st) {
				s.setW(wx, wy+1, wz, bs(s.wood.fence).state(0, 0))
				chain := bs("iron_chain").state(0, 0)
				for yy := wy + 2; yy < wy+d; yy++ {
					s.setW(wx, yy, wz, chain)
				}
				return
			}
			above = d <= msMaxChain && empty && wy+d < s.cb.y1
		}
	}
}
