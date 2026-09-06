package worldgen

import "sync"

// End cities — a port of vanilla's EndCityPieces: a house tower of real
// end_city templates grows towers, fat towers and bridges recursively (depth
// 8), each batch of children discarded if it collides with an earlier piece
// of another batch, and one bridge in a city may end in the ship. Sited on
// the outer End islands' highlands. The templates carry the treasure chests
// (end_city_treasure via their "Chest" markers), the shulker sentries and
// the ship's elytra frame; the server seeds the last two on arrival.

const (
	endCityCell     = 320 // one candidate per 320-block cell of the outer islands
	endCityOdds     = 0.5 // of qualifying cells
	endCityMaxDepth = 8
)

// EndCity is a placed city site (or the zero value).
type EndCity struct {
	X, Y, Z int
	Rot     int // vanilla Rotation: 0 none, 1 clockwise 90, 2 180, 3 counter-clockwise 90
	Exists  bool
}

// endSurface is the top of the outer-island terrain at a column (ok false in
// the void).
func (g *Generator) endSurface(x, z int) (int, bool) {
	top, _, ok := g.endOuterColumn(x, z)
	return top, ok
}

// EndCityIn returns the city owning (wx,wz)'s cell: the highlands only, on
// solid ground under the base and at every corner of its footprint (vanilla
// takes the lowest corner of the start and rejects one below y=60; the outer
// islands here are patchy plates, so the cell tries a handful of spots).
func (g *Generator) EndCityIn(wx, wz int) EndCity {
	ox, oz := cellOrigin(wx, endCityCell), cellOrigin(wz, endCityCell)
	if hash01(g.seed, ox, oz, 0xEC00) >= endCityOdds {
		return EndCity{}
	}
	for try := uint64(0); try < 24; try++ {
		x := ox + 24 + int(hash01(g.seed, ox, oz, 0xEC01+try*2)*float64(endCityCell-48))
		z := oz + 24 + int(hash01(g.seed, ox, oz, 0xEC02+try*2)*float64(endCityCell-48))
		if g.endBiome(x, z) != "minecraft:end_highlands" {
			continue
		}
		y := 1 << 30
		solid := true
		for _, d := range [5][2]int{{0, 0}, {-7, -7}, {7, -7}, {-7, 7}, {7, 7}} {
			top, ok := g.endSurface(x+d[0], z+d[1])
			if !ok {
				solid = false
				break
			}
			if top < y {
				y = top
			}
		}
		if !solid || y < 40 {
			continue
		}
		return EndCity{X: x, Y: y + 1, Z: z, Rot: int(hash01(g.seed, ox, oz, 0xEC03) * 4), Exists: true}
	}
	return EndCity{}
}

// endPiece is one template in vanilla's own terms: the template position is
// the rotation pivot (rotation about the min corner, so a rotated piece
// extends into negative x/z), and overwrite=false keeps the world's blocks
// where the template has air.
type endPiece struct {
	tmpl      *Template
	x, y, z   int
	rot       int
	overwrite bool
	genDepth  int
}

// placed converts a vanilla-positioned piece into a PlacedPiece with a
// non-negative footprint (our rotatePos convention).
func (p *endPiece) placed() PlacedPiece {
	sx, sz := p.tmpl.Size[0], p.tmpl.Size[2]
	ox, oz := p.x, p.z
	switch p.rot & 3 {
	case 1:
		ox -= sz - 1
	case 2:
		ox -= sx - 1
		oz -= sz - 1
	case 3:
		oz -= sx - 1
	}
	w, h, d := p.tmpl.rotatedSize(p.rot)
	return PlacedPiece{Tmpl: p.tmpl, OX: ox, OY: p.y, OZ: oz, Rot: p.rot, SkipAir: !p.overwrite,
		x1: ox + w, y1: p.y + h, z1: oz + d}
}

// rotateOffset is StructureTemplate.transform about a zero pivot.
func rotateOffset(x, z, rot int) (int, int) {
	switch rot & 3 {
	case 1:
		return -z, x
	case 2:
		return -x, -z
	case 3:
		return z, -x
	}
	return x, z
}

