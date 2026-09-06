package worldgen

import "sync"

// Nether fortresses — a port of vanilla's fortress piece generator. A start
// crossing at the site grows bridges (straight spans, crossings, rooms,
// stair rooms, blaze thrones, the castle entrance) and, behind an entrance,
// the castle corridors (straight, crossings, turns with a chance of a
// chest, stairs, T-balconies, the nether-wart stalk room). Pieces are drawn
// from weighted tables with per-fortress placement caps, a piece may not
// follow itself unless its table row allows it, growth stops thirty pieces
// deep or 112 blocks from the start, and any dead end that cannot fit its
// next piece gets a crumbling bridge stub. The finished structure is shifted
// so its lowest floor lies between y=48 and y=70.
//
// Every piece is an oriented box: local (x,y,z) map to the world through
// the piece's facing, and directional block states (fences, stairs) are
// mirrored and rotated the way vanilla's setOrientation prescribes. Blocks
// are written per chunk — a piece stamps only the cells inside the chunk
// being generated — so the same piece re-stamps identically for every
// chunk it crosses.

const (
	fortressCell     = 400 // one candidate per 400-block cell
	fortressOdds     = 0.5 // of cells
	fortressMaxDepth = 30
	fortressReach    = 112 // blocks from the start, beyond which nothing grows
	fortressMinY     = 48  // moveInsideHeights band
	fortressMaxY     = 70
)

// fdir is a horizontal facing in vanilla's Direction order.
type fdir int

const (
	fNorth fdir = iota
	fSouth
	fWest
	fEast
)

// fbox is an inclusive world-space box.
type fbox struct{ x0, y0, z0, x1, y1, z1 int }

func (b fbox) intersects(o fbox) bool {
	return b.x1 >= o.x0 && b.x0 <= o.x1 && b.y1 >= o.y0 && b.y0 <= o.y1 && b.z1 >= o.z0 && b.z0 <= o.z1
}

// orientBox is BoundingBox.orientBox: a w×h×d box whose local origin sits
// at the foot plus an offset, extended along the facing.
func orientBox(footX, footY, footZ, offX, offY, offZ, w, h, d int, dir fdir) fbox {
	switch dir {
	case fNorth:
		return fbox{footX + offX, footY + offY, footZ - d + 1 + offZ, footX + w - 1 + offX, footY + h - 1 + offY, footZ + offZ}
	case fWest:
		return fbox{footX - d + 1 + offZ, footY + offY, footZ + offX, footX + offZ, footY + h - 1 + offY, footZ + w - 1 + offX}
	case fEast:
		return fbox{footX + offZ, footY + offY, footZ + offX, footX + d - 1 + offZ, footY + h - 1 + offY, footZ + w - 1 + offX}
	}
	return fbox{footX + offX, footY + offY, footZ + offZ, footX + w - 1 + offX, footY + h - 1 + offY, footZ + d - 1 + offZ}
}

// fkind is one of the sixteen piece kinds.
type fkind int

const (
	fkBridgeCrossing fkind = iota
	fkBridgeEndFiller
	fkBridgeStraight
	fkCorridorStairs
	fkCorridorBalcony
	fkCastleEntrance
	fkCorridorCrossing
	fkCorridorLeftTurn
	fkCorridor
	fkCorridorRightTurn
	fkStalkRoom
	fkMonsterThrone
	fkRoomCrossing
	fkStairsRoom
)

// fpiece is one placed piece.
type fpiece struct {
	kind     fkind
	box      fbox
	dir      fdir
	depth    int
	rot, mir int    // state transform for this facing (setOrientation)
	seed     uint64 // bridge end filler: its own roll
	chest    bool   // corridor turns: a chest in the corner
}

// pieceWeight is one row of a placement table.
type pieceWeight struct {
	kind       fkind
	weight     int
	placeCount int
	maxCount   int // 0 = unlimited
	allowInRow bool
}

func (w *pieceWeight) canPlace() bool { return w.maxCount == 0 || w.placeCount < w.maxCount }

func bridgeWeights() []*pieceWeight {
	return []*pieceWeight{
		{kind: fkBridgeStraight, weight: 30, allowInRow: true},
		{kind: fkBridgeCrossing, weight: 10, maxCount: 4},
		{kind: fkRoomCrossing, weight: 10, maxCount: 4},
		{kind: fkStairsRoom, weight: 10, maxCount: 3},
		{kind: fkMonsterThrone, weight: 5, maxCount: 2},
		{kind: fkCastleEntrance, weight: 5, maxCount: 1},
	}
}

func castleWeights() []*pieceWeight {
	return []*pieceWeight{
		{kind: fkCorridor, weight: 25, allowInRow: true},
		{kind: fkCorridorCrossing, weight: 15, maxCount: 5},
		{kind: fkCorridorRightTurn, weight: 5, maxCount: 10},
		{kind: fkCorridorLeftTurn, weight: 5, maxCount: 10},
		{kind: fkCorridorStairs, weight: 10, maxCount: 3, allowInRow: true},
		{kind: fkCorridorBalcony, weight: 7, maxCount: 2},
		{kind: fkStalkRoom, weight: 5, maxCount: 2},
	}
}

