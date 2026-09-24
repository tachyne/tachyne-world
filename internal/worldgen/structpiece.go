package worldgen

import (
	"strings"
	"sync"
)

// Shared machinery for the structures vanilla builds from hand-written
// pieces (StructurePiece): strongholds and mineshafts. A piece is a box
// with an optional facing; a facing turns the piece's local (x,y,z) into
// world cells and mirrors/rotates the directional states it places, the
// way StructurePiece.setOrientation prescribes. A piece without a facing
// works in world coordinates directly (the mineshaft room and crossing).
//
// Stamping is per chunk: every write is clipped to the chunk being
// generated, and every random choice a piece makes while it is drawn is
// keyed by the world cell it concerns, so a piece crossing a chunk border
// draws identically on both sides no matter which chunk generates first.

// opiece is the placement shared by every piece kind.
type opiece struct {
	box      fbox
	dir      fdir
	oriented bool // false: local coordinates are world coordinates
	rot, mir int  // the state transform for dir (setOrientation)
}

func newOPiece(box fbox, dir fdir) opiece {
	p := opiece{box: box, dir: dir, oriented: true}
	p.rot, p.mir = orientation(dir)
	return p
}

func (p *opiece) wx(x, z int) int {
	if !p.oriented {
		return x
	}
	switch p.dir {
	case fWest:
		return p.box.x1 - z
	case fEast:
		return p.box.x0 + z
	}
	return p.box.x0 + x
}

func (p *opiece) wy(y int) int {
	if !p.oriented {
		return y
	}
	return p.box.y0 + y
}

func (p *opiece) wz(x, z int) int {
	if !p.oriented {
		return z
	}
	switch p.dir {
	case fNorth:
		return p.box.z1 - z
	case fSouth:
		return p.box.z0 + z
	}
	return p.box.z0 + x
}

func (p *opiece) world(x, y, z int) [3]int { return [3]int{p.wx(x, z), p.wy(y), p.wz(x, z)} }

func (b fbox) inside(x, y, z int) bool {
	return x >= b.x0 && x <= b.x1 && y >= b.y0 && y <= b.y1 && z >= b.z0 && z <= b.z1
}

func (b fbox) union(o fbox) fbox {
	return fbox{min(b.x0, o.x0), min(b.y0, o.y0), min(b.z0, o.z0), max(b.x1, o.x1), max(b.y1, o.y1), max(b.z1, o.z1)}
}

func (b fbox) center() (int, int, int) {
	return b.x0 + (b.x1-b.x0+1)/2, b.y0 + (b.y1-b.y0+1)/2, b.z0 + (b.z1-b.z0+1)/2
}

// ---- block specs ------------------------------------------------------------

// bspec names a block and the properties vanilla sets on it
// ("facing=south,half=upper"); the rest keep the block's default state.
type bspec struct{ name, props string }

func bs(name string) bspec              { return bspec{name: name} }
func bsp(name, props string) bspec      { return bspec{name: name, props: props} }
func (b bspec) with(props string) bspec { return bspec{b.name, joinProps(b.props, props)} }

func joinProps(a, b string) string {
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	return a + "," + b
}

type bspecKey struct {
	b        bspec
	rot, mir int
}

var (
	bspecCache = map[bspecKey]uint32{}
	bspecMu    sync.Mutex
)

