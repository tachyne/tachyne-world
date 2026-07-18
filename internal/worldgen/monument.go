package worldgen

// Ocean monument — a prismarine temple on the deep-ocean floor: a hollow
// prismarine-brick hall lit by sea lanterns, dark-prismarine accents, and a
// central core hiding the gold-block treasure. A faithful-feel stand-in for the
// vanilla jigsaw monument (no room templates); the elder guardians that curse
// intruders and the guardians that patrol it are seeded by the server when a
// player approaches (see guardian.go).

const (
	monumentCell   = 448
	monumentOdds   = 0.5
	monumentRadius = 9  // 19×19 footprint
	monumentTall   = 14 // from seafloor up toward the surface
)

// Monument is a placed temple (or the zero value). Core is the treasure-core
// centre; a player near it triggers guardian seeding.
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

// stampMonument stamps the temple portion overlapping this chunk.
func (g *Generator) stampMonument(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	m := g.MonumentIn(baseX+8, baseZ+8)
	if !m.Exists {
		return
	}
	bricks := blockBase("prismarine_bricks")
	dark := blockBase("dark_prismarine")
	lantern := blockBase("sea_lantern")
	gold := blockBase("gold_block")
	R := monumentRadius
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := baseX+lx, baseZ+lz
			dx, dz := wx-m.X, wz-m.Z
			if dx < -R || dx > R || dz < -R || dz > R {
				continue
			}
			shellXZ := dx == -R || dx == R || dz == -R || dz == R
			for dy := 0; dy <= monumentTall; dy++ {
				wy := m.Y + dy
				switch {
				case dy == 0 || dy == monumentTall || shellXZ: // floor, roof, walls
					b := bricks
					// Sea lanterns studded through the shell for the glow.
					if (dx+dz+dy)%5 == 0 {
						b = lantern
					} else if (dx*dz+dy)%7 == 0 {
						b = dark
					}
					setSectionBlock(ch, lx, wy, lz, b, true)
				default:
					setSectionBlock(ch, lx, wy, lz, Air, true) // hollow hall
				}
			}
			// Central treasure core: a dark-prismarine pillar rooted on the floor
			// (so it isn't culled as a floating fragment) with gold at its top.
			if dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1 {
				top := m.Y + monumentTall/2 + 1
				for wy := m.Y + 1; wy <= top; wy++ {
					b := dark
					if dx == 0 && dz == 0 && wy >= top-1 {
						b = gold
					}
					setSectionBlock(ch, lx, wy, lz, b, true)
				}
			}
		}
	}
}