// pieceShape is a kind's oriented-box parameters (createPiece).
var pieceShape = map[fkind][6]int{ // offX, offY, offZ, w, h, d
	fkBridgeCrossing:    {-8, -3, 0, 19, 10, 19},
	fkBridgeEndFiller:   {-1, -3, 0, 5, 10, 8},
	fkBridgeStraight:    {-1, -3, 0, 5, 10, 19},
	fkCorridorStairs:    {-1, -7, 0, 5, 14, 10},
	fkCorridorBalcony:   {-3, 0, 0, 9, 7, 9},
	fkCastleEntrance:    {-5, -3, 0, 13, 14, 13},
	fkCorridorCrossing:  {-1, 0, 0, 5, 7, 5},
	fkCorridorLeftTurn:  {-1, 0, 0, 5, 7, 5},
	fkCorridor:          {-1, 0, 0, 5, 7, 5},
	fkCorridorRightTurn: {-1, 0, 0, 5, 7, 5},
	fkStalkRoom:         {-5, -3, 0, 13, 14, 13},
	fkMonsterThrone:     {-2, 0, 0, 7, 8, 9},
	fkRoomCrossing:      {-2, 0, 0, 7, 9, 7},
	fkStairsRoom:        {-2, 0, 0, 7, 11, 7},
}

// orientation is StructurePiece.setOrientation: the mirror/rotation a
// facing applies to directional states.
func orientation(dir fdir) (rot, mir int) {
	switch dir {
	case fSouth:
		return 0, mirLR
	case fWest:
		return 1, mirLR
	case fEast:
		return 1, mirNone
	}
	return 0, mirNone
}

// ---- generation -------------------------------------------------------------

type fortressGen struct {
	rng     *jigsawRNG
	pieces  []*fpiece
	start   *fpiece
	bridges []*pieceWeight
	castle  []*pieceWeight
	prev    *pieceWeight
	pending []*fpiece
}

func (g *fortressGen) collides(b fbox) bool {
	for _, p := range g.pieces {
		if p.box.intersects(b) {
			return true
		}
	}
	return false
}

// createPiece places a kind at a foot if its box is high enough and free.
func (g *fortressGen) createPiece(kind fkind, footX, footY, footZ int, dir fdir, depth int) *fpiece {
	s := pieceShape[kind]
	box := orientBox(footX, footY, footZ, s[0], s[1], s[2], s[3], s[4], s[5], dir)
	if box.y0 <= 10 || g.collides(box) {
		return nil
	}
	p := &fpiece{kind: kind, box: box, dir: dir, depth: depth}
	p.rot, p.mir = orientation(dir)
	switch kind {
	case fkBridgeEndFiller:
		p.seed = g.rng.next()
	case fkCorridorLeftTurn, fkCorridorRightTurn:
		p.chest = g.rng.intn(3) == 0
	}
	return p
}

func totalWeight(rows []*pieceWeight) int {
	any, total := false, 0
	for _, r := range rows {
		if r.maxCount > 0 && r.placeCount < r.maxCount {
			any = true
		}
		total += r.weight
	}
	if !any {
		return -1
	}
	return total
}

// generatePiece draws a piece from a table (five tries), else a bridge stub.
func (g *fortressGen) generatePiece(rows *[]*pieceWeight, footX, footY, footZ int, dir fdir, depth int) *fpiece {
	total := totalWeight(*rows)
	for attempt := 0; attempt < 5 && total > 0 && depth <= fortressMaxDepth; attempt++ {
		sel := g.rng.intn(total)
		for i, row := range *rows {
			sel -= row.weight
			if sel >= 0 {
				continue
			}
			if !row.canPlace() || (row == g.prev && !row.allowInRow) {
				break
			}
			if p := g.createPiece(row.kind, footX, footY, footZ, dir, depth); p != nil {
				row.placeCount++
				g.prev = row
				if !row.canPlace() {
					*rows = append((*rows)[:i], (*rows)[i+1:]...)
				}
				return p
			}
			break
		}
	}
	return g.createPiece(fkBridgeEndFiller, footX, footY, footZ, dir, depth)
}

func (g *fortressGen) generateAndAdd(footX, footY, footZ int, dir fdir, depth int, castle bool) {
	if abs(footX-g.start.box.x0) > fortressReach || abs(footZ-g.start.box.z0) > fortressReach {
		return // vanilla builds a stub here but never adds it
	}
	rows := &g.bridges
	if castle {
		rows = &g.castle
	}
	if p := g.generatePiece(rows, footX, footY, footZ, dir, depth+1); p != nil {
		g.pieces = append(g.pieces, p)
		g.pending = append(g.pending, p)
	}
}

func (g *fortressGen) childForward(p *fpiece, xOff, yOff int, castle bool) {
	b := p.box
	switch p.dir {
	case fNorth:
		g.generateAndAdd(b.x0+xOff, b.y0+yOff, b.z0-1, p.dir, p.depth, castle)
	case fSouth:
		g.generateAndAdd(b.x0+xOff, b.y0+yOff, b.z1+1, p.dir, p.depth, castle)
	case fWest:
		g.generateAndAdd(b.x0-1, b.y0+yOff, b.z0+xOff, p.dir, p.depth, castle)
	case fEast:
		g.generateAndAdd(b.x1+1, b.y0+yOff, b.z0+xOff, p.dir, p.depth, castle)
	}
}

func (g *fortressGen) childLeft(p *fpiece, yOff, zOff int, castle bool) {
	b := p.box
	switch p.dir {
	case fNorth, fSouth:
		g.generateAndAdd(b.x0-1, b.y0+yOff, b.z0+zOff, fWest, p.depth, castle)
	case fWest, fEast:
		g.generateAndAdd(b.x0+zOff, b.y0+yOff, b.z0-1, fNorth, p.depth, castle)
	}
}

func (g *fortressGen) childRight(p *fpiece, yOff, zOff int, castle bool) {
	b := p.box
	switch p.dir {
	case fNorth, fSouth:
		g.generateAndAdd(b.x1+1, b.y0+yOff, b.z0+zOff, fEast, p.depth, castle)
	case fWest, fEast:
		g.generateAndAdd(b.x0+zOff, b.y0+yOff, b.z1+1, fSouth, p.depth, castle)
	}
}

