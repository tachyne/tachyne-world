package worldgen

import "sync"

// Strongholds — a port of vanilla's stronghold piece generator
// (StrongholdPieces). A spiral staircase at the site grows a maze of
// small-door pieces drawn from a weighted table: straight corridors with
// side doors, prison halls, left and right turns, room crossings (a
// pillar with torches, a fountain, or a gallery with a chest), straight
// and spiral stairs, five-way crossings, chest corridors, libraries and
// the portal room, each capped per stronghold. The first piece off the
// start is always a five-way crossing, libraries wait for depth 5 and the
// portal room for depth 6, a piece never follows itself, growth stops 50
// pieces deep or 112 blocks from the start, and a dead end that runs into
// another piece gets a filler corridor to join it. A stronghold without a
// portal room is thrown away and rolled again. The finished maze is sunk
// below sea level.
//
// Every wall, floor and ceiling is drawn with the stone-brick selector
// (cracked, mossy and infested bricks among the plain ones), and the shell
// passes leave air where a cave already cut through. Placement stays on
// the engine's own grid (one candidate per 1536-block cell).

const (
	strongholdCell = 1536
	strongholdOdds = 0.6

	shMaxDepth = 50
	shReach    = 112
	shStartY   = 64
)

var (
	EndPortalFrame = blockBase("end_portal_frame") // + eye(2)×4 + facing(4); eye=true is the LOW half
)

// shKind is a stronghold piece kind.
type shKind int

const (
	shStraight shKind = iota
	shPrisonHall
	shLeftTurn
	shRightTurn
	shRoomCrossing
	shStraightStairsDown
	shStairsDown
	shFiveCrossing
	shChestCorridor
	shLibrary
	shPortalRoom
	shFillerCorridor
	shStart
	shKinds
)

var shKindNames = [shKinds]string{
	"straight", "prison_hall", "left_turn", "right_turn", "room_crossing", "straight_stairs_down",
	"stairs_down", "five_crossing", "chest_corridor", "library", "portal_room", "filler_corridor", "start",
}

// shDoor is SmallDoorType.
type shDoor int

const (
	doorOpening shDoor = iota
	doorWood
	doorGrates
	doorIron
)

// shPiece is one placed stronghold piece.
type shPiece struct {
	opiece
	kind  shKind
	depth int
	door  shDoor
	// Per-kind flags: straight (left, right); five crossing (leftLow,
	// leftHigh, rightLow, rightHigh); library (tall); start (source).
	f1, f2, f3, f4 bool
	typ            int // room crossing: 0 pillar, 1 fountain, 2 gallery, 3-4 bare
	steps          int // filler corridor length
}

// shWeight is a row of the piece table (PieceWeight).
type shWeight struct {
	kind             shKind
	weight           int
	placed, maxPlace int
	minDepth         int // doPlace also needs depth > minDepth (library 4, portal room 5)
}

func (w *shWeight) doPlace(depth int) bool {
	return (w.maxPlace == 0 || w.placed < w.maxPlace) && depth > w.minDepth
}
func (w *shWeight) valid() bool { return w.maxPlace == 0 || w.placed < w.maxPlace }

func strongholdWeights() []*shWeight {
	return []*shWeight{
		{kind: shStraight, weight: 40},
		{kind: shPrisonHall, weight: 5, maxPlace: 5},
		{kind: shLeftTurn, weight: 20},
		{kind: shRightTurn, weight: 20},
		{kind: shRoomCrossing, weight: 10, maxPlace: 6},
		{kind: shStraightStairsDown, weight: 5, maxPlace: 5},
		{kind: shStairsDown, weight: 5, maxPlace: 5},
		{kind: shFiveCrossing, weight: 5, maxPlace: 4},
		{kind: shChestCorridor, weight: 5, maxPlace: 4},
		{kind: shLibrary, weight: 10, maxPlace: 2, minDepth: 4},
		{kind: shPortalRoom, weight: 20, maxPlace: 1, minDepth: 5},
	}
}

// shShape is each kind's oriented box (offX, offY, offZ, width, height, depth).
var shShape = map[shKind][6]int{
	shStraight:           {-1, -1, 0, 5, 5, 7},
	shPrisonHall:         {-1, -1, 0, 9, 5, 11},
	shLeftTurn:           {-1, -1, 0, 5, 5, 5},
	shRightTurn:          {-1, -1, 0, 5, 5, 5},
	shRoomCrossing:       {-4, -1, 0, 11, 7, 11},
	shStraightStairsDown: {-1, -7, 0, 5, 11, 8},
	shStairsDown:         {-1, -7, 0, 5, 11, 5},
	shFiveCrossing:       {-4, -3, 0, 10, 9, 11},
	shChestCorridor:      {-1, -1, 0, 5, 5, 7},
	shLibrary:            {-4, -1, 0, 14, 11, 15},
	shPortalRoom:         {-4, -1, 0, 11, 8, 16},
}

// ---- generation -------------------------------------------------------------

type shGen struct {
	rng     *jigsawRNG
	pieces  []*shPiece
	pending []*shPiece
	weights []*shWeight
	total   int
	imposed shKind // -1: none
	prev    *shWeight
	start   *shPiece
	portal  *shPiece
}

