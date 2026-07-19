package worldgen

// Ocean monument — a faithful port of vanilla OceanMonumentPieces.MonumentBuilding
// (the 58×58×23 prismarine temple). The shell — the two wings, entrance archways,
// entrance wall, roof, and the lower/middle/upper interior walls — is transcribed
// block-for-block from the decompiled coordinates, as is the 7×7 foundation grid
// (rooted to the seafloor) and the surrounding water moat. The interior room
// detail-maze (RoomDefinition graph) is not reproduced; instead the gold-block
// treasure is placed hidden in the core, and the elder guardians + patrolling
// guardians are seeded by the server when a player approaches (guardian.go).
//
// Orientation is fixed (north): the monument is four-fold near-symmetric, so the
// only visible effect is which way the entrance faces. Local coordinates 0..58
// map to world via (ox+lx, oy+ly, oz+lz); setSectionBlock clips to the chunk.

const (
	monumentCell = 448
	monumentOdds = 0.5
	monumentHalf = 29 // the 58-wide building is centred on the site
)

// Monument is a placed temple (or the zero value). X,Z is the centre column; a
// player near it triggers guardian seeding.
type Monument struct {
	X, Y, Z int // centre column; Y is the seafloor it rises from
	Exists  bool
}

// MonumentIn returns the monument owning (wx,wz)'s cell, if the site is deep
// ocean floor.
func (g *Generator) MonumentIn(wx, wz int) Monument {
	ox, oz := cellOrigin(wx, monumentCell), cellOrigin(wz, monumentCell)
	if hash01(g.seed, ox, oz, 0x0C00) >= monumentOdds {
		return Monument{}
	}
	x := ox + 96 + int(hash01(g.seed, ox, oz, 0x0C01)*float64(monumentCell-192))
	z := oz + 96 + int(hash01(g.seed, ox, oz, 0x0C02)*float64(monumentCell-192))
	floor := g.Height(x, z)
	if floor >= SeaLevel-12 || floor < 20 { // needs deep water above it
		return Monument{}
	}
	return Monument{X: x, Y: floor, Z: z, Exists: true}
}

// monStamp carries the per-chunk stamping context. Local (0..58) → world.
type monStamp struct {
	ch                 *Chunk
	baseX, baseZ       int // chunk min corner
	ox, oy, oz         int // monument min corner (world)
	gray, light, black uint32
	lamp, gold, deco   uint32
}

// place sets a block at local (lx,ly,lz); setSectionBlock clips to the chunk.
func (s *monStamp) place(lx, ly, lz int, b uint32) {
	wx, wy, wz := s.ox+lx, s.oy+ly, s.oz+lz
	setSectionBlock(s.ch, wx-s.baseX, wy, wz-s.baseZ, b, true)
}

// getW reads a world column block if it lies in this chunk (else Air).
func (s *monStamp) getW(wx, wy, wz int) uint32 {
	lx, lz := wx-s.baseX, wz-s.baseZ
	if lx < 0 || lx >= 16 || lz < 0 || lz >= 16 || wy < MinY || wy >= MinY+len(s.ch.Sections)*16 {
		return Air
	}
	sec := (wy - MinY) / 16
	return s.ch.Sections[sec][((wy-MinY)%16*16+lz)*16+lx]
}

// genBox fills a local box uniformly (vanilla generateBox with equal edge/inside).
func (s *monStamp) genBox(x0, y0, z0, x1, y1, z1 int, b uint32) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				s.place(x, y, z, b)
			}
		}
	}
}

// waterBox clears a local box to water (air above sea level) — vanilla
// generateWaterBox, which carves the monument's interior + moat out of terrain.
func (s *monStamp) waterBox(x0, y0, z0, x1, y1, z1 int) {
	for y := y0; y <= y1; y++ {
		b := Water
		if s.oy+y >= SeaLevel {
			b = Air
		}
		for x := x0; x <= x1; x++ {
			for z := z0; z <= z1; z++ {
				s.place(x, y, z, b)
			}
		}
	}
}