// addChildren is each kind's growth rule.
func (g *fortressGen) addChildren(p *fpiece) {
	switch p.kind {
	case fkBridgeCrossing:
		g.childForward(p, 8, 3, false)
		g.childLeft(p, 3, 8, false)
		g.childRight(p, 3, 8, false)
	case fkBridgeStraight:
		g.childForward(p, 1, 3, false)
	case fkCorridorStairs:
		g.childForward(p, 1, 0, true)
	case fkCorridorBalcony:
		zOff := 1
		if p.dir == fWest || p.dir == fNorth {
			zOff = 5
		}
		g.childLeft(p, 0, zOff, g.rng.intn(8) > 0)
		g.childRight(p, 0, zOff, g.rng.intn(8) > 0)
	case fkCastleEntrance:
		g.childForward(p, 5, 3, true)
	case fkCorridorCrossing:
		g.childForward(p, 1, 0, true)
		g.childLeft(p, 0, 1, true)
		g.childRight(p, 0, 1, true)
	case fkCorridorLeftTurn:
		g.childLeft(p, 0, 1, true)
	case fkCorridor:
		g.childForward(p, 1, 0, true)
	case fkCorridorRightTurn:
		g.childRight(p, 0, 1, true)
	case fkStalkRoom:
		g.childForward(p, 5, 3, true)
		g.childForward(p, 5, 11, true)
	case fkRoomCrossing:
		g.childForward(p, 2, 0, false)
		g.childLeft(p, 0, 2, false)
		g.childRight(p, 0, 2, false)
	case fkStairsRoom:
		g.childRight(p, 6, 2, false)
	}
}

// assembleFortress runs the whole generation for a site.
func assembleFortress(seed int64, x, z int) []*fpiece {
	g := &fortressGen{rng: newJigsawRNG(seed, x, z), bridges: bridgeWeights(), castle: castleWeights()}
	dir := fdir(g.rng.intn(4))
	start := &fpiece{kind: fkBridgeCrossing, box: orientBox(x, 64, z, 0, 0, 0, 19, 10, 19, dir), dir: dir}
	start.rot, start.mir = orientation(dir)
	g.start = start
	g.pieces = append(g.pieces, start)
	g.addChildren(start)
	for len(g.pending) > 0 {
		i := g.rng.intn(len(g.pending))
		p := g.pending[i]
		g.pending = append(g.pending[:i], g.pending[i+1:]...)
		g.addChildren(p)
	}
	// moveInsideHeights(48, 70): shift so the lowest floor lands in the band.
	minY, maxY := 1<<30, -1<<30
	for _, p := range g.pieces {
		if p.box.y0 < minY {
			minY = p.box.y0
		}
		if p.box.y1 > maxY {
			maxY = p.box.y1
		}
	}
	span := fortressMaxY - fortressMinY + 1 - (maxY - minY + 1)
	target := fortressMinY
	if span > 1 {
		target += g.rng.intn(span)
	}
	dy := target - minY
	for _, p := range g.pieces {
		p.box.y0 += dy
		p.box.y1 += dy
	}
	return g.pieces
}

// ---- siting + cache ---------------------------------------------------------

// Fortress is a placed fortress site (or the zero value).
type Fortress struct {
	X, Z   int
	Exists bool
}

// FortressIn returns the fortress owning (wx,wz)'s cell.
func (g *Generator) FortressIn(wx, wz int) Fortress {
	ox, oz := cellOrigin(wx, fortressCell), cellOrigin(wz, fortressCell)
	if hash01(g.seed, ox, oz, 0xF0A7) >= fortressOdds {
		return Fortress{}
	}
	x := ox + 64 + int(hash01(g.seed, ox, oz, 0xF0A8)*float64(fortressCell-128))
	z := oz + 64 + int(hash01(g.seed, ox, oz, 0xF0A9)*float64(fortressCell-128))
	return Fortress{X: x, Z: z, Exists: true}
}

type fortressKey struct {
	seed int64
	x, z int
}

var (
	fortressCache = map[fortressKey][]*fpiece{}
	fortressMu    sync.Mutex
)

// AssembleFortress assembles (and caches) a site's pieces. Deterministic.
func (g *Generator) assembleFortress(f Fortress) []*fpiece {
	k := fortressKey{g.seed, f.X, f.Z}
	fortressMu.Lock()
	p, ok := fortressCache[k]
	fortressMu.Unlock()
	if ok {
		return p
	}
	p = assembleFortress(g.seed, f.X, f.Z)
	fortressMu.Lock()
	fortressCache[k] = p
	fortressMu.Unlock()
	return p
}

// FortressPiece is a piece's world box, for the server's spawn overrides.
type FortressPiece struct{ X0, Y0, Z0, X1, Y1, Z1 int }

// FortressPieces returns the pieces' inclusive world boxes.
func (g *Generator) FortressPieces(f Fortress) []FortressPiece {
	ps := g.assembleFortress(f)
	out := make([]FortressPiece, len(ps))
	for i, p := range ps {
		out[i] = FortressPiece{p.box.x0, p.box.y0, p.box.z0, p.box.x1, p.box.y1, p.box.z1}
	}
	return out
}

// FortressChests returns the corridor-turn chests (chests/nether_bridge).
func (g *Generator) FortressChests(f Fortress) [][3]int {
	var out [][3]int
	for _, p := range g.assembleFortress(f) {
		if !p.chest {
			continue
		}
		switch p.kind {
		case fkCorridorLeftTurn:
			out = append(out, p.worldPos(3, 2, 3))
		case fkCorridorRightTurn:
			out = append(out, p.worldPos(1, 2, 3))
		}
	}
	return out
}