func (g *shGen) collision(b fbox) *shPiece {
	for _, p := range g.pieces {
		if p.box.intersects(b) {
			return p
		}
	}
	return nil
}

func (g *shGen) okBox(b fbox) bool { return b.y0 > 10 && g.collision(b) == nil }

func (g *shGen) randomDoor() shDoor {
	switch g.rng.intn(5) {
	case 2:
		return doorWood
	case 3:
		return doorGrates
	case 4:
		return doorIron
	}
	return doorOpening
}

// create is findAndCreatePieceFactory: the kind's box at a foot, if it fits.
func (g *shGen) create(kind shKind, fx, fy, fz int, dir fdir, depth int) *shPiece {
	s := shShape[kind]
	box := orientBox(fx, fy, fz, s[0], s[1], s[2], s[3], s[4], s[5], dir)
	if kind == shLibrary && !g.okBox(box) {
		box = orientBox(fx, fy, fz, s[0], s[1], s[2], s[3], 6, s[5], dir) // the short library
	}
	if !g.okBox(box) {
		return nil
	}
	p := &shPiece{opiece: newOPiece(box, dir), kind: kind, depth: depth}
	if kind != shPortalRoom {
		p.door = g.randomDoor()
	}
	switch kind {
	case shStraight:
		p.f1 = g.rng.intn(2) == 0
		p.f2 = g.rng.intn(2) == 0
	case shFiveCrossing:
		p.f1 = g.rng.intn(2) == 0
		p.f2 = g.rng.intn(2) == 0
		p.f3 = g.rng.intn(2) == 0
		p.f4 = g.rng.intn(3) > 0
	case shRoomCrossing:
		p.typ = g.rng.intn(5)
	case shLibrary:
		p.f1 = box.y1-box.y0+1 > 6
	}
	return p
}

func (g *shGen) updateWeights() bool {
	any := false
	g.total = 0
	for _, w := range g.weights {
		if w.maxPlace > 0 && w.placed < w.maxPlace {
			any = true
		}
		g.total += w.weight
	}
	return any
}

// fromSmallDoor is generatePieceFromSmallDoor.
func (g *shGen) fromSmallDoor(fx, fy, fz int, dir fdir, depth int) *shPiece {
	if !g.updateWeights() {
		return nil
	}
	if g.imposed >= 0 {
		k := g.imposed
		g.imposed = -1
		if p := g.create(k, fx, fy, fz, dir, depth); p != nil {
			return p
		}
	}
	for attempt := 0; attempt < 5; attempt++ {
		sel := g.rng.intn(g.total)
		for i, w := range g.weights {
			sel -= w.weight
			if sel >= 0 {
				continue
			}
			if !w.doPlace(depth) || w == g.prev {
				break
			}
			if p := g.create(w.kind, fx, fy, fz, dir, depth); p != nil {
				w.placed++
				g.prev = w
				if !w.valid() {
					g.weights = append(g.weights[:i:i], g.weights[i+1:]...)
				}
				return p
			}
		}
	}
	if box, ok := g.fillerBox(fx, fy, fz, dir); ok && box.y0 > 1 {
		p := &shPiece{opiece: newOPiece(box, dir), kind: shFillerCorridor, depth: depth}
		if dir == fNorth || dir == fSouth {
			p.steps = box.z1 - box.z0 + 1
		} else {
			p.steps = box.x1 - box.x0 + 1
		}
		return p
	}
	return nil
}

// fillerBox is FillerCorridor.findPieceBox: a short corridor that closes the
// gap to a piece already standing in the way, at the same floor.
func (g *shGen) fillerBox(fx, fy, fz int, dir fdir) (fbox, bool) {
	box := orientBox(fx, fy, fz, -1, -1, 0, 5, 5, 4, dir)
	c := g.collision(box)
	if c == nil || c.box.y0 != box.y0 {
		return fbox{}, false
	}
	for d := 2; d >= 1; d-- {
		if !c.box.intersects(orientBox(fx, fy, fz, -1, -1, 0, 5, 5, d, dir)) {
			return orientBox(fx, fy, fz, -1, -1, 0, 5, 5, d+1, dir), true
		}
	}
	return fbox{}, false
}

func (g *shGen) add(fx, fy, fz int, dir fdir, depth int) {
	if depth > shMaxDepth {
		return
	}
	if abs(fx-g.start.box.x0) > shReach || abs(fz-g.start.box.z0) > shReach {
		return
	}
	if p := g.fromSmallDoor(fx, fy, fz, dir, depth+1); p != nil {
		g.pieces = append(g.pieces, p)
		g.pending = append(g.pending, p)
	}
}

func (g *shGen) forward(p *shPiece, xOff, yOff int) {
	b := p.box
	switch p.dir {
	case fNorth:
		g.add(b.x0+xOff, b.y0+yOff, b.z0-1, p.dir, p.depth)
	case fSouth:
		g.add(b.x0+xOff, b.y0+yOff, b.z1+1, p.dir, p.depth)
	case fWest:
		g.add(b.x0-1, b.y0+yOff, b.z0+xOff, p.dir, p.depth)
	case fEast:
		g.add(b.x1+1, b.y0+yOff, b.z0+xOff, p.dir, p.depth)
	}
}