type endCityGen struct {
	rng         *jigsawRNG
	shipCreated bool
}

func (e *endCityGen) intn(n int) int { return e.rng.intn(n) }
func (e *endCityGen) boolean() bool  { return e.rng.next()&1 == 1 }

// newPiece is EndCityPieces.addPiece: the child sits at the parent's position
// plus the offset rotated by the parent's rotation.
func (e *endCityGen) newPiece(parent *endPiece, off [3]int, name string, rot int, overwrite bool) *endPiece {
	t := templates["end_city/"+name]
	if t == nil {
		return nil
	}
	dx, dz := rotateOffset(off[0], off[2], parent.rot)
	return &endPiece{tmpl: t, x: parent.x + dx, y: parent.y + off[1], z: parent.z + dz, rot: rot & 3, overwrite: overwrite}
}

func add(list *[]*endPiece, p *endPiece) *endPiece {
	if p != nil {
		*list = append(*list, p)
	}
	return p
}

// sectionGen is one of vanilla's four SectionGenerators.
type sectionGen func(e *endCityGen, depth int, parent *endPiece, off *[3]int, out *[]*endPiece) bool

var (
	towerBridges = [4]struct {
		rot int
		off [3]int
	}{{0, [3]int{1, -1, 0}}, {1, [3]int{6, -1, 1}}, {3, [3]int{0, -1, 5}}, {2, [3]int{5, -1, 6}}}
	fatTowerBridges = [4]struct {
		rot int
		off [3]int
	}{{0, [3]int{4, -1, 0}}, {1, [3]int{12, -1, 4}}, {3, [3]int{0, -1, 8}}, {2, [3]int{8, -1, 12}}}
)

func houseTowerGen(e *endCityGen, depth int, parent *endPiece, off *[3]int, out *[]*endPiece) bool {
	if depth > endCityMaxDepth || parent == nil {
		return false
	}
	rot := parent.rot
	var o [3]int
	if off != nil {
		o = *off
	}
	last := add(out, e.newPiece(parent, o, "base_floor", rot, true))
	if last == nil {
		return false
	}
	switch e.intn(3) {
	case 0:
		add(out, e.newPiece(last, [3]int{-1, 4, -1}, "base_roof", rot, true))
	case 1:
		last = add(out, e.newPiece(last, [3]int{-1, 0, -1}, "second_floor_2", rot, false))
		last = add(out, e.newPiece(last, [3]int{-1, 8, -1}, "second_roof", rot, false))
		e.recursiveChildren(towerGen, depth+1, last, nil, out)
	case 2:
		last = add(out, e.newPiece(last, [3]int{-1, 0, -1}, "second_floor_2", rot, false))
		last = add(out, e.newPiece(last, [3]int{-1, 4, -1}, "third_floor_2", rot, false))
		last = add(out, e.newPiece(last, [3]int{-1, 8, -1}, "third_roof", rot, true))
		e.recursiveChildren(towerGen, depth+1, last, nil, out)
	}
	return true
}

func towerGen(e *endCityGen, depth int, parent *endPiece, _ *[3]int, out *[]*endPiece) bool {
	if parent == nil {
		return false
	}
	rot := parent.rot
	last := add(out, e.newPiece(parent, [3]int{3 + e.intn(2), -3, 3 + e.intn(2)}, "tower_base", rot, true))
	last = add(out, e.newPiece(last, [3]int{0, 7, 0}, "tower_piece", rot, true))
	var bridgePiece *endPiece
	if e.intn(3) == 0 {
		bridgePiece = last
	}
	height := 1 + e.intn(3)
	for i := 0; i < height; i++ {
		last = add(out, e.newPiece(last, [3]int{0, 4, 0}, "tower_piece", rot, true))
		if i < height-1 && e.boolean() {
			bridgePiece = last
		}
	}
	if bridgePiece != nil {
		for _, b := range towerBridges {
			if e.boolean() {
				start := add(out, e.newPiece(bridgePiece, b.off, "bridge_end", rot+b.rot, true))
				e.recursiveChildren(towerBridgeGen, depth+1, start, nil, out)
			}
		}
		add(out, e.newPiece(last, [3]int{-1, 4, -1}, "tower_top", rot, true))
	} else {
		if depth != 7 {
			return e.recursiveChildren(fatTowerGen, depth+1, last, nil, out)
		}
		add(out, e.newPiece(last, [3]int{-1, 4, -1}, "tower_top", rot, true))
	}
	return true
}