// FortressSpawners returns the blaze spawner cells of the monster thrones.
func (g *Generator) FortressSpawners(f Fortress) [][3]int {
	var out [][3]int
	for _, p := range g.assembleFortress(f) {
		if p.kind == fkMonsterThrone {
			out = append(out, p.worldPos(3, 5, 5))
		}
	}
	return out
}

// stampFortress stamps the pieces overlapping this nether chunk.
func (g *Generator) stampFortress(ch *Chunk, cx, cz int32) {
	chunk := fbox{int(cx) * 16, MinY, int(cz) * 16, int(cx)*16 + 15, MinY + len(ch.Sections)*16 - 1, int(cz)*16 + 15}
	for _, f := range g.FortressesNear(int(cx)*16+8, int(cz)*16+8) {
		s := &fstamp{ch: ch, chunk: chunk}
		for _, p := range g.assembleFortress(f) {
			if p.box.intersects(chunk) {
				s.p = p
				s.postProcess()
			}
		}
	}
}

// FortressesNear returns the fortresses of the cell around (wx,wz) and its
// eight neighbours: a fortress reaches 112 blocks from its start, so its
// pieces cross cell borders.
func (g *Generator) FortressesNear(wx, wz int) []Fortress {
	var out []Fortress
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if f := g.FortressIn(wx+dx*fortressCell, wz+dz*fortressCell); f.Exists {
				out = append(out, f)
			}
		}
	}
	return out
}

// ---- piece geometry ---------------------------------------------------------

func (p *fpiece) worldX(x, z int) int {
	switch p.dir {
	case fWest:
		return p.box.x1 - z
	case fEast:
		return p.box.x0 + z
	}
	return p.box.x0 + x
}

func (p *fpiece) worldZ(x, z int) int {
	switch p.dir {
	case fNorth:
		return p.box.z1 - z
	case fSouth:
		return p.box.z0 + z
	}
	return p.box.z0 + x
}

func (p *fpiece) worldPos(x, y, z int) [3]int {
	return [3]int{p.worldX(x, z), p.box.y0 + y, p.worldZ(x, z)}
}

// ---- block specs ------------------------------------------------------------

// fblock is a block with the directional properties vanilla sets on it;
// the piece's orientation mirrors and rotates them when it lands.
type fblock struct {
	name  string
	props map[string]string
}

var (
	fbNB       = fblock{name: "nether_bricks"}
	fbAir      = fblock{name: "air"}
	fbSoulSand = fblock{name: "soul_sand"}
	fbWart     = fblock{name: "nether_wart"}
	fbLava     = fblock{name: "lava"}
	fbChest    = fblock{name: "chest"}
	fbSpawner  = fblock{name: "spawner"}
)

func fence(sides ...string) fblock {
	b := fblock{name: "nether_brick_fence", props: map[string]string{}}
	for _, s := range sides {
		b.props[s] = "true"
	}
	return b
}

func stairs(facing string) fblock {
	return fblock{name: "nether_brick_stairs", props: map[string]string{"facing": facing}}
}

type fstateKey struct {
	spec     string
	rot, mir int
}

var (
	fstateCache = map[fstateKey]uint32{}
	fstateMu    sync.Mutex
)

// resolve turns a block spec into a state under the piece's orientation.
func (p *fpiece) resolve(b fblock) uint32 {
	key := fstateKey{b.name, p.rot, p.mir}
	for _, s := range []string{"north", "south", "east", "west", "facing"} {
		if v, ok := b.props[s]; ok {
			key.spec += "|" + s + "=" + v
		}
	}
	fstateMu.Lock()
	st, ok := fstateCache[key]
	fstateMu.Unlock()
	if ok {
		return st
	}
	st = resolveStateM(paletteEntry{Name: b.name, Props: b.props}, p.rot, p.mir)
	if st == tmplSkip {
		st = Air
	}
	fstateMu.Lock()
	fstateCache[key] = st
	fstateMu.Unlock()
	return st
}

// ---- stamping ---------------------------------------------------------------

type fstamp struct {
	ch    *Chunk
	chunk fbox
	p     *fpiece
}

func (s *fstamp) inside(x, y, z int) bool {
	return x >= s.chunk.x0 && x <= s.chunk.x1 && y >= s.chunk.y0 && y <= s.chunk.y1 && z >= s.chunk.z0 && z <= s.chunk.z1
}

// place is placeBlock: a local cell, if it lies in this chunk.
func (s *fstamp) place(b fblock, x, y, z int) {
	wx, wy, wz := s.p.worldX(x, z), s.p.box.y0+y, s.p.worldZ(x, z)
	if s.inside(wx, wy, wz) {
		setSectionBlock(s.ch, wx-s.chunk.x0, wy, wz-s.chunk.z0, s.p.resolve(b), true)
	}
}

func (s *fstamp) at(x, y, z int) (uint32, bool) {
	wx, wy, wz := s.p.worldX(x, z), s.p.box.y0+y, s.p.worldZ(x, z)
	if !s.inside(wx, wy, wz) {
		return Air, false
	}
	return sectionBlockAt(s.ch, wx-s.chunk.x0, wy, wz-s.chunk.z0), true
}

// box is generateBox: edge block on the faces, fill block inside.
func (s *fstamp) box(x0, y0, z0, x1, y1, z1 int, edge, fill fblock) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if y != y0 && y != y1 && x != x0 && x != x1 && z != z0 && z != z1 {
					s.place(fill, x, y, z)
				} else {
					s.place(edge, x, y, z)
				}
			}
		}
	}
}

// solid is generateBox with one block throughout.
func (s *fstamp) solid(x0, y0, z0, x1, y1, z1 int, b fblock) { s.box(x0, y0, z0, x1, y1, z1, b, b) }