func (g *shGen) left(p *shPiece, yOff, zOff int) {
	b := p.box
	if p.dir == fNorth || p.dir == fSouth {
		g.add(b.x0-1, b.y0+yOff, b.z0+zOff, fWest, p.depth)
	} else {
		g.add(b.x0+zOff, b.y0+yOff, b.z0-1, fNorth, p.depth)
	}
}

func (g *shGen) right(p *shPiece, yOff, zOff int) {
	b := p.box
	if p.dir == fNorth || p.dir == fSouth {
		g.add(b.x1+1, b.y0+yOff, b.z0+zOff, fEast, p.depth)
	} else {
		g.add(b.x0+zOff, b.y0+yOff, b.z1+1, fSouth, p.depth)
	}
}

// addChildren is each kind's growth rule.
func (g *shGen) addChildren(p *shPiece) {
	switch p.kind {
	case shStart, shStairsDown:
		if p.f1 { // the start: its first child is a five-way crossing
			g.imposed = shFiveCrossing
		}
		g.forward(p, 1, 1)
	case shStraight:
		g.forward(p, 1, 1)
		if p.f1 {
			g.left(p, 1, 2)
		}
		if p.f2 {
			g.right(p, 1, 2)
		}
	case shPrisonHall, shStraightStairsDown, shChestCorridor:
		g.forward(p, 1, 1)
	case shLeftTurn:
		if p.dir != fNorth && p.dir != fEast {
			g.right(p, 1, 1)
		} else {
			g.left(p, 1, 1)
		}
	case shRightTurn:
		if p.dir != fNorth && p.dir != fEast {
			g.left(p, 1, 1)
		} else {
			g.right(p, 1, 1)
		}
	case shRoomCrossing:
		g.forward(p, 4, 1)
		g.left(p, 1, 4)
		g.right(p, 1, 4)
	case shFiveCrossing:
		a, b := 3, 5
		if p.dir == fWest || p.dir == fNorth {
			a, b = 8-a, 8-b
		}
		g.forward(p, 5, 1)
		if p.f1 {
			g.left(p, a, 1)
		}
		if p.f2 {
			g.left(p, b, 7)
		}
		if p.f3 {
			g.right(p, a, 1)
		}
		if p.f4 {
			g.right(p, b, 7)
		}
	case shPortalRoom:
		g.portal = p
	}
}

// assembleStronghold lays out the stronghold whose start sits in chunk
// (cx,cz), retrying until one holds a portal room.
func assembleStronghold(seed int64, cx, cz int) []*shPiece {
	for tries := int64(0); ; tries++ {
		g := &shGen{rng: newJigsawRNG(seed+tries, cx, cz), weights: strongholdWeights(), imposed: -1}
		dir := fdir(g.rng.intn(4))
		x, z := cx*16+2, cz*16+2
		start := &shPiece{opiece: newOPiece(fbox{x, shStartY, z, x + 4, shStartY + 10, z + 4}, dir), kind: shStart, f1: true}
		g.start = start
		g.pieces = append(g.pieces, start)
		g.addChildren(start)
		for len(g.pending) > 0 {
			i := g.rng.intn(len(g.pending))
			p := g.pending[i]
			g.pending = append(g.pending[:i], g.pending[i+1:]...)
			g.addChildren(p)
		}
		if g.portal == nil && tries < 64 {
			continue
		}
		// moveBelowSeaLevel(sea level, min y, 10): the top lands at or below 53.
		bb := g.pieces[0].box
		for _, p := range g.pieces {
			bb = bb.union(p.box)
		}
		maxY := SeaLevel - 10
		top := bb.y1 - bb.y0 + 1 + MinY + 1
		if top < maxY {
			top += g.rng.intn(maxY - top)
		}
		dy := top - bb.y1
		for _, p := range g.pieces {
			p.box.y0 += dy
			p.box.y1 += dy
		}
		return g.pieces
	}
}

// ---- siting + cache ---------------------------------------------------------

// Stronghold describes one generated stronghold, located by its portal room.
type Stronghold struct {
	X, Z int // portal-room centre (the 3x3 portal interior centre)
	Y    int // the frame ring's level
	// LocX, LocZ are StructurePlacement.getLocatePos: the minimum corner of
	// the chunk the stronghold starts in (its staircase), which is where an
	// eye of ender flies and what /locate reports — not the portal room.
	LocX, LocZ int
	Exists     bool
	room       *shPiece
	pieces     []*shPiece
}

type strongholdKey struct {
	seed   int64
	cx, cz int
}

var (
	strongholdCache = map[strongholdKey][]*shPiece{}
	strongholdMu    sync.Mutex
)

func (g *Generator) strongholdPieces(cx, cz int) []*shPiece {
	k := strongholdKey{g.seed, cx, cz}
	strongholdMu.Lock()
	p, ok := strongholdCache[k]
	strongholdMu.Unlock()
	if ok {
		return p
	}
	p = assembleStronghold(g.seed, cx, cz)
	strongholdMu.Lock()
	if len(strongholdCache) > 256 {
		strongholdCache = map[strongholdKey][]*shPiece{}
	}
	strongholdCache[k] = p
	strongholdMu.Unlock()
	return p
}

