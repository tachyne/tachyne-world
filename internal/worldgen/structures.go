package worldgen

// Structures: surface lakes and buried dungeons, and the entry point that
// stamps every structure (mineshafts live in mineshaft.go, strongholds in
// stronghold.go). Same philosophy as features.go — every structure is a pure
// function of (seed, world coordinates), and each chunk stamps only its own
// intersection, so neighbours agree with no shared state.

// Grid cell sizes (blocks) and per-cell odds.
const (
	lakeCell    = 64
	lakeOdds    = 0.10
	dungeonCell = 48
	dungeonOdds = 0.28
)

// cellOrigin maps a world coordinate to its grid cell corner.
func cellOrigin(w, cell int) int {
	if w < 0 {
		return ((w - cell + 1) / cell) * cell
	}
	return (w / cell) * cell
}

// cellHash gives the deterministic roll for a grid cell.
func (g *Generator) cellHash(cx, cz, cell int, salt uint64) float64 {
	return hash01(g.seed, cx/cell, cz/cell, salt)
}

// ---- lakes -----------------------------------------------------------------

type lake struct {
	x, z   int // centre column
	r      int // horizontal radius 4..8
	depth  int
	lava   bool
	exists bool
}

// lakeIn rolls the lake for the cell containing (wx,wz).
func (g *Generator) lakeIn(wx, wz int) lake {
	ox, oz := cellOrigin(wx, lakeCell), cellOrigin(wz, lakeCell)
	if hash01(g.seed, ox, oz, 0xA1CE) >= lakeOdds {
		return lake{}
	}
	cx := ox + 8 + int(hash01(g.seed, ox, oz, 0xA2)*float64(lakeCell-16))
	cz := oz + 8 + int(hash01(g.seed, ox, oz, 0xA3)*float64(lakeCell-16))
	return lake{
		x: cx, z: cz,
		r:      4 + int(hash01(g.seed, ox, oz, 0xA4)*5),
		depth:  3 + int(hash01(g.seed, ox, oz, 0xA5)*3),
		lava:   hash01(g.seed, ox, oz, 0xA6) < 0.2,
		exists: true,
	}
}

// stampLakes carves lake bowls that intersect this chunk. The bowl replaces
// terrain below the rim with fluid and opens the columns above to air.
func (g *Generator) stampLakes(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	seen := map[[2]int]bool{}
	for _, off := range [][2]int{{0, 0}, {lakeCell, 0}, {-lakeCell, 0}, {0, lakeCell}, {0, -lakeCell},
		{lakeCell, lakeCell}, {lakeCell, -lakeCell}, {-lakeCell, lakeCell}, {-lakeCell, -lakeCell}} {
		l := g.lakeIn(baseX+8+off[0], baseZ+8+off[1])
		if !l.exists || seen[[2]int{l.x, l.z}] {
			continue
		}
		seen[[2]int{l.x, l.z}] = true
		rim := g.Height(l.x, l.z) - 1 // water surface sits one below the local rim
		if rim <= SeaLevel+1 {
			continue // no lakes punched into beaches/ocean
		}
		// Lakes only form on flat-ish ground. Reject sites where the terrain
		// across the disc swings more than the lake is deep — otherwise the
		// fixed-height fluid disc drapes down a mountainside and floats in the
		// air on the downhill side (the "floating lava lake" bug).
		if !g.lakeSiteFlat(l, rim) {
			continue
		}
		fluid := Water
		if l.lava {
			fluid = Lava
		}
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				wx, wz := baseX+lx, baseZ+lz
				dx, dz := wx-l.x, wz-l.z
				d2 := float64(dx*dx+dz*dz) / float64(l.r*l.r)
				if d2 > 1 {
					continue
				}
				// Clamp per column: skip columns whose local ground sits well
				// below the rim (the fluid would float there) or well above it
				// (an uphill wall we'd otherwise gouge open).
				if lh := g.Height(wx, wz); lh < rim-l.depth || lh > rim+3 {
					continue
				}
				// Bowl: deepest at the centre, shallow at the rim.
				dip := int(float64(l.depth) * (1 - d2))
				for y := rim - dip; y <= rim; y++ {
					setSectionBlock(ch, lx, y, lz, fluid, true)
				}
				for y := rim + 1; y <= rim+l.depth+4; y++ { // open the air above
					setSectionBlock(ch, lx, y, lz, Air, true)
				}
			}
		}
	}
}

// lakeSiteFlat reports whether the terrain around a lake is level enough to
// hold it: every sampled point on and just past the rim must sit within a
// lake-depth of the water surface. Steep sites (peaks, hillsides) are rejected,
// which is what keeps fluid discs from floating off a slope.
func (g *Generator) lakeSiteFlat(l lake, rim int) bool {
	for _, s := range [][2]int{
		{l.r, 0}, {-l.r, 0}, {0, l.r}, {0, -l.r},
		{l.r, l.r}, {l.r, -l.r}, {-l.r, l.r}, {-l.r, -l.r},
	} {
		if absInt(g.Height(l.x+s[0], l.z+s[1])-(rim+1)) > l.depth+2 {
			return false
		}
	}
	return true
}

// ---- dungeons ----------------------------------------------------------------