// column is fillColumnDown: from a local cell downward through air and
// lava until something solid.
func (s *fstamp) column(b fblock, x, startY, z int) {
	wx, wz := s.p.worldX(x, z), s.p.worldZ(x, z)
	if wx < s.chunk.x0 || wx > s.chunk.x1 || wz < s.chunk.z0 || wz > s.chunk.z1 {
		return
	}
	st := s.p.resolve(b)
	for wy := s.p.box.y0 + startY; wy > MinY+1; wy-- {
		if wy > s.chunk.y1 {
			continue
		}
		cur := sectionBlockAt(s.ch, wx-s.chunk.x0, wy, wz-s.chunk.z0)
		if cur != Air && !IsLava(cur) && !IsWater(cur) {
			return
		}
		setSectionBlock(s.ch, wx-s.chunk.x0, wy, wz-s.chunk.z0, st, true)
	}
}

// postProcess stamps the piece's block program.
func (s *fstamp) postProcess() {
	switch s.p.kind {
	case fkBridgeCrossing:
		s.bridgeCrossing()
	case fkBridgeEndFiller:
		s.bridgeEndFiller()
	case fkBridgeStraight:
		s.bridgeStraight()
	case fkCorridorStairs:
		s.corridorStairs()
	case fkCorridorBalcony:
		s.corridorBalcony()
	case fkCastleEntrance:
		s.castleEntrance()
	case fkCorridorCrossing:
		s.corridorCrossing()
	case fkCorridorLeftTurn:
		s.corridorLeftTurn()
	case fkCorridor:
		s.corridor()
	case fkCorridorRightTurn:
		s.corridorRightTurn()
	case fkStalkRoom:
		s.stalkRoom()
	case fkMonsterThrone:
		s.monsterThrone()
	case fkRoomCrossing:
		s.roomCrossing()
	case fkStairsRoom:
		s.stairsRoom()
	}
}

func (s *fstamp) bridgeCrossing() {
	s.solid(7, 3, 0, 11, 4, 18, fbNB)
	s.solid(0, 3, 7, 18, 4, 11, fbNB)
	s.solid(8, 5, 0, 10, 7, 18, fbAir)
	s.solid(0, 5, 8, 18, 7, 10, fbAir)
	s.solid(7, 5, 0, 7, 5, 7, fbNB)
	s.solid(7, 5, 11, 7, 5, 18, fbNB)
	s.solid(11, 5, 0, 11, 5, 7, fbNB)
	s.solid(11, 5, 11, 11, 5, 18, fbNB)
	s.solid(0, 5, 7, 7, 5, 7, fbNB)
	s.solid(11, 5, 7, 18, 5, 7, fbNB)
	s.solid(0, 5, 11, 7, 5, 11, fbNB)
	s.solid(11, 5, 11, 18, 5, 11, fbNB)
	s.solid(7, 2, 0, 11, 2, 5, fbNB)
	s.solid(7, 2, 13, 11, 2, 18, fbNB)
	s.solid(7, 0, 0, 11, 1, 3, fbNB)
	s.solid(7, 0, 15, 11, 1, 18, fbNB)
	for x := 7; x <= 11; x++ {
		for z := 0; z <= 2; z++ {
			s.column(fbNB, x, -1, z)
			s.column(fbNB, x, -1, 18-z)
		}
	}
	s.solid(0, 2, 7, 5, 2, 11, fbNB)
	s.solid(13, 2, 7, 18, 2, 11, fbNB)
	s.solid(0, 0, 7, 3, 1, 11, fbNB)
	s.solid(15, 0, 7, 18, 1, 11, fbNB)
	for x := 0; x <= 2; x++ {
		for z := 7; z <= 11; z++ {
			s.column(fbNB, x, -1, z)
			s.column(fbNB, 18-x, -1, z)
		}
	}
}

func (s *fstamp) bridgeEndFiller() {
	r := &jigsawRNG{s: s.p.seed}
	for x := 0; x <= 4; x++ {
		for y := 3; y <= 4; y++ {
			z := r.intn(8)
			s.solid(x, y, 0, x, y, z, fbNB)
		}
	}
	z := r.intn(8)
	s.solid(0, 5, 0, 0, 5, z, fbNB)
	z = r.intn(8)
	s.solid(4, 5, 0, 4, 5, z, fbNB)
	for x := 0; x <= 4; x++ {
		zx := r.intn(5)
		s.solid(x, 2, 0, x, 2, zx, fbNB)
	}
	for x := 0; x <= 4; x++ {
		for y := 0; y <= 1; y++ {
			zx := r.intn(3)
			s.solid(x, y, 0, x, y, zx, fbNB)
		}
	}
}

func (s *fstamp) bridgeStraight() {
	s.solid(0, 3, 0, 4, 4, 18, fbNB)
	s.solid(1, 5, 0, 3, 7, 18, fbAir)
	s.solid(0, 5, 0, 0, 5, 18, fbNB)
	s.solid(4, 5, 0, 4, 5, 18, fbNB)
	s.solid(0, 2, 0, 4, 2, 5, fbNB)
	s.solid(0, 2, 13, 4, 2, 18, fbNB)
	s.solid(0, 0, 0, 4, 1, 3, fbNB)
	s.solid(0, 0, 15, 4, 1, 18, fbNB)
	for x := 0; x <= 4; x++ {
		for z := 0; z <= 2; z++ {
			s.column(fbNB, x, -1, z)
			s.column(fbNB, x, -1, 18-z)
		}
	}
	nse := fence("north", "south", "east")
	nsw := fence("north", "south", "west")
	s.solid(0, 1, 1, 0, 4, 1, nse)
	s.solid(0, 3, 4, 0, 4, 4, nse)
	s.solid(0, 3, 14, 0, 4, 14, nse)
	s.solid(0, 1, 17, 0, 4, 17, nse)
	s.solid(4, 1, 1, 4, 4, 1, nsw)
	s.solid(4, 3, 4, 4, 4, 4, nsw)
	s.solid(4, 3, 14, 4, 4, 14, nsw)
	s.solid(4, 1, 17, 4, 4, 17, nsw)
}