// fillColumnDown roots a foundation pillar: from local y downward, replace
// water/air with `b` until solid ground (or the world floor).
func (s *monStamp) fillColumnDown(b uint32, x, y, z int) {
	wx, wz := s.ox+x, s.oz+z
	for wy := s.oy + y; wy > MinY; wy-- {
		cur := s.getW(wx, wy, wz)
		if cur != Air && cur != Water {
			break
		}
		setSectionBlock(s.ch, wx-s.baseX, wy, wz-s.baseZ, b, true)
	}
}

// stampMonument stamps the temple portion overlapping this chunk.
func (g *Generator) stampMonument(ch *Chunk, cx, cz int32) {
	m := g.MonumentIn(int(cx)*16+8, int(cz)*16+8)
	if !m.Exists {
		return
	}
	s := &monStamp{
		ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16,
		ox: m.X - monumentHalf, oy: m.Y, oz: m.Z - monumentHalf,
		gray:  blockBase("prismarine"),
		light: blockBase("prismarine_bricks"),
		black: blockBase("dark_prismarine"),
		lamp:  blockBase("sea_lantern"),
		gold:  blockBase("gold_block"),
	}
	s.deco = s.light // DOT_DECO_DATA == BASE_LIGHT

	// Shell (MonumentBuilding.postProcess order).
	s.wing(false, 0)
	s.wing(true, 33)
	s.entranceArchs()
	s.entranceWall()
	s.roofPiece()
	s.lowerWall()
	s.middleWall()
	s.upperWall()

	// Foundation grid: 4×4 prismarine-brick pads on a 7×7 grid (skipping the
	// entrance gap), each rooted down to the seafloor.
	for n := 0; n < 7; n++ {
		for n3 := 0; n3 < 7; {
			if n3 == 0 && n == 3 {
				n3 = 6
			}
			n4, n5 := n*9, n3*9
			for i := 0; i < 4; i++ {
				for j := 0; j < 4; j++ {
					s.place(n4+i, 0, n5+j, s.light)
					s.fillColumnDown(s.light, n4+i, -1, n5+j)
				}
			}
			if n == 0 || n == 6 {
				n3++
				continue
			}
			n3 += 6
		}
	}

	// Surrounding water moat (five stepped rings).
	for n := 0; n < 5; n++ {
		s.waterBox(-1-n, 0+n*2, -1-n, -1-n, 23, 58+n)
		s.waterBox(58+n, 0+n*2, -1-n, 58+n, 23, 58+n)
		s.waterBox(0-n, 0+n*2, -1-n, 57+n, 23, -1-n)
		s.waterBox(0-n, 0+n*2, 58+n, 57+n, 23, 58+n)
	}

	// The gold-block treasure (vanilla hides a 2×2×2 in the core room). A dark-
	// prismarine pillar rooted on the central floor (so removeFloatingFragments
	// keeps it) hides the gold in its heart.
	s.genBox(27, 1, 27, 30, 6, 30, s.black)
	s.genBox(28, 4, 28, 29, 5, 29, s.gold)
}

// ---- shell pieces (block-for-block from OceanMonumentPieces) -----------------