// StrongholdIn rolls the stronghold for the cell containing (wx,wz). The cell
// around the origin never generates one (vanilla: the first ring is ~1500+).
func (g *Generator) StrongholdIn(wx, wz int) Stronghold {
	ox, oz := cellOrigin(wx, strongholdCell), cellOrigin(wz, strongholdCell)
	if ox == cellOrigin(0, strongholdCell) && oz == cellOrigin(0, strongholdCell) {
		return Stronghold{}
	}
	if hash01(g.seed, ox, oz, 0x57011) >= strongholdOdds {
		return Stronghold{}
	}
	x := ox + 200 + int(hash01(g.seed, ox, oz, 0x5702)*float64(strongholdCell-400))
	z := oz + 200 + int(hash01(g.seed, ox, oz, 0x5703)*float64(strongholdCell-400))
	pieces := g.strongholdPieces(x>>4, z>>4)
	st := Stronghold{Exists: true, pieces: pieces, LocX: (x >> 4) << 4, LocZ: (z >> 4) << 4}
	for _, p := range pieces {
		if p.kind == shPortalRoom {
			st.room = p
			c := p.world(5, 3, 10)
			st.X, st.Y, st.Z = c[0], c[1], c[2]
			return st
		}
	}
	return Stronghold{} // no portal room in 64 tries: never in practice
}

// StrongholdsNear is the stronghold of the cell around (wx,wz) and its
// neighbours' (a stronghold reaches ~130 blocks from its start).
func (g *Generator) StrongholdsNear(wx, wz int) []Stronghold {
	var out []Stronghold
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if st := g.StrongholdIn(wx+dx*strongholdCell, wz+dz*strongholdCell); st.Exists {
				out = append(out, st)
			}
		}
	}
	return out
}

// PieceCounts reports how many pieces of each kind the stronghold holds,
// by name.
func (st Stronghold) PieceCounts() map[string]int {
	out := map[string]int{}
	for _, p := range st.pieces {
		out[shKindNames[p.kind]]++
	}
	return out
}

// Bounds is the stronghold's whole extent.
func (st Stronghold) Bounds() (x0, y0, z0, x1, y1, z1 int) {
	if len(st.pieces) == 0 {
		return
	}
	b := st.pieces[0].box
	for _, p := range st.pieces {
		b = b.union(p.box)
	}
	return b.x0, b.y0, b.z0, b.x1, b.y1, b.z1
}

// Contains reports whether a cell lies inside one of the stronghold's pieces.
func (st Stronghold) Contains(x, y, z int) bool {
	for _, p := range st.pieces {
		if p.box.inside(x, y, z) {
			return true
		}
	}
	return false
}

// frameEye is the portal room's roll for one frame's eye (nextFloat > 0.9).
func frameEye(seed int64, w [3]int) bool { return hash3(seed, w[0], w[1], w[2], 0x5704) > 0.9 }

type shFrame struct {
	x, z   int
	facing string
}

// shFrames are the portal room's twelve frames in local coordinates.
var shFrames = [12]shFrame{
	{4, 8, "north"}, {5, 8, "north"}, {6, 8, "north"},
	{4, 12, "south"}, {5, 12, "south"}, {6, 12, "south"},
	{3, 9, "east"}, {3, 10, "east"}, {3, 11, "east"},
	{7, 9, "west"}, {7, 10, "west"}, {7, 11, "west"},
}

func (p *shPiece) frameState(seed int64, f shFrame) ([3]int, uint32) {
	w := p.world(f.x, 3, f.z)
	eye := "false"
	if frameEye(seed, w) {
		eye = "true"
	}
	return w, bsp("end_portal_frame", "facing="+f.facing+",eye="+eye).state(p.rot, p.mir)
}

// FramePositions lists the twelve end-portal frames around the portal
// interior, with each frame's block state.
func (st Stronghold) FramePositions(seed int64) [12]struct {
	X, Y, Z int
	State   uint32
} {
	var out [12]struct {
		X, Y, Z int
		State   uint32
	}
	if st.room == nil {
		return out
	}
	for i, f := range shFrames {
		w, s := st.room.frameState(seed, f)
		out[i].X, out[i].Y, out[i].Z, out[i].State = w[0], w[1], w[2], s
	}
	return out
}

// StrongholdChest is a loot chest a stronghold placed.
type StrongholdChest struct {
	X, Y, Z int
	Table   string
}

// Chests lists the stronghold's loot chests: the chest corridors'
// (chests/stronghold_corridor), the gallery crossings'
// (chests/stronghold_crossing) and the libraries' (chests/stronghold_library).
func (st Stronghold) Chests() []StrongholdChest {
	var out []StrongholdChest
	add := func(w [3]int, table string) { out = append(out, StrongholdChest{w[0], w[1], w[2], table}) }
	for _, p := range st.pieces {
		switch p.kind {
		case shChestCorridor:
			add(p.world(3, 2, 3), "chests/stronghold_corridor")
		case shRoomCrossing:
			if p.typ == 2 {
				add(p.world(3, 4, 8), "chests/stronghold_crossing")
			}
		case shLibrary:
			add(p.world(3, 3, 5), "chests/stronghold_library")
			if p.f1 {
				add(p.world(12, 8, 1), "chests/stronghold_library")
			}
		}
	}
	return out
}