func (s *fstamp) corridorStairs() {
	st := stairs("south")
	ns := fence("north", "south")
	for step := 0; step <= 9; step++ {
		floor := max(1, 7-step)
		roof := min(max(floor+5, 14-step), 13)
		s.solid(0, 0, step, 4, floor, step, fbNB)
		s.solid(1, floor+1, step, 3, roof-1, step, fbAir)
		if step <= 6 {
			s.place(st, 1, floor+1, step)
			s.place(st, 2, floor+1, step)
			s.place(st, 3, floor+1, step)
		}
		s.solid(0, roof, step, 4, roof, step, fbNB)
		s.solid(0, floor+1, step, 0, roof-1, step, fbNB)
		s.solid(4, floor+1, step, 4, roof-1, step, fbNB)
		if step&1 == 0 {
			s.solid(0, floor+2, step, 0, floor+3, step, ns)
			s.solid(4, floor+2, step, 4, floor+3, step, ns)
		}
		for x := 0; x <= 4; x++ {
			s.column(fbNB, x, -1, step)
		}
	}
}

func (s *fstamp) corridorBalcony() {
	ns := fence("north", "south")
	we := fence("west", "east")
	s.solid(0, 0, 0, 8, 1, 8, fbNB)
	s.solid(0, 2, 0, 8, 5, 8, fbAir)
	s.solid(0, 6, 0, 8, 6, 5, fbNB)
	s.solid(0, 2, 0, 2, 5, 0, fbNB)
	s.solid(6, 2, 0, 8, 5, 0, fbNB)
	s.solid(1, 3, 0, 1, 4, 0, we)
	s.solid(7, 3, 0, 7, 4, 0, we)
	s.solid(0, 2, 4, 8, 2, 8, fbNB)
	s.solid(1, 1, 4, 2, 2, 4, fbAir)
	s.solid(6, 1, 4, 7, 2, 4, fbAir)
	s.solid(1, 3, 8, 7, 3, 8, we)
	s.place(fence("east", "south"), 0, 3, 8)
	s.place(fence("west", "south"), 8, 3, 8)
	s.solid(0, 3, 6, 0, 3, 7, ns)
	s.solid(8, 3, 6, 8, 3, 7, ns)
	s.solid(0, 3, 4, 0, 5, 5, fbNB)
	s.solid(8, 3, 4, 8, 5, 5, fbNB)
	s.solid(1, 3, 5, 2, 5, 5, fbNB)
	s.solid(6, 3, 5, 7, 5, 5, fbNB)
	s.solid(1, 4, 5, 1, 5, 5, we)
	s.solid(7, 4, 5, 7, 5, 5, we)
	for z := 0; z <= 5; z++ {
		for x := 0; x <= 8; x++ {
			s.column(fbNB, x, -1, z)
		}
	}
}

// bigRoomShell is the walls, roof and fence work the castle entrance and
// the stalk room share.
func (s *fstamp) bigRoomShell() {
	s.solid(0, 3, 0, 12, 4, 12, fbNB)
	s.solid(0, 5, 0, 12, 13, 12, fbAir)
	s.solid(0, 5, 0, 1, 12, 12, fbNB)
	s.solid(11, 5, 0, 12, 12, 12, fbNB)
	s.solid(2, 5, 11, 4, 12, 12, fbNB)
	s.solid(8, 5, 11, 10, 12, 12, fbNB)
	s.solid(5, 9, 11, 7, 12, 12, fbNB)
	s.solid(2, 5, 0, 4, 12, 1, fbNB)
	s.solid(8, 5, 0, 10, 12, 1, fbNB)
	s.solid(5, 9, 0, 7, 12, 1, fbNB)
	s.solid(2, 11, 2, 10, 12, 10, fbNB)
}

func (s *fstamp) bigRoomBattlements() {
	we := fence("west", "east")
	ns := fence("north", "south")
	for i := 1; i <= 11; i += 2 {
		s.solid(i, 10, 0, i, 11, 0, we)
		s.solid(i, 10, 12, i, 11, 12, we)
		s.solid(0, 10, i, 0, 11, i, ns)
		s.solid(12, 10, i, 12, 11, i, ns)
		s.place(fbNB, i, 13, 0)
		s.place(fbNB, i, 13, 12)
		s.place(fbNB, 0, 13, i)
		s.place(fbNB, 12, 13, i)
		if i != 11 {
			s.place(we, i+1, 13, 0)
			s.place(we, i+1, 13, 12)
			s.place(ns, 0, 13, i+1)
			s.place(ns, 12, 13, i+1)
		}
	}
	s.place(fence("north", "east"), 0, 13, 0)
	s.place(fence("south", "east"), 0, 13, 12)
	s.place(fence("south", "west"), 12, 13, 12)
	s.place(fence("north", "west"), 12, 13, 0)
}

func (s *fstamp) bigRoomFoundation() {
	s.solid(4, 2, 0, 8, 2, 12, fbNB)
	s.solid(0, 2, 4, 12, 2, 8, fbNB)
	s.solid(4, 0, 0, 8, 1, 3, fbNB)
	s.solid(4, 0, 9, 8, 1, 12, fbNB)
	s.solid(0, 0, 4, 3, 1, 8, fbNB)
	s.solid(9, 0, 4, 12, 1, 8, fbNB)
	for x := 4; x <= 8; x++ {
		for z := 0; z <= 2; z++ {
			s.column(fbNB, x, -1, z)
			s.column(fbNB, x, -1, 12-z)
		}
	}
	for x := 0; x <= 2; x++ {
		for z := 4; z <= 8; z++ {
			s.column(fbNB, x, -1, z)
			s.column(fbNB, 12-x, -1, z)
		}
	}
}