func (s *monStamp) wing(mirror bool, n int) {
	s.genBox(n+0, 0, 0, n+24, 0, 20, s.gray)
	s.waterBox(n+0, 1, 0, n+24, 10, 20)
	for n4 := 0; n4 < 4; n4++ {
		s.genBox(n+n4, n4+1, n4, n+n4, n4+1, 20, s.light)
		s.genBox(n+n4+7, n4+5, n4+7, n+n4+7, n4+5, 20, s.light)
		s.genBox(n+17-n4, n4+5, n4+7, n+17-n4, n4+5, 20, s.light)
		s.genBox(n+24-n4, n4+1, n4, n+24-n4, n4+1, 20, s.light)
		s.genBox(n+n4+1, n4+1, n4, n+23-n4, n4+1, n4, s.light)
		s.genBox(n+n4+8, n4+5, n4+7, n+16-n4, n4+5, n4+7, s.light)
	}
	s.genBox(n+4, 4, 4, n+6, 4, 20, s.gray)
	s.genBox(n+7, 4, 4, n+17, 4, 6, s.gray)
	s.genBox(n+18, 4, 4, n+20, 4, 20, s.gray)
	s.genBox(n+11, 8, 11, n+13, 8, 20, s.gray)
	s.place(n+12, 9, 12, s.deco)
	s.place(n+12, 9, 15, s.deco)
	s.place(n+12, 9, 18, s.deco)
	n4 := n + 5
	n5 := n + 19
	if mirror {
		n4, n5 = n+19, n+5
	}
	for n3 := 20; n3 >= 5; n3 -= 3 {
		s.place(n4, 5, n3, s.deco)
	}
	for n3 := 19; n3 >= 7; n3 -= 3 {
		s.place(n5, 5, n3, s.deco)
	}
	for n3 := 0; n3 < 4; n3++ {
		n6 := n + 17 - n3*3
		if mirror {
			n6 = n + 24 - (17 - n3*3)
		}
		s.place(n6, 5, 5, s.deco)
	}
	s.place(n5, 5, 5, s.deco)
	s.genBox(n+11, 1, 12, n+13, 7, 12, s.gray)
	s.genBox(n+12, 1, 11, n+12, 7, 13, s.gray)
}

func (s *monStamp) entranceArchs() {
	s.waterBox(25, 0, 0, 32, 8, 20)
	for i := 0; i < 4; i++ {
		s.genBox(24, 2, 5+i*4, 24, 4, 5+i*4, s.light)
		s.genBox(22, 4, 5+i*4, 23, 4, 5+i*4, s.light)
		s.place(25, 5, 5+i*4, s.light)
		s.place(26, 6, 5+i*4, s.light)
		s.place(26, 5, 5+i*4, s.lamp)
		s.genBox(33, 2, 5+i*4, 33, 4, 5+i*4, s.light)
		s.genBox(34, 4, 5+i*4, 35, 4, 5+i*4, s.light)
		s.place(32, 5, 5+i*4, s.light)
		s.place(31, 6, 5+i*4, s.light)
		s.place(31, 5, 5+i*4, s.lamp)
		s.genBox(27, 6, 5+i*4, 30, 6, 5+i*4, s.gray)
	}
}

func (s *monStamp) entranceWall() {
	s.genBox(15, 0, 21, 42, 0, 21, s.gray)
	s.waterBox(26, 1, 21, 31, 3, 21)
	s.genBox(21, 12, 21, 36, 12, 21, s.gray)
	s.genBox(17, 11, 21, 40, 11, 21, s.gray)
	s.genBox(16, 10, 21, 41, 10, 21, s.gray)
	s.genBox(15, 7, 21, 42, 9, 21, s.gray)
	s.genBox(16, 6, 21, 41, 6, 21, s.gray)
	s.genBox(17, 5, 21, 40, 5, 21, s.gray)
	s.genBox(21, 4, 21, 36, 4, 21, s.gray)
	s.genBox(22, 3, 21, 26, 3, 21, s.gray)
	s.genBox(31, 3, 21, 35, 3, 21, s.gray)
	s.genBox(23, 2, 21, 25, 2, 21, s.gray)
	s.genBox(32, 2, 21, 34, 2, 21, s.gray)
	s.genBox(28, 4, 20, 29, 4, 21, s.light)
	s.place(27, 3, 21, s.light)
	s.place(30, 3, 21, s.light)
	s.place(26, 2, 21, s.light)
	s.place(31, 2, 21, s.light)
	s.place(25, 1, 21, s.light)
	s.place(32, 1, 21, s.light)
	for n := 0; n < 7; n++ {
		s.place(28-n, 6+n, 21, s.black)
		s.place(29+n, 6+n, 21, s.black)
	}
	for n := 0; n < 4; n++ {
		s.place(28-n, 9+n, 21, s.black)
		s.place(29+n, 9+n, 21, s.black)
	}
	s.place(28, 12, 21, s.black)
	s.place(29, 12, 21, s.black)
	for n := 0; n < 3; n++ {
		s.place(22-n*2, 8, 21, s.black)
		s.place(22-n*2, 9, 21, s.black)
		s.place(35+n*2, 8, 21, s.black)
		s.place(35+n*2, 9, 21, s.black)
	}
	s.waterBox(15, 13, 21, 42, 15, 21)
	s.waterBox(15, 1, 21, 15, 6, 21)
	s.waterBox(16, 1, 21, 16, 5, 21)
	s.waterBox(17, 1, 21, 20, 4, 21)
	s.waterBox(21, 1, 21, 21, 3, 21)
	s.waterBox(22, 1, 21, 22, 2, 21)
	s.waterBox(23, 1, 21, 24, 1, 21)
	s.waterBox(42, 1, 21, 42, 6, 21)
	s.waterBox(41, 1, 21, 41, 5, 21)
	s.waterBox(37, 1, 21, 40, 4, 21)
	s.waterBox(36, 1, 21, 36, 3, 21)
	s.waterBox(33, 1, 21, 34, 1, 21)
	s.waterBox(35, 1, 21, 35, 2, 21)
}