// state resolves a spec under a piece's orientation: the default state,
// the named properties mirrored then rotated, a rail's shape turned with
// the piece and a door's hinge flipped by a mirror (DoorBlock.mirror).
func (b bspec) state(rot, mir int) uint32 {
	k := bspecKey{b, rot, mir}
	bspecMu.Lock()
	st, ok := bspecCache[k]
	bspecMu.Unlock()
	if ok {
		return st
	}
	st = blockID(b.name)
	if info, ok := InfoForState(st); ok {
		if b.props != "" {
			for _, kv := range strings.Split(b.props, ",") {
				name, val, _ := strings.Cut(kv, "=")
				name, val = mirrorProp(name, val, mir)
				name, val = rotateProp(name, val, rot)
				if name == "shape" && rot%2 == 1 {
					switch val {
					case "north_south":
						val = "east_west"
					case "east_west":
						val = "north_south"
					}
				}
				if info.HasProperty(name) {
					st = SetProperty(info, st, name, val)
				}
			}
		}
		if mir != mirNone && info.HasProperty("hinge") {
			if GetProperty(info, st, "hinge") == "left" {
				st = SetProperty(info, st, "hinge", "right")
			} else {
				st = SetProperty(info, st, "hinge", "left")
			}
		}
	}
	bspecMu.Lock()
	bspecCache[k] = st
	bspecMu.Unlock()
	return st
}

// ---- per-cell randomness ------------------------------------------------------

// hash3 is hash01 over a block position.
func hash3(seed int64, x, y, z int, salt uint64) float64 {
	return hash01(seed^int64(uint64(int64(y))*0x632be59bd9b4e019), x, z, salt)
}

// ---- stamping -----------------------------------------------------------------

// pstamp draws one piece into one chunk.
type pstamp struct {
	g       *Generator
	ch      *Chunk
	cb      fbox // the chunk's box, full height
	p       *opiece
	salt    uint64            // the piece's own salt for per-cell rolls
	keep    func(uint32) bool // canBeReplaced: cells this piece must not overwrite
	heights map[[2]int]int    // column heights, per chunk
	shaped  *[][3]int         // connectors placed (for the shape pass)
}

func newChunkStamp(g *Generator, ch *Chunk, cx, cz int32) *pstamp {
	shaped := [][3]int{}
	return &pstamp{
		g: g, ch: ch,
		cb:      fbox{int(cx) * 16, MinY, int(cz) * 16, int(cx)*16 + 15, MinY + len(ch.Sections)*16 - 1, int(cz)*16 + 15},
		heights: map[[2]int]int{},
		shaped:  &shaped,
	}
}

func (s *pstamp) getW(x, y, z int) uint32 {
	if !s.cb.inside(x, y, z) {
		return Air
	}
	return sectionBlockAt(s.ch, x-s.cb.x0, y, z-s.cb.z0)
}

func (s *pstamp) setW(x, y, z int, st uint32) {
	if s.cb.inside(x, y, z) {
		setSectionBlock(s.ch, x-s.cb.x0, y, z-s.cb.z0, st, true)
	}
}

// get is getBlock: the cell at a local position, air outside the chunk.
func (s *pstamp) get(x, y, z int) uint32 {
	return s.getW(s.p.wx(x, z), s.p.wy(y), s.p.wz(x, z))
}

func (s *pstamp) height(x, z int) int {
	k := [2]int{x, z}
	if h, ok := s.heights[k]; ok {
		return h
	}
	h := s.g.Height(x, z)
	s.heights[k] = h
	return h
}

// placeState is placeBlock with a resolved state.
func (s *pstamp) placeState(st uint32, x, y, z int) {
	wx, wy, wz := s.p.wx(x, z), s.p.wy(y), s.p.wz(x, z)
	if !s.cb.inside(wx, wy, wz) {
		return
	}
	if s.keep != nil && s.keep(s.getW(wx, wy, wz)) {
		return
	}
	s.setW(wx, wy, wz, st)
	if isShapeChecked(st) {
		*s.shaped = append(*s.shaped, [3]int{wx, wy, wz})
	}
}

// place is placeBlock: a spec at a local cell, turned with the piece.
func (s *pstamp) place(b bspec, x, y, z int) { s.placeState(b.state(s.p.rot, s.p.mir), x, y, z) }