func (s *fstamp) castleEntrance() {
	s.bigRoomShell()
	s.solid(5, 8, 0, 7, 8, 0, fence())
	s.bigRoomBattlements()
	nsw := fence("north", "south", "west")
	nse := fence("north", "south", "east")
	for z := 3; z <= 9; z += 2 {
		s.solid(1, 7, z, 1, 8, z, nsw)
		s.solid(11, 7, z, 11, 8, z, nse)
	}
	s.bigRoomFoundation()
	s.solid(5, 5, 5, 7, 5, 7, fbNB)
	s.solid(6, 1, 6, 6, 4, 6, fbAir)
	s.place(fbNB, 6, 0, 6)
	s.place(fbLava, 6, 5, 6)
}

func (s *fstamp) smallCorridorFloorRoof() {
	s.solid(0, 0, 0, 4, 1, 4, fbNB)
	s.solid(0, 2, 0, 4, 5, 4, fbAir)
}

func (s *fstamp) smallCorridorEnd() {
	s.solid(0, 6, 0, 4, 6, 4, fbNB)
	for x := 0; x <= 4; x++ {
		for z := 0; z <= 4; z++ {
			s.column(fbNB, x, -1, z)
		}
	}
}

func (s *fstamp) corridorCrossing() {
	s.smallCorridorFloorRoof()
	s.solid(0, 2, 0, 0, 5, 0, fbNB)
	s.solid(4, 2, 0, 4, 5, 0, fbNB)
	s.solid(0, 2, 4, 0, 5, 4, fbNB)
	s.solid(4, 2, 4, 4, 5, 4, fbNB)
	s.smallCorridorEnd()
}

func (s *fstamp) corridorLeftTurn() {
	s.smallCorridorFloorRoof()
	we := fence("west", "east")
	ns := fence("north", "south")
	s.solid(4, 2, 0, 4, 5, 4, fbNB)
	s.solid(4, 3, 1, 4, 4, 1, ns)
	s.solid(4, 3, 3, 4, 4, 3, ns)
	s.solid(0, 2, 0, 0, 5, 0, fbNB)
	s.solid(0, 2, 4, 3, 5, 4, fbNB)
	s.solid(1, 3, 4, 1, 4, 4, we)
	s.solid(3, 3, 4, 3, 4, 4, we)
	if s.p.chest {
		s.place(fbChest, 3, 2, 3)
	}
	s.smallCorridorEnd()
}

func (s *fstamp) corridor() {
	s.smallCorridorFloorRoof()
	ns := fence("north", "south")
	s.solid(0, 2, 0, 0, 5, 4, fbNB)
	s.solid(4, 2, 0, 4, 5, 4, fbNB)
	s.solid(0, 3, 1, 0, 4, 1, ns)
	s.solid(0, 3, 3, 0, 4, 3, ns)
	s.solid(4, 3, 1, 4, 4, 1, ns)
	s.solid(4, 3, 3, 4, 4, 3, ns)
	s.smallCorridorEnd()
}

func (s *fstamp) corridorRightTurn() {
	s.smallCorridorFloorRoof()
	we := fence("west", "east")
	ns := fence("north", "south")
	s.solid(0, 2, 0, 0, 5, 4, fbNB)
	s.solid(0, 3, 1, 0, 4, 1, ns)
	s.solid(0, 3, 3, 0, 4, 3, ns)
	s.solid(4, 2, 0, 4, 5, 0, fbNB)
	s.solid(1, 2, 4, 4, 5, 4, fbNB)
	s.solid(1, 3, 4, 1, 4, 4, we)
	s.solid(3, 3, 4, 3, 4, 4, we)
	if s.p.chest {
		s.place(fbChest, 1, 2, 3)
	}
	s.smallCorridorEnd()
}

func (s *fstamp) stalkRoom() {
	s.bigRoomShell()
	s.bigRoomBattlements()
	nsw := fence("north", "south", "west")
	nse := fence("north", "south", "east")
	for z := 3; z <= 9; z += 2 {
		s.solid(1, 7, z, 1, 8, z, nsw)
		s.solid(11, 7, z, 11, 8, z, nse)
	}
	st := stairs("north")
	for ix := 0; ix <= 6; ix++ {
		z := ix + 4
		for x := 5; x <= 7; x++ {
			s.place(st, x, 5+ix, z)
		}
		if z >= 5 && z <= 8 {
			s.solid(5, 5, z, 7, ix+4, z, fbNB)
		} else if z >= 9 && z <= 10 {
			s.solid(5, 8, z, 7, ix+4, z, fbNB)
		}
		if ix >= 1 {
			s.solid(5, 6+ix, z, 7, 9+ix, z, fbAir)
		}
	}
	for x := 5; x <= 7; x++ {
		s.place(st, x, 12, 11)
	}
	s.solid(5, 6, 7, 5, 7, 7, nse)
	s.solid(7, 6, 7, 7, 7, 7, nsw)
	s.solid(5, 13, 12, 7, 13, 12, fbAir)
	s.solid(2, 5, 2, 3, 5, 3, fbNB)
	s.solid(2, 5, 9, 3, 5, 10, fbNB)
	s.solid(2, 5, 4, 2, 5, 8, fbNB)
	s.solid(9, 5, 2, 10, 5, 3, fbNB)
	s.solid(9, 5, 9, 10, 5, 10, fbNB)
	s.solid(10, 5, 4, 10, 5, 8, fbNB)
	east, west := stairs("east"), stairs("west")
	s.place(west, 4, 5, 2)
	s.place(west, 4, 5, 3)
	s.place(west, 4, 5, 9)
	s.place(west, 4, 5, 10)
	s.place(east, 8, 5, 2)
	s.place(east, 8, 5, 3)
	s.place(east, 8, 5, 9)
	s.place(east, 8, 5, 10)
	s.solid(3, 4, 4, 4, 4, 8, fbSoulSand)
	s.solid(8, 4, 4, 9, 4, 8, fbSoulSand)
	s.solid(3, 5, 4, 4, 5, 8, fbWart)
	s.solid(8, 5, 4, 9, 5, 8, fbWart)
	s.bigRoomFoundation()
}