func towerBridgeGen(e *endCityGen, depth int, parent *endPiece, _ *[3]int, out *[]*endPiece) bool {
	if parent == nil {
		return false
	}
	rot := parent.rot
	length := e.intn(4) + 1
	last := add(out, e.newPiece(parent, [3]int{0, 0, -4}, "bridge_piece", rot, true))
	if last == nil {
		return false
	}
	last.genDepth = -1
	nextY := 0
	for i := 0; i < length; i++ {
		if e.boolean() {
			last = add(out, e.newPiece(last, [3]int{0, nextY, -4}, "bridge_piece", rot, true))
			nextY = 0
		} else {
			if e.boolean() {
				last = add(out, e.newPiece(last, [3]int{0, nextY, -4}, "bridge_steep_stairs", rot, true))
			} else {
				last = add(out, e.newPiece(last, [3]int{0, nextY, -8}, "bridge_gentle_stairs", rot, true))
			}
			nextY = 4
		}
	}
	if !e.shipCreated && e.intn(10-depth) == 0 {
		add(out, e.newPiece(last, [3]int{-8 + e.intn(8), nextY, -70 + e.intn(10)}, "ship", rot, true))
		e.shipCreated = true
	} else if !e.recursiveChildren(houseTowerGen, depth+1, last, &[3]int{-3, nextY + 1, -11}, out) {
		return false
	}
	last = add(out, e.newPiece(last, [3]int{4, nextY, 0}, "bridge_end", rot+2, true))
	if last != nil {
		last.genDepth = -1
	}
	return true
}

func fatTowerGen(e *endCityGen, depth int, parent *endPiece, _ *[3]int, out *[]*endPiece) bool {
	if parent == nil {
		return false
	}
	rot := parent.rot
	last := add(out, e.newPiece(parent, [3]int{-3, 4, -3}, "fat_tower_base", rot, true))
	last = add(out, e.newPiece(last, [3]int{0, 4, 0}, "fat_tower_middle", rot, true))
	for i := 0; i < 2 && e.intn(3) != 0; i++ {
		last = add(out, e.newPiece(last, [3]int{0, 8, 0}, "fat_tower_middle", rot, true))
		for _, b := range fatTowerBridges {
			if e.boolean() {
				start := add(out, e.newPiece(last, b.off, "bridge_end", rot+b.rot, true))
				e.recursiveChildren(towerBridgeGen, depth+1, start, nil, out)
			}
		}
	}
	add(out, e.newPiece(last, [3]int{-2, 8, -2}, "fat_tower_top", rot, true))
	return true
}

// recursiveChildren generates one batch of children and keeps it only if no
// child collides with an earlier piece of another batch.
func (e *endCityGen) recursiveChildren(gen sectionGen, depth int, parent *endPiece, off *[3]int, pieces *[]*endPiece) bool {
	if depth > endCityMaxDepth || parent == nil {
		return false
	}
	var children []*endPiece
	if !gen(e, depth, parent, off, &children) {
		return false
	}
	tag := int(int32(e.rng.next()))
	for _, c := range children {
		c.genDepth = tag
		if hit := findCollision(*pieces, c); hit != nil && hit.genDepth != parent.genDepth {
			return false
		}
	}
	*pieces = append(*pieces, children...)
	return true
}

func findCollision(pieces []*endPiece, c *endPiece) *endPiece {
	cb := c.placed()
	ax0, ay0, az0, ax1, ay1, az1 := cb.bbox()
	for _, p := range pieces {
		pb := p.placed()
		bx0, by0, bz0, bx1, by1, bz1 := pb.bbox()
		if overlaps(ax0, ay0, az0, ax1, ay1, az1, bx0, by0, bz0, bx1, by1, bz1) {
			return p
		}
	}
	return nil
}

