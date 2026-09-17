package worldgen

// A scattered piece — ScatteredFeaturePiece: a code-built structure of one
// piece placed at the average ground height of its footprint, turned to one
// of four directions. The desert temple has its own older copy of this
// (templeStamp); the jungle temple and the swamp hut share this one.

type scatteredPiece struct {
	X, Y, Z int // the piece's min corner; Y is local y=0
	W, D    int // width (x) and depth (z) before rotation
	Dir     int // 0 north, 1 south, 2 west, 3 east
	Exists  bool
}

// world maps a piece-local cell to the world (StructurePiece.getWorldX/Y/Z).
func (p scatteredPiece) world(x, y, z int) (int, int, int) {
	maxX, maxZ := p.X+p.W-1, p.Z+p.D-1
	switch p.Dir {
	case 0: // north
		return p.X + x, p.Y + y, maxZ - z
	case 1: // south
		return p.X + x, p.Y + y, p.Z + z
	case 2: // west
		return maxX - z, p.Y + y, p.Z + x
	}
	return p.X + z, p.Y + y, p.Z + x // east
}

// overlaps reports whether the piece's footprint touches chunk (baseX, baseZ).
func (p scatteredPiece) overlaps(baseX, baseZ int) bool {
	sx, sz := p.W, p.D
	if p.Dir >= 2 {
		sx, sz = p.D, p.W
	}
	return p.X < baseX+16 && p.X+sx > baseX && p.Z < baseZ+16 && p.Z+sz > baseZ
}

// averageGround is updateAverageGroundHeight: the mean MOTION_BLOCKING_NO_
// LEAVES height over the footprint (water counts as blocking).
func (g *Generator) averageGround(p scatteredPiece) int {
	total, n := 0, 0
	sx, sz := p.W, p.D
	if p.Dir >= 2 {
		sx, sz = p.D, p.W
	}
	for dx := 0; dx < sx; dx++ {
		for dz := 0; dz < sz; dz++ {
			h := g.Height(p.X+dx, p.Z+dz)
			if h < SeaLevel {
				h = SeaLevel
			}
			total += h
			n++
		}
	}
	return total / n
}

type pieceStamp struct {
	p            scatteredPiece
	ch           *Chunk
	baseX, baseZ int
}

func (s *pieceStamp) place(state uint32, x, y, z int) {
	wx, wy, wz := s.p.world(x, y, z)
	if wx-s.baseX < 0 || wx-s.baseX >= 16 || wz-s.baseZ < 0 || wz-s.baseZ >= 16 {
		return
	}
	setSectionBlock(s.ch, wx-s.baseX, wy, wz-s.baseZ, s.orient(state), true)
}

func (s *pieceStamp) at(x, y, z int) uint32 {
	wx, wy, wz := s.p.world(x, y, z)
	return sectionBlockAt(s.ch, wx-s.baseX, wy, wz-s.baseZ)
}

// box is generateBox: the shell in outer, the inside in inner.
func (s *pieceStamp) box(x0, y0, z0, x1, y1, z1 int, outer, inner uint32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				if y == y0 || y == y1 || x == x0 || x == x1 || z == z0 || z == z1 {
					s.place(outer, x, y, z)
				} else {
					s.place(inner, x, y, z)
				}
			}
		}
	}
}

// boxPick is generateBox with a BlockSelector: every cell drawn by pick.
func (s *pieceStamp) boxPick(x0, y0, z0, x1, y1, z1 int, pick func() uint32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				s.place(pick(), x, y, z)
			}
		}
	}
}

func (s *pieceStamp) airBox(x0, y0, z0, x1, y1, z1 int) { s.box(x0, y0, z0, x1, y1, z1, Air, Air) }

// fillDown is fillColumnDown: the state from the cell down through air and
// water until ground.
func (s *pieceStamp) fillDown(state uint32, x, y, z int) {
	wx, wy, wz := s.p.world(x, y, z)
	if wx-s.baseX < 0 || wx-s.baseX >= 16 || wz-s.baseZ < 0 || wz-s.baseZ >= 16 {
		return
	}
	for ; wy > MinY+1; wy-- {
		b := sectionBlockAt(s.ch, wx-s.baseX, wy, wz-s.baseZ)
		if b != Air && !IsWater(b) {
			return
		}
		setSectionBlock(s.ch, wx-s.baseX, wy, wz-s.baseZ, s.orient(state), true)
	}
}

// orient applies the piece's mirror + rotation to a directional state
// (StructurePiece.placeBlock): south mirrors, west mirrors then turns
// clockwise, east turns clockwise — on a facing property, and on the four
// side properties of wires, tripwires, vines and fences.
func (s *pieceStamp) orient(state uint32) uint32 {
	info, ok := InfoForState(state)
	if !ok {
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
	turn := func(f string) string {
		switch s.p.Dir {
		case 1:
			return mirror(f)
		case 2:
			return cw(mirror(f))
		case 3:
			return cw(f)
		}
		return f
	}
	if f := GetProperty(info, state, "facing"); f != "" {
		state = SetProperty(info, state, "facing", turn(f))
	}
	sides := []string{"north", "east", "south", "west"}
	if info.HasProperty("north") && info.HasProperty("east") && info.HasProperty("south") && info.HasProperty("west") {
		vals := map[string]string{}
		for _, sd := range sides {
			vals[sd] = GetProperty(info, state, sd)
		}
		for _, sd := range sides {
			state = SetProperty(info, state, turn(sd), vals[sd])
		}
	}
	return state
}

// withProps builds a state from a block's default with properties set.
func withProps(name string, kv ...string) uint32 {
	st := blockID(name)
	info, ok := InfoForState(st)
	if !ok {
		return st
	}
	for i := 0; i+1 < len(kv); i += 2 {
		st = SetProperty(info, st, kv[i], kv[i+1])
	}
	return st
}