func (s *monStamp) roofPiece() {
	s.genBox(21, 0, 22, 36, 0, 36, s.gray)
	s.waterBox(21, 1, 22, 36, 23, 36)
	for i := 0; i < 4; i++ {
		s.genBox(21+i, 13+i, 21+i, 36-i, 13+i, 21+i, s.light)
		s.genBox(21+i, 13+i, 36-i, 36-i, 13+i, 36-i, s.light)
		s.genBox(21+i, 13+i, 22+i, 21+i, 13+i, 35-i, s.light)
		s.genBox(36-i, 13+i, 22+i, 36-i, 13+i, 35-i, s.light)
	}
	s.genBox(25, 16, 25, 32, 16, 32, s.gray)
	s.genBox(25, 17, 25, 25, 19, 25, s.light)
	s.genBox(32, 17, 25, 32, 19, 25, s.light)
	s.genBox(25, 17, 32, 25, 19, 32, s.light)
	s.genBox(32, 17, 32, 32, 19, 32, s.light)
	s.place(26, 20, 26, s.light)
	s.place(27, 21, 27, s.light)
	s.place(27, 20, 27, s.lamp)
	s.place(26, 20, 31, s.light)
	s.place(27, 21, 30, s.light)
	s.place(27, 20, 30, s.lamp)
	s.place(31, 20, 31, s.light)
	s.place(30, 21, 30, s.light)
	s.place(30, 20, 30, s.lamp)
	s.place(31, 20, 26, s.light)
	s.place(30, 21, 27, s.light)
	s.place(30, 20, 27, s.lamp)
	s.genBox(28, 21, 27, 29, 21, 27, s.gray)
	s.genBox(27, 21, 28, 27, 21, 29, s.gray)
	s.genBox(28, 21, 30, 29, 21, 30, s.gray)
	s.genBox(30, 21, 28, 30, 21, 29, s.gray)
}

func (s *monStamp) lowerWall() {
	s.genBox(0, 0, 21, 6, 0, 57, s.gray)
	s.waterBox(0, 1, 21, 6, 7, 57)
	s.genBox(4, 4, 21, 6, 4, 53, s.gray)
	for n := 0; n < 4; n++ {
		s.genBox(n, n+1, 21, n, n+1, 57-n, s.light)
	}
	for n := 23; n < 53; n += 3 {
		s.place(5, 5, n, s.deco)
	}
	s.place(5, 5, 52, s.deco)
	s.genBox(4, 1, 52, 6, 3, 52, s.gray)
	s.genBox(5, 1, 51, 5, 3, 53, s.gray)

	s.genBox(51, 0, 21, 57, 0, 57, s.gray)
	s.waterBox(51, 1, 21, 57, 7, 57)
	s.genBox(51, 4, 21, 53, 4, 53, s.gray)
	for n := 0; n < 4; n++ {
		s.genBox(57-n, n+1, 21, 57-n, n+1, 57-n, s.light)
	}
	for n := 23; n < 53; n += 3 {
		s.place(52, 5, n, s.deco)
	}
	s.place(52, 5, 52, s.deco)
	s.genBox(51, 1, 52, 53, 3, 52, s.gray)
	s.genBox(52, 1, 51, 52, 3, 53, s.gray)

	s.genBox(7, 0, 51, 50, 0, 57, s.gray)
	s.waterBox(7, 1, 51, 50, 10, 57)
	for n := 0; n < 4; n++ {
		s.genBox(n+1, n+1, 57-n, 56-n, n+1, 57-n, s.light)
	}
}