// box is generateBox with an edge and a fill block.
func (s *pstamp) box(x0, y0, z0, x1, y1, z1 int, edge, fill bspec, skipAir bool) {
	e, f := edge.state(s.p.rot, s.p.mir), fill.state(s.p.rot, s.p.mir)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if skipAir && s.get(x, y, z) == Air {
					continue
				}
				if y != y0 && y != y1 && x != x0 && x != x1 && z != z0 && z != z1 {
					s.placeState(f, x, y, z)
				} else {
					s.placeState(e, x, y, z)
				}
			}
		}
	}
}

// fill is box with one block throughout, never skipping air.
func (s *pstamp) fill(x0, y0, z0, x1, y1, z1 int, b bspec) {
	s.box(x0, y0, z0, x1, y1, z1, b, b, false)
}

// air is fill with (cave) air.
func (s *pstamp) air(x0, y0, z0, x1, y1, z1 int) { s.fill(x0, y0, z0, x1, y1, z1, bs("air")) }

// boxSel is generateBox with a BlockSelector: sel picks each cell's block
// from its world position and whether it lies on the box's faces.
func (s *pstamp) boxSel(x0, y0, z0, x1, y1, z1 int, skipAir bool, sel func(wx, wy, wz int, edge bool) uint32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if skipAir && s.get(x, y, z) == Air {
					continue
				}
				edge := y == y0 || y == y1 || x == x0 || x == x1 || z == z0 || z == z1
				s.placeState(sel(s.p.wx(x, z), s.p.wy(y), s.p.wz(x, z), edge), x, y, z)
			}
		}
	}
}

// roll is the piece's random draw for one local cell (and purpose).
func (s *pstamp) roll(x, y, z int, salt uint64) float64 {
	return hash3(s.g.seed, s.p.wx(x, z), s.p.wy(y), s.p.wz(x, z), s.salt^salt)
}

// maybeBox is generateMaybeBox.
func (s *pstamp) maybeBox(prob float64, x0, y0, z0, x1, y1, z1 int, edge, fill bspec, skipAir, interior bool, salt uint64) {
	e, f := edge.state(s.p.rot, s.p.mir), fill.state(s.p.rot, s.p.mir)
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if s.roll(x, y, z, salt) > prob {
					continue
				}
				if skipAir && s.get(x, y, z) == Air {
					continue
				}
				if interior && !s.interior(x, y, z) {
					continue
				}
				if y != y0 && y != y1 && x != x0 && x != x1 && z != z0 && z != z1 {
					s.placeState(f, x, y, z)
				} else {
					s.placeState(e, x, y, z)
				}
			}
		}
	}
}

// maybeBlock is maybeGenerateBlock.
func (s *pstamp) maybeBlock(prob float64, x, y, z int, b bspec, salt uint64) {
	if s.roll(x, y, z, salt) < prob {
		s.place(b, x, y, z)
	}
}

// interior is isInterior: the cell above lies in the chunk and under the
// column's ocean-floor height.
func (s *pstamp) interior(x, y, z int) bool {
	wx, wy, wz := s.p.wx(x, z), s.p.wy(y+1), s.p.wz(x, z)
	if !s.cb.inside(wx, wy, wz) {
		return false
	}
	return wy < s.height(wx, wz)
}

// replaceableByStructures is StructurePiece.isReplaceableByStructures.
func replaceableByStructures(st uint32) bool {
	return st == Air || IsFluid(st) || inRange(st, glowLichenLo, glowLichenHi) ||
		inRange(st, seagrassLo, seagrassHi) || inRange(st, tallSeagrassLo, tallSeagrassHi)
}

var (
	seagrassLo, seagrassHi         = BlockRange("seagrass")
	tallSeagrassLo, tallSeagrassHi = BlockRange("tall_seagrass")
)

func inRange(st, lo, hi uint32) bool { return st >= lo && st <= hi }

// chest is createChest: a chest facing away from the one solid wall beside
// it (StructurePiece.reorient), unless one stands there already.
func (s *pstamp) chest(x, y, z int) [3]int {
	w := s.p.world(x, y, z)
	if !s.cb.inside(w[0], w[1], w[2]) || isChestState(s.getW(w[0], w[1], w[2])) {
		return w
	}
	s.setW(w[0], w[1], w[2], bsp("chest", "facing="+s.chestFacing(w)).state(0, 0))
	return w
}