type endCityKey struct {
	seed int64
	x, z int
}

var (
	endCityCache = map[endCityKey][]PlacedPiece{}
	endCityMu    sync.Mutex
)

// AssembleEndCity is EndCityPieces.startHouseTower for a site (cached).
func (g *Generator) AssembleEndCity(c EndCity) []PlacedPiece {
	k := endCityKey{g.seed, c.X, c.Z}
	endCityMu.Lock()
	p, ok := endCityCache[k]
	endCityMu.Unlock()
	if ok {
		return p
	}
	e := &endCityGen{rng: newJigsawRNG(g.seed, c.X, c.Z)}
	var pieces []*endPiece
	base := templates["end_city/base_floor"]
	if base != nil {
		root := &endPiece{tmpl: base, x: c.X, y: c.Y, z: c.Z, rot: c.Rot, overwrite: true}
		last := add(&pieces, root)
		last = add(&pieces, e.newPiece(last, [3]int{-1, 0, -1}, "second_floor_1", c.Rot, false))
		last = add(&pieces, e.newPiece(last, [3]int{-1, 4, -1}, "third_floor_1", c.Rot, false))
		last = add(&pieces, e.newPiece(last, [3]int{-1, 8, -1}, "third_roof", c.Rot, true))
		e.recursiveChildren(towerGen, 1, last, nil, &pieces)
	}
	p = make([]PlacedPiece, len(pieces))
	for i, ep := range pieces {
		p[i] = ep.placed()
	}
	endCityMu.Lock()
	endCityCache[k] = p
	endCityMu.Unlock()
	return p
}

// stampEndCities stamps the city pieces overlapping this End chunk.
func (g *Generator) stampEndCities(ch *Chunk, cx, cz int32) {
	for _, c := range g.EndCitiesNear(int(cx)*16+8, int(cz)*16+8) {
		g.StampPieces(ch, cx, cz, g.AssembleEndCity(c))
	}
}

// EndCitiesNear returns the cities of the cell around (wx,wz) and its eight
// neighbours — bridges and the ship reach well past a cell's border.
func (g *Generator) EndCitiesNear(wx, wz int) []EndCity {
	var out []EndCity
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if c := g.EndCityIn(wx+dx*endCityCell, wz+dz*endCityCell); c.Exists {
				out = append(out, c)
			}
		}
	}
	return out
}

// EndCityChest is a city treasure chest and its table.
type EndCityChest struct {
	X, Y, Z int
	Table   string
}

// EndCityChests returns the city's loot chests.
func (g *Generator) EndCityChests(c EndCity) []EndCityChest {
	var out []EndCityChest
	for _, pc := range g.AssembleEndCity(c) {
		for i, ch := range pc.Tmpl.Chests {
			rx, ry, rz := pc.Tmpl.rotatePos(ch[0], ch[1], ch[2], pc.Rot)
			table := "chests/end_city_treasure"
			if i < len(pc.Tmpl.ChestLoot) && pc.Tmpl.ChestLoot[i] != "" {
				table = pc.Tmpl.ChestLoot[i]
			}
			out = append(out, EndCityChest{pc.OX + rx, pc.OY + ry, pc.OZ + rz, table})
		}
	}
	return out
}

// EndCityMob is a marker the server seeds: a shulker sentry, or the elytra
// item frame in the ship (Type "elytra", facing the rotated south).
type EndCityMob struct {
	X, Y, Z int
	Type    string
	Rot     int
}

// EndCityMobs returns the city's sentry and elytra markers.
func (g *Generator) EndCityMobs(c EndCity) []EndCityMob {
	var out []EndCityMob
	for _, pc := range g.AssembleEndCity(c) {
		for _, m := range pc.Tmpl.Mobs {
			rx, ry, rz := pc.Tmpl.rotatePos(m.Pos[0], m.Pos[1], m.Pos[2], pc.Rot)
			out = append(out, EndCityMob{pc.OX + rx, pc.OY + ry, pc.OZ + rz, m.Type, pc.Rot})
		}
	}
	return out
}