// Spawner is the portal room's silverfish spawner.
func (st Stronghold) Spawner() ([3]int, bool) {
	if st.room == nil {
		return [3]int{}, false
	}
	return st.room.world(5, 3, 6), true
}

// ---- stamping -----------------------------------------------------------------

// stampStrongholds draws the stronghold pieces crossing this chunk.
func (g *Generator) stampStrongholds(ch *Chunk, cx, cz int32) {
	s := newChunkStamp(g, ch, cx, cz)
	for _, st := range g.StrongholdsNear(int(cx)*16+8, int(cz)*16+8) {
		for i, p := range st.pieces {
			if !p.box.intersects(s.cb) {
				continue
			}
			s.p, s.salt = &p.opiece, 0x5700+uint64(i)*0x9E37
			s.drawStronghold(p)
		}
	}
	s.reshape()
}

var (
	sbStoneBricks = bs("stone_bricks")
	sbSSSlab      = bs("smooth_stone_slab")
	sbSBSlab      = bs("stone_brick_slab")
	sbWallTorch   = bs("wall_torch")
	sbOakPlanks   = bs("oak_planks")
	sbBookshelf   = bs("bookshelf")
	sbCobble      = bs("cobblestone")
	sbIronBars    = bs("iron_bars")
	sbOakFence    = bs("oak_fence")
)

// shSelect is the smooth-stone selector: the faces of a box are stone
// bricks, one in five cracked, three in ten mossy and one in twenty
// infested; the inside is air.
func (s *pstamp) shSelect(wx, wy, wz int, edge bool) uint32 {
	if !edge {
		return Air
	}
	switch r := hash3(s.g.seed, wx, wy, wz, 0x5705); {
	case r < 0.2:
		return blockID("cracked_stone_bricks")
	case r < 0.5:
		return blockID("mossy_stone_bricks")
	case r < 0.55:
		return blockID("infested_stone_bricks")
	}
	return blockID("stone_bricks")
}

func (s *pstamp) shBox(x0, y0, z0, x1, y1, z1 int, skipAir bool) {
	s.boxSel(x0, y0, z0, x1, y1, z1, skipAir, s.shSelect)
}

// door is generateSmallDoor.
func (s *pstamp) door(d shDoor, x, y, z int) {
	switch d {
	case doorOpening:
		s.air(x, y, z, x+2, y+2, z)
	case doorWood, doorIron:
		for _, c := range [][2]int{{0, 0}, {0, 1}, {0, 2}, {1, 2}, {2, 2}, {2, 1}, {2, 0}} {
			s.place(sbStoneBricks, x+c[0], y+c[1], z)
		}
		name := "oak_door"
		if d == doorIron {
			name = "iron_door"
		}
		s.place(bs(name), x+1, y, z)
		s.place(bsp(name, "half=upper"), x+1, y+1, z)
		if d == doorIron {
			s.place(bsp("stone_button", "facing=north"), x+2, y+1, z+1)
			s.place(bsp("stone_button", "facing=south"), x+2, y+1, z-1)
		}
	case doorGrates:
		s.place(bs("air"), x+1, y, z)
		s.place(bs("air"), x+1, y+1, z)
		s.place(sbIronBars.with("west=true"), x, y, z)
		s.place(sbIronBars.with("west=true"), x, y+1, z)
		ew := sbIronBars.with("east=true,west=true")
		s.place(ew, x, y+2, z)
		s.place(ew, x+1, y+2, z)
		s.place(ew, x+2, y+2, z)
		s.place(sbIronBars.with("east=true"), x+2, y+1, z)
		s.place(sbIronBars.with("east=true"), x+2, y, z)
	}
}

func (s *pstamp) drawStronghold(p *shPiece) {
	switch p.kind {
	case shStart, shStairsDown:
		s.stairsDown(p)
	case shStraight:
		s.straight(p)
	case shPrisonHall:
		s.prisonHall(p)
	case shLeftTurn, shRightTurn:
		s.turn(p)
	case shRoomCrossing:
		s.roomCrossing(p)
	case shStraightStairsDown:
		s.straightStairsDown(p)
	case shFiveCrossing:
		s.fiveCrossing(p)
	case shChestCorridor:
		s.chestCorridor(p)
	case shLibrary:
		s.library(p)
	case shPortalRoom:
		s.portalRoom(p)
	case shFillerCorridor:
		s.fillerCorridor(p)
	}
}

func (s *pstamp) stairsDown(p *shPiece) {
	s.shBox(0, 0, 0, 4, 10, 4, true)
	s.door(p.door, 1, 7, 0)
	s.door(doorOpening, 1, 1, 4)
	for _, c := range []struct {
		x, y, z int
		slab    bool
	}{
		{2, 6, 1, false}, {1, 5, 1, false}, {1, 6, 1, true}, {1, 5, 2, false}, {1, 4, 3, false},
		{1, 5, 3, true}, {2, 4, 3, false}, {3, 3, 3, false}, {3, 4, 3, true}, {3, 3, 2, false},
		{3, 2, 1, false}, {3, 3, 1, true}, {2, 2, 1, false}, {1, 1, 1, false}, {1, 2, 1, true},
		{1, 1, 2, false}, {1, 1, 3, true},
	} {
		if c.slab {
			s.place(sbSSSlab, c.x, c.y, c.z)
		} else {
			s.place(sbStoneBricks, c.x, c.y, c.z)
		}
	}
}