func (s *monStamp) middleWall() {
	s.genBox(7, 0, 21, 13, 0, 50, s.gray)
	s.waterBox(7, 1, 21, 13, 10, 50)
	s.genBox(11, 8, 21, 13, 8, 53, s.gray)
	for n := 0; n < 4; n++ {
		s.genBox(n+7, n+5, 21, n+7, n+5, 54, s.light)
	}
	for n := 21; n <= 45; n += 3 {
		s.place(12, 9, n, s.deco)
	}

	s.genBox(44, 0, 21, 50, 0, 50, s.gray)
	s.waterBox(44, 1, 21, 50, 10, 50)
	s.genBox(44, 8, 21, 46, 8, 53, s.gray)
	for n := 0; n < 4; n++ {
		s.genBox(50-n, n+5, 21, 50-n, n+5, 54, s.light)
	}
	for n := 21; n <= 45; n += 3 {
		s.place(45, 9, n, s.deco)
	}

	s.genBox(14, 0, 44, 43, 0, 50, s.gray)
	s.waterBox(14, 1, 44, 43, 10, 50)
	for n := 12; n <= 45; n += 3 {
		s.place(n, 9, 45, s.deco)
		s.place(n, 9, 52, s.deco)
		if n != 12 && n != 18 && n != 24 && n != 33 && n != 39 && n != 45 {
			continue
		}
		s.place(n, 9, 47, s.deco)
		s.place(n, 9, 50, s.deco)
		s.place(n, 10, 45, s.deco)
		s.place(n, 10, 46, s.deco)
		s.place(n, 10, 51, s.deco)
		s.place(n, 10, 52, s.deco)
		s.place(n, 11, 47, s.deco)
		s.place(n, 11, 50, s.deco)
		s.place(n, 12, 48, s.deco)
		s.place(n, 12, 49, s.deco)
	}
	for n := 0; n < 3; n++ {
		s.genBox(8+n, 5+n, 54, 49-n, 5+n, 54, s.gray)
	}
	s.genBox(11, 8, 54, 46, 8, 54, s.light)
	s.genBox(14, 8, 44, 43, 8, 53, s.gray)
}

func (s *monStamp) upperWall() {
	s.genBox(14, 0, 21, 20, 0, 43, s.gray)
	s.waterBox(14, 1, 22, 20, 14, 43)
	s.genBox(18, 12, 22, 20, 12, 39, s.gray)
	s.genBox(18, 12, 21, 20, 12, 21, s.light)
	for n := 0; n < 4; n++ {
		s.genBox(n+14, n+9, 21, n+14, n+9, 43-n, s.light)
	}
	for n := 23; n <= 39; n += 3 {
		s.place(19, 13, n, s.deco)
	}

	s.genBox(37, 0, 21, 43, 0, 43, s.gray)
	s.waterBox(37, 1, 22, 43, 14, 43)
	s.genBox(37, 12, 22, 39, 12, 39, s.gray)
	s.genBox(37, 12, 21, 39, 12, 21, s.light)
	for n := 0; n < 4; n++ {
		s.genBox(43-n, n+9, 21, 43-n, n+9, 43-n, s.light)
	}
	for n := 23; n <= 39; n += 3 {
		s.place(38, 13, n, s.deco)
	}

	s.genBox(21, 0, 37, 36, 0, 43, s.gray)
	s.waterBox(21, 1, 37, 36, 14, 43)
	s.genBox(21, 12, 37, 36, 12, 39, s.gray)
	for n := 0; n < 4; n++ {
		s.genBox(15+n, n+9, 43-n, 42-n, n+9, 43-n, s.light)
	}
	for n := 21; n <= 36; n += 3 {
		s.place(n, 13, 38, s.deco)
	}
}