var chestLo, chestHi = BlockRange("chest")

func isChestState(st uint32) bool { return st >= chestLo && st <= chestHi }

// chestFacing is StructurePiece.reorient over the chunk's cells.
func (s *pstamp) chestFacing(w [3]int) string {
	dirs := []struct {
		name   string
		dx, dz int
	}{{"north", 0, -1}, {"east", 1, 0}, {"south", 0, 1}, {"west", -1, 0}}
	opp := map[string]string{"north": "south", "south": "north", "east": "west", "west": "east"}
	cw := map[string]string{"north": "east", "east": "south", "south": "west", "west": "north"}
	solid := func(d string) bool {
		for _, e := range dirs {
			if e.name == d {
				return IsFullCube(s.getW(w[0]+e.dx, w[1], w[2]+e.dz))
			}
		}
		return false
	}
	wall := ""
	for _, d := range dirs {
		n := s.getW(w[0]+d.dx, w[1], w[2]+d.dz)
		if isChestState(n) {
			return "north"
		}
		if IsFullCube(n) {
			if wall != "" {
				wall = ""
				goto open
			}
			wall = d.name
		}
	}
	if wall != "" {
		return opp[wall]
	}
open:
	f := "north"
	if solid(f) {
		f = opp[f]
	}
	if solid(f) {
		f = cw[f]
	}
	if solid(f) {
		f = opp[f]
	}
	return f
}

// ---- the shape pass ---------------------------------------------------------

var (
	ironBarsLo, ironBarsHi = BlockRange("iron_bars")
)

// isShapeChecked: the connectors whose sides vanilla recomputes from their
// neighbours once the structure is down (SHAPE_CHECK_BLOCKS' fences and
// iron bars).
func isShapeChecked(st uint32) bool {
	return (st >= ironBarsLo && st <= ironBarsHi) || isWoodFence(st)
}

var woodFences = func() [][2]uint32 {
	var out [][2]uint32
	for _, n := range []string{"oak_fence", "dark_oak_fence", "spruce_fence", "birch_fence", "jungle_fence", "acacia_fence", "pale_oak_fence"} {
		lo, hi := BlockRange(n)
		out = append(out, [2]uint32{lo, hi})
	}
	return out
}()

func isWoodFence(st uint32) bool {
	for _, r := range woodFences {
		if st >= r[0] && st <= r[1] {
			return true
		}
	}
	return false
}

// reshape recomputes the four sides of every connector the pieces placed
// in this chunk from what now stands beside it (Block.updateFromNeighbour-
// Shapes in the chunk's post-processing): a side connects to a full block
// or to a connector of its own family. A side facing out of the chunk
// keeps the side the piece gave it.
func (s *pstamp) reshape() {
	sides := []struct {
		name   string
		dx, dz int
	}{{"north", 0, -1}, {"east", 1, 0}, {"south", 0, 1}, {"west", -1, 0}}
	for _, c := range *s.shaped {
		st := s.getW(c[0], c[1], c[2])
		if !isShapeChecked(st) {
			continue
		}
		info, ok := InfoForState(st)
		if !ok {
			continue
		}
		bars := st >= ironBarsLo && st <= ironBarsHi
		for _, d := range sides {
			nx, nz := c[0]+d.dx, c[2]+d.dz
			if !s.cb.inside(nx, c[1], nz) {
				continue
			}
			n := s.getW(nx, c[1], nz)
			conn := IsFullCube(n)
			if bars {
				conn = conn || (n >= ironBarsLo && n <= ironBarsHi)
			} else {
				conn = conn || isWoodFence(n)
			}
			v := "false"
			if conn {
				v = "true"
			}
			st = SetProperty(info, st, d.name, v)
		}
		s.setW(c[0], c[1], c[2], st)
	}
}