func (s *pstamp) straight(p *shPiece) {
	s.shBox(0, 0, 0, 4, 4, 6, true)
	s.door(p.door, 1, 1, 0)
	s.door(doorOpening, 1, 1, 6)
	east, west := sbWallTorch.with("facing=east"), sbWallTorch.with("facing=west")
	s.maybeBlock(0.1, 1, 2, 1, east, 1)
	s.maybeBlock(0.1, 3, 2, 1, west, 2)
	s.maybeBlock(0.1, 1, 2, 5, east, 3)
	s.maybeBlock(0.1, 3, 2, 5, west, 4)
	if p.f1 {
		s.air(0, 1, 2, 0, 3, 4)
	}
	if p.f2 {
		s.air(4, 1, 2, 4, 3, 4)
	}
}

func (s *pstamp) prisonHall(p *shPiece) {
	s.shBox(0, 0, 0, 8, 4, 10, true)
	s.door(p.door, 1, 1, 0)
	s.air(1, 1, 10, 3, 3, 10)
	s.shBox(4, 1, 1, 4, 3, 1, false)
	s.shBox(4, 1, 3, 4, 3, 3, false)
	s.shBox(4, 1, 7, 4, 3, 7, false)
	s.shBox(4, 1, 9, 4, 3, 9, false)
	ns := sbIronBars.with("north=true,south=true")
	we := sbIronBars.with("west=true,east=true")
	for y := 1; y <= 3; y++ {
		s.place(ns, 4, y, 4)
		s.place(ns.with("east=true"), 4, y, 5)
		s.place(ns, 4, y, 6)
		s.place(we, 5, y, 5)
		s.place(we, 6, y, 5)
		s.place(we, 7, y, 5)
	}
	s.place(ns, 4, 3, 2)
	s.place(ns, 4, 3, 8)
	lower, upper := bsp("iron_door", "facing=west"), bsp("iron_door", "facing=west,half=upper")
	s.place(lower, 4, 1, 2)
	s.place(upper, 4, 2, 2)
	s.place(lower, 4, 1, 8)
	s.place(upper, 4, 2, 8)
}

func (s *pstamp) turn(p *shPiece) {
	s.shBox(0, 0, 0, 4, 4, 4, true)
	s.door(p.door, 1, 1, 0)
	towardLeft := p.dir != fNorth && p.dir != fEast // the left turn's opening is at x=4 here
	if p.kind == shRightTurn {
		towardLeft = !towardLeft
	}
	if towardLeft {
		s.air(4, 1, 1, 4, 3, 3)
	} else {
		s.air(0, 1, 1, 0, 3, 3)
	}
}

func (s *pstamp) roomCrossing(p *shPiece) {
	s.shBox(0, 0, 0, 10, 6, 10, true)
	s.door(p.door, 4, 1, 0)
	s.air(4, 1, 10, 6, 3, 10)
	s.air(0, 1, 4, 0, 3, 6)
	s.air(10, 1, 4, 10, 3, 6)
	switch p.typ {
	case 0: // the pillar
		s.place(sbStoneBricks, 5, 1, 5)
		s.place(sbStoneBricks, 5, 2, 5)
		s.place(sbStoneBricks, 5, 3, 5)
		s.place(sbWallTorch.with("facing=west"), 4, 3, 5)
		s.place(sbWallTorch.with("facing=east"), 6, 3, 5)
		s.place(sbWallTorch.with("facing=south"), 5, 3, 4)
		s.place(sbWallTorch.with("facing=north"), 5, 3, 6)
		for _, c := range [][2]int{{4, 4}, {4, 5}, {4, 6}, {6, 4}, {6, 5}, {6, 6}, {5, 4}, {5, 6}} {
			s.place(sbSSSlab, c[0], 1, c[1])
		}
	case 1: // the fountain
		for i := 0; i < 5; i++ {
			s.place(sbStoneBricks, 3, 1, 3+i)
			s.place(sbStoneBricks, 7, 1, 3+i)
			s.place(sbStoneBricks, 3+i, 1, 3)
			s.place(sbStoneBricks, 3+i, 1, 7)
		}
		s.place(sbStoneBricks, 5, 1, 5)
		s.place(sbStoneBricks, 5, 2, 5)
		s.place(sbStoneBricks, 5, 3, 5)
		s.place(bs("water"), 5, 4, 5)
	case 2: // the gallery
		for z := 1; z <= 9; z++ {
			s.place(sbCobble, 1, 3, z)
			s.place(sbCobble, 9, 3, z)
		}
		for x := 1; x <= 9; x++ {
			s.place(sbCobble, x, 3, 1)
			s.place(sbCobble, x, 3, 9)
		}
		for _, c := range [][3]int{{5, 1, 4}, {5, 1, 6}, {5, 3, 4}, {5, 3, 6}, {4, 1, 5}, {6, 1, 5}, {4, 3, 5}, {6, 3, 5}} {
			s.place(sbCobble, c[0], c[1], c[2])
		}
		for y := 1; y <= 3; y++ {
			s.place(sbCobble, 4, y, 4)
			s.place(sbCobble, 6, y, 4)
			s.place(sbCobble, 4, y, 6)
			s.place(sbCobble, 6, y, 6)
		}
		s.place(sbWallTorch, 5, 3, 5)
		for z := 2; z <= 8; z++ {
			s.place(sbOakPlanks, 2, 3, z)
			s.place(sbOakPlanks, 3, 3, z)
			if z <= 3 || z >= 7 {
				s.place(sbOakPlanks, 4, 3, z)
				s.place(sbOakPlanks, 5, 3, z)
				s.place(sbOakPlanks, 6, 3, z)
			}
			s.place(sbOakPlanks, 7, 3, z)
			s.place(sbOakPlanks, 8, 3, z)
		}
		ladder := bsp("ladder", "facing=west")
		s.place(ladder, 9, 1, 3)
		s.place(ladder, 9, 2, 3)
		s.place(ladder, 9, 3, 3)
		s.chest(3, 4, 8)
	}
}