// Dungeon describes one buried spawner room.
type Dungeon struct {
	X, Y, Z int // room centre (spawner position)
	W, D    int // half-extents (room spans X±W, Z±D), height 4
	Mob     int // 0 zombie, 1 skeleton, 2 spider
	ChestX  int
	ChestZ  int
	Exists  bool
}

// DungeonIn rolls the dungeon for the cell containing (wx,wz). Exported: the
// server uses it to run spawners and fill loot chests.
func (g *Generator) DungeonIn(wx, wz int) Dungeon {
	ox, oz := cellOrigin(wx, dungeonCell), cellOrigin(wz, dungeonCell)
	if hash01(g.seed, ox, oz, 0xD00) >= dungeonOdds {
		return Dungeon{}
	}
	x := ox + 10 + int(hash01(g.seed, ox, oz, 0xD01)*float64(dungeonCell-20))
	z := oz + 10 + int(hash01(g.seed, ox, oz, 0xD02)*float64(dungeonCell-20))
	surf := g.Height(x, z)
	depth := 12 + int(hash01(g.seed, ox, oz, 0xD03)*30)
	y := surf - depth
	if y < MinY+8 {
		y = MinY + 8
	}
	d := Dungeon{
		X: x, Y: y, Z: z,
		W:      2 + int(hash01(g.seed, ox, oz, 0xD04)*2), // 2..3 → rooms 5x5..7x7
		D:      2 + int(hash01(g.seed, ox, oz, 0xD05)*2),
		Mob:    int(hash01(g.seed, ox, oz, 0xD06) * 3),
		Exists: true,
	}
	d.ChestX = d.X - d.W + int(hash01(g.seed, ox, oz, 0xD07)*float64(2*d.W))
	d.ChestZ = d.Z - d.D // chest against the north wall
	return d
}

// stampDungeons writes the parts of nearby dungeon rooms inside this chunk.
func (g *Generator) stampDungeons(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range [][2]int{{0, 0}, {dungeonCell, 0}, {-dungeonCell, 0}, {0, dungeonCell},
		{0, -dungeonCell}, {dungeonCell, dungeonCell}, {dungeonCell, -dungeonCell},
		{-dungeonCell, dungeonCell}, {-dungeonCell, -dungeonCell}} {
		d := g.DungeonIn(baseX+8+off[0], baseZ+8+off[1])
		if !d.Exists {
			continue
		}
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				wx, wz := baseX+lx, baseZ+lz
				dx, dz := wx-d.X, wz-d.Z
				if dx < -d.W-1 || dx > d.W+1 || dz < -d.D-1 || dz > d.D+1 {
					continue
				}
				wall := dx == -d.W-1 || dx == d.W+1 || dz == -d.D-1 || dz == d.D+1
				for wy := d.Y - 1; wy <= d.Y+4; wy++ {
					switch {
					case wy == d.Y-1 || wy == d.Y+4 || wall: // shell
						b := Cobblestone
						if hash01(g.seed, wx*7+wy, wz*13, 0xD10) < 0.4 {
							b = MossyCobblestone
						}
						setSectionBlock(ch, lx, wy, lz, b, true)
					default: // hollow interior
						setSectionBlock(ch, lx, wy, lz, Air, true)
					}
				}
				if dx == 0 && dz == 0 {
					setSectionBlock(ch, lx, d.Y, lz, Spawner, true)
				}
				if wx == d.ChestX && wz == d.ChestZ {
					setSectionBlock(ch, lx, d.Y, lz, ChestNorth, true)
				}
			}
		}
	}
}

// stampStructures is the decoration entry point for all of the above.
func (g *Generator) stampStructures(ch *Chunk, cx, cz int32) {
	g.stampLakes(ch, cx, cz)
	g.stampMineshafts(ch, cx, cz)
	g.stampDungeons(ch, cx, cz)
	g.stampVillages(ch, cx, cz)
	g.stampStrongholds(ch, cx, cz)
	g.stampDesertTemples(ch, cx, cz)
	g.stampRuinedPortals(ch, cx, cz)
	g.stampOutposts(ch, cx, cz)
	g.stampAncientCity(ch, cx, cz)
	g.stampTrailRuins(ch, cx, cz)
	g.stampAbandonedCamps(ch, cx, cz)
	g.stampTrialChambers(ch, cx, cz)
	g.stampShipwreck(ch, cx, cz)
	g.stampOceanRuins(ch, cx, cz)
	g.stampBuriedTreasure(ch, cx, cz)
	g.stampMonument(ch, cx, cz)
	g.stampIgloo(ch, cx, cz)
	g.stampMansion(ch, cx, cz)
	g.stampDesertWell(ch, cx, cz)
	g.stampJungleTemples(ch, cx, cz) // JungleTemplePiece: the mossy pyramid and its traps
	g.stampSwampHuts(ch, cx, cz)     // SwampHutPiece: the witch's hut on stilts
	g.stampFossils(ch, cx, cz)       // FossilFeature: bones in the desert and swamp rock
	g.stampIcebergs(ch, cx, cz)      // IcebergFeature: the frozen oceans' bergs
	g.stampBlueIce(ch, cx, cz)       // BlueIceFeature: blue ice under the bergs
}