func (s *fstamp) monsterThrone() {
	s.solid(0, 2, 0, 6, 7, 7, fbAir)
	s.solid(1, 0, 0, 5, 1, 7, fbNB)
	s.solid(1, 2, 1, 5, 2, 7, fbNB)
	s.solid(1, 3, 2, 5, 3, 7, fbNB)
	s.solid(1, 4, 3, 5, 4, 7, fbNB)
	s.solid(1, 2, 0, 1, 4, 2, fbNB)
	s.solid(5, 2, 0, 5, 4, 2, fbNB)
	s.solid(1, 5, 2, 1, 5, 3, fbNB)
	s.solid(5, 5, 2, 5, 5, 3, fbNB)
	s.solid(0, 5, 3, 0, 5, 8, fbNB)
	s.solid(6, 5, 3, 6, 5, 8, fbNB)
	s.solid(1, 5, 8, 5, 5, 8, fbNB)
	we := fence("west", "east")
	ns := fence("north", "south")
	s.place(fence("west"), 1, 6, 3)
	s.place(fence("east"), 5, 6, 3)
	s.place(fence("east", "north"), 0, 6, 3)
	s.place(fence("west", "north"), 6, 6, 3)
	s.solid(0, 6, 4, 0, 6, 7, ns)
	s.solid(6, 6, 4, 6, 6, 7, ns)
	s.place(fence("east", "south"), 0, 6, 8)
	s.place(fence("west", "south"), 6, 6, 8)
	s.solid(1, 6, 8, 5, 6, 8, we)
	s.place(fence("east"), 1, 7, 8)
	s.solid(2, 7, 8, 4, 7, 8, we)
	s.place(fence("west"), 5, 7, 8)
	s.place(fence("east"), 2, 8, 8)
	s.place(we, 3, 8, 8)
	s.place(fence("west"), 4, 8, 8)
	s.place(fbSpawner, 3, 5, 5)
	for x := 0; x <= 6; x++ {
		for z := 0; z <= 6; z++ {
			s.column(fbNB, x, -1, z)
		}
	}
}

func (s *fstamp) roomCrossing() {
	s.solid(0, 0, 0, 6, 1, 6, fbNB)
	s.solid(0, 2, 0, 6, 7, 6, fbAir)
	s.solid(0, 2, 0, 1, 6, 0, fbNB)
	s.solid(0, 2, 6, 1, 6, 6, fbNB)
	s.solid(5, 2, 0, 6, 6, 0, fbNB)
	s.solid(5, 2, 6, 6, 6, 6, fbNB)
	s.solid(0, 2, 0, 0, 6, 1, fbNB)
	s.solid(0, 2, 5, 0, 6, 6, fbNB)
	s.solid(6, 2, 0, 6, 6, 1, fbNB)
	s.solid(6, 2, 5, 6, 6, 6, fbNB)
	we := fence("west", "east")
	ns := fence("north", "south")
	s.solid(2, 6, 0, 4, 6, 0, fbNB)
	s.solid(2, 5, 0, 4, 5, 0, we)
	s.solid(2, 6, 6, 4, 6, 6, fbNB)
	s.solid(2, 5, 6, 4, 5, 6, we)
	s.solid(0, 6, 2, 0, 6, 4, fbNB)
	s.solid(0, 5, 2, 0, 5, 4, ns)
	s.solid(6, 6, 2, 6, 6, 4, fbNB)
	s.solid(6, 5, 2, 6, 5, 4, ns)
	for x := 0; x <= 6; x++ {
		for z := 0; z <= 6; z++ {
			s.column(fbNB, x, -1, z)
		}
	}
}

func (s *fstamp) stairsRoom() {
	s.solid(0, 0, 0, 6, 1, 6, fbNB)
	s.solid(0, 2, 0, 6, 10, 6, fbAir)
	s.solid(0, 2, 0, 1, 8, 0, fbNB)
	s.solid(5, 2, 0, 6, 8, 0, fbNB)
	s.solid(0, 2, 1, 0, 8, 6, fbNB)
	s.solid(6, 2, 1, 6, 8, 6, fbNB)
	s.solid(1, 2, 6, 5, 8, 6, fbNB)
	we := fence("west", "east")
	ns := fence("north", "south")
	s.solid(0, 3, 2, 0, 5, 4, ns)
	s.solid(6, 3, 2, 6, 5, 2, ns)
	s.solid(6, 3, 4, 6, 5, 4, ns)
	s.place(fbNB, 5, 2, 5)
	s.solid(4, 2, 5, 4, 3, 5, fbNB)
	s.solid(3, 2, 5, 3, 4, 5, fbNB)
	s.solid(2, 2, 5, 2, 5, 5, fbNB)
	s.solid(1, 2, 5, 1, 6, 5, fbNB)
	s.solid(1, 7, 1, 5, 7, 4, fbNB)
	s.solid(6, 8, 2, 6, 8, 4, fbAir)
	s.solid(2, 6, 0, 4, 8, 0, fbNB)
	s.solid(2, 5, 0, 4, 5, 0, we)
	for x := 0; x <= 6; x++ {
		for z := 0; z <= 6; z++ {
			s.column(fbNB, x, -1, z)
		}
	}
}