func (s *pstamp) straightStairsDown(p *shPiece) {
	s.shBox(0, 0, 0, 4, 10, 7, true)
	s.door(p.door, 1, 7, 0)
	s.door(doorOpening, 1, 1, 7)
	st := bsp("cobblestone_stairs", "facing=south")
	for i := 0; i < 6; i++ {
		for x := 1; x <= 3; x++ {
			s.place(st, x, 6-i, 1+i)
			if i < 5 {
				s.place(sbStoneBricks, x, 5-i, 1+i)
			}
		}
	}
}

func (s *pstamp) fiveCrossing(p *shPiece) {
	s.shBox(0, 0, 0, 9, 8, 10, true)
	s.door(p.door, 4, 3, 0)
	if p.f1 {
		s.air(0, 3, 1, 0, 5, 3)
	}
	if p.f3 {
		s.air(9, 3, 1, 9, 5, 3)
	}
	if p.f2 {
		s.air(0, 5, 7, 0, 7, 9)
	}
	if p.f4 {
		s.air(9, 5, 7, 9, 7, 9)
	}
	s.air(5, 1, 10, 7, 3, 10)
	s.shBox(1, 2, 1, 8, 2, 6, false)
	s.shBox(4, 1, 5, 4, 4, 9, false)
	s.shBox(8, 1, 5, 8, 4, 9, false)
	s.shBox(1, 4, 7, 3, 4, 9, false)
	s.shBox(1, 3, 5, 3, 3, 6, false)
	s.fill(1, 3, 4, 3, 3, 4, sbSSSlab)
	s.fill(1, 4, 6, 3, 4, 6, sbSSSlab)
	s.shBox(5, 1, 7, 7, 1, 8, false)
	s.fill(5, 1, 9, 7, 1, 9, sbSSSlab)
	s.fill(5, 2, 7, 7, 2, 7, sbSSSlab)
	s.fill(4, 5, 7, 4, 5, 9, sbSSSlab)
	s.fill(8, 5, 7, 8, 5, 9, sbSSSlab)
	s.fill(5, 5, 7, 7, 5, 9, sbSSSlab.with("type=double"))
	s.place(sbWallTorch.with("facing=south"), 6, 5, 6)
}

func (s *pstamp) chestCorridor(p *shPiece) {
	s.shBox(0, 0, 0, 4, 4, 6, true)
	s.door(p.door, 1, 1, 0)
	s.door(doorOpening, 1, 1, 6)
	s.fill(3, 1, 2, 3, 1, 4, sbStoneBricks)
	s.place(sbSBSlab, 3, 1, 1)
	s.place(sbSBSlab, 3, 1, 5)
	s.place(sbSBSlab, 3, 2, 2)
	s.place(sbSBSlab, 3, 2, 4)
	for z := 2; z <= 4; z++ {
		s.place(sbSBSlab, 2, 1, z)
	}
	s.chest(3, 2, 3)
}

func (s *pstamp) library(p *shPiece) {
	tall := p.f1
	h := 6
	if tall {
		h = 11
	}
	s.shBox(0, 0, 0, 13, h-1, 14, true)
	s.door(p.door, 4, 1, 0)
	web := bs("cobweb")
	s.maybeBox(0.07, 2, 1, 1, 11, 4, 13, web, web, false, false, 0x11)
	for d := 1; d <= 13; d++ {
		b := sbBookshelf
		if (d-1)%4 == 0 {
			b = sbOakPlanks
		}
		s.fill(1, 1, d, 1, 4, d, b)
		s.fill(12, 1, d, 12, 4, d, b)
		if (d-1)%4 == 0 {
			s.place(sbWallTorch.with("facing=east"), 2, 3, d)
			s.place(sbWallTorch.with("facing=west"), 11, 3, d)
		}
		if tall {
			s.fill(1, 6, d, 1, 9, d, b)
			s.fill(12, 6, d, 12, 9, d, b)
		}
	}
	for d := 3; d < 12; d += 2 {
		s.fill(3, 1, d, 4, 3, d, sbBookshelf)
		s.fill(6, 1, d, 7, 3, d, sbBookshelf)
		s.fill(9, 1, d, 10, 3, d, sbBookshelf)
	}
	if tall {
		s.fill(1, 5, 1, 3, 5, 13, sbOakPlanks)
		s.fill(10, 5, 1, 12, 5, 13, sbOakPlanks)
		s.fill(4, 5, 1, 9, 5, 2, sbOakPlanks)
		s.fill(4, 5, 12, 9, 5, 13, sbOakPlanks)
		s.place(sbOakPlanks, 9, 5, 11)
		s.place(sbOakPlanks, 8, 5, 11)
		s.place(sbOakPlanks, 9, 5, 10)
		we := sbOakFence.with("west=true,east=true")
		ns := sbOakFence.with("north=true,south=true")
		s.fill(3, 6, 3, 3, 6, 11, ns)
		s.fill(10, 6, 3, 10, 6, 9, ns)
		s.fill(4, 6, 2, 9, 6, 2, we)
		s.fill(4, 6, 12, 7, 6, 12, we)
		s.place(sbOakFence.with("north=true,east=true"), 3, 6, 2)
		s.place(sbOakFence.with("south=true,east=true"), 3, 6, 12)
		s.place(sbOakFence.with("north=true,west=true"), 10, 6, 2)
		for i := 0; i <= 2; i++ {
			s.place(sbOakFence.with("south=true,west=true"), 8+i, 6, 12-i)
			if i != 2 {
				s.place(sbOakFence.with("north=true,east=true"), 8+i, 6, 11-i)
			}
		}
		ladder := bsp("ladder", "facing=south")
		for y := 1; y <= 7; y++ {
			s.place(ladder, 10, y, 13)
		}
		e, w := sbOakFence.with("east=true"), sbOakFence.with("west=true")
		s.place(e, 6, 9, 7)
		s.place(w, 7, 9, 7)
		s.place(e, 6, 8, 7)
		s.place(w, 7, 8, 7)
		nswe := ns.with("west=true,east=true")
		s.place(nswe, 6, 7, 7)
		s.place(nswe, 7, 7, 7)
		s.place(e, 5, 7, 7)
		s.place(w, 8, 7, 7)
		s.place(e.with("north=true"), 6, 7, 6)
		s.place(e.with("south=true"), 6, 7, 8)
		s.place(w.with("north=true"), 7, 7, 6)
		s.place(w.with("south=true"), 7, 7, 8)
		torch := bs("torch")
		for _, c := range [][3]int{{5, 8, 7}, {8, 8, 7}, {6, 8, 6}, {6, 8, 8}, {7, 8, 6}, {7, 8, 8}} {
			s.place(torch, c[0], c[1], c[2])
		}
	}
	s.chest(3, 3, 5)
	if tall {
		s.place(bs("air"), 12, 9, 1)
		s.chest(12, 8, 1)
	}
}

func (s *pstamp) portalRoom(p *shPiece) {
	s.shBox(0, 0, 0, 10, 7, 15, false)
	s.door(doorGrates, 4, 1, 0)
	s.shBox(1, 6, 1, 1, 6, 14, false)
	s.shBox(9, 6, 1, 9, 6, 14, false)
	s.shBox(2, 6, 1, 8, 6, 2, false)
	s.shBox(2, 6, 14, 8, 6, 14, false)
	s.shBox(1, 1, 1, 2, 1, 4, false)
	s.shBox(8, 1, 1, 9, 1, 4, false)
	lava := bs("lava")
	s.fill(1, 1, 1, 1, 1, 3, lava)
	s.fill(9, 1, 1, 9, 1, 3, lava)
	s.shBox(3, 1, 8, 7, 1, 12, false)
	s.fill(4, 1, 9, 6, 1, 11, lava)
	ns := sbIronBars.with("north=true,south=true")
	we := sbIronBars.with("west=true,east=true")
	for z := 3; z < 14; z += 2 {
		s.fill(0, 3, z, 0, 4, z, ns)
		s.fill(10, 3, z, 10, 4, z, ns)
	}
	for x := 2; x < 9; x += 2 {
		s.fill(x, 3, 15, x, 4, 15, we)
	}
	st := bsp("stone_brick_stairs", "facing=north")
	s.shBox(4, 1, 5, 6, 1, 7, false)
	s.shBox(4, 2, 6, 6, 2, 7, false)
	s.shBox(4, 3, 7, 6, 3, 7, false)
	for x := 4; x <= 6; x++ {
		s.place(st, x, 1, 4)
		s.place(st, x, 2, 5)
		s.place(st, x, 3, 6)
	}
	all := true
	for _, f := range shFrames {
		w, state := p.frameState(s.g.seed, f)
		all = all && frameEye(s.g.seed, w)
		s.placeState(state, f.x, 3, f.z)
	}
	if all {
		for x := 4; x <= 6; x++ {
			for z := 9; z <= 11; z++ {
				s.placeState(EndPortalBlock, x, 3, z)
			}
		}
	}
	s.place(bs("spawner"), 5, 3, 6)
}

func (s *pstamp) fillerCorridor(p *shPiece) {
	for i := 0; i < p.steps; i++ {
		for x := 0; x <= 4; x++ {
			s.place(sbStoneBricks, x, 0, i)
			s.place(sbStoneBricks, x, 4, i)
		}
		for y := 1; y <= 3; y++ {
			s.place(sbStoneBricks, 0, y, i)
			s.place(bs("air"), 1, y, i)
			s.place(bs("air"), 2, y, i)
			s.place(bs("air"), 3, y, i)
			s.place(sbStoneBricks, 4, y, i)
		}
	}
}
