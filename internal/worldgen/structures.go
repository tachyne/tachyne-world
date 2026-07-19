package worldgen

// Structures: surface lakes, buried dungeons, mineshaft networks and surface
// ruins. Same philosophy as features.go — every structure is a pure function
// of (seed, world coordinates) placed on a sparse grid, and each chunk stamps
// only its own intersection, so neighbours agree with no shared state.

// Structure block states (1.21.5).
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

// Grid cell sizes (blocks) and per-cell odds.
const (
	lakeCell    = 64
	lakeOdds    = 0.10
	dungeonCell = 48
	dungeonOdds = 0.28
	shaftCell   = 256
	shaftOdds   = 0.45
	ruinCell    = 96
	ruinOdds    = 0.16
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

// ---- mineshafts ---------------------------------------------------------------

type shaftArm struct {
	x, y, z int // start
	dx, dz  int // direction (unit)
	length  int
}

// shaftArms builds the deterministic corridor set for the cell at (wx,wz).
func (g *Generator) shaftArms(wx, wz int) []shaftArm {
	ox, oz := cellOrigin(wx, shaftCell), cellOrigin(wz, shaftCell)
	if hash01(g.seed, ox, oz, 0x111E) >= shaftOdds {
		return nil
	}
	cx := ox + shaftCell/4 + int(hash01(g.seed, ox, oz, 0x51)*float64(shaftCell/2))
	cz := oz + shaftCell/4 + int(hash01(g.seed, ox, oz, 0x52)*float64(shaftCell/2))
	y := -30 + int(hash01(g.seed, ox, oz, 0x53)*25) // -30..-5: deepslate/stone band
	dirs := [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}
	var arms []shaftArm
	for i, d := range dirs {
		if hash01(g.seed, ox+i, oz, 0x54) < 0.75 { // ~3 of 4 arms
			ln := 40 + int(hash01(g.seed, ox, oz+i, 0x55)*80)
			arms = append(arms, shaftArm{cx, y, cz, d[0], d[1], ln})
			// One branch partway along, perpendicular.
			if hash01(g.seed, ox+i, oz+i, 0x56) < 0.5 {
				at := ln / 2
				arms = append(arms, shaftArm{cx + d[0]*at, y, cz + d[1]*at, d[1], d[0],
					30 + int(hash01(g.seed, ox-i, oz, 0x57)*40)})
			}
		}
	}
	return arms
}

// stampMineshafts carves corridor cross-sections that pass through this chunk.
func (g *Generator) stampMineshafts(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for ddx := -1; ddx <= 1; ddx++ {
		for ddz := -1; ddz <= 1; ddz++ {
			for _, a := range g.shaftArms(baseX+8+ddx*shaftCell, baseZ+8+ddz*shaftCell) {
				g.stampArm(ch, baseX, baseZ, a)
			}
		}
	}
}

func (g *Generator) stampArm(ch *Chunk, baseX, baseZ int, a shaftArm) {
	for i := 0; i <= a.length; i++ {
		wx, wz := a.x+a.dx*i, a.z+a.dz*i
		lx, lz := wx-baseX, wz-baseZ
		if lx < -1 || lx > 16 || lz < -1 || lz > 16 {
			continue
		}
		// 3-wide corridor: centre + both sides, 3 high.
		px, pz := a.dz, a.dx // perpendicular
		for w := -1; w <= 1; w++ {
			clx, clz := wx+px*w-baseX, wz+pz*w-baseZ
			setSectionBlock(ch, clx, a.y-1, clz, OakPlanks, true) // floor
			for h := 0; h < 3; h++ {
				setSectionBlock(ch, clx, a.y+h, clz, Air, true)
			}
			// Cobwebs in corners now and then.
			if w != 0 && hash01(g.seed, wx*3+w, wz*5, 0x58) < 0.04 {
				setSectionBlock(ch, clx, a.y+2, clz, Cobweb, true)
			}
		}
		// Support frame every 6 blocks: fence posts + plank beam.
		if i%6 == 3 {
			for _, w := range []int{-1, 1} {
				setSectionBlock(ch, wx+px*w-baseX, a.y, wz+pz*w-baseZ, OakFence, true)
				setSectionBlock(ch, wx+px*w-baseX, a.y+1, wz+pz*w-baseZ, OakFence, true)
			}
			for w := -1; w <= 1; w++ {
				setSectionBlock(ch, wx+px*w-baseX, a.y+2, wz+pz*w-baseZ, OakPlanks, true)
			}
		}
		// A stretch of rail along the middle.
		if hash01(g.seed, wx, wz, 0x59) < 0.5 {
			shape := RailFlat // north_south
			if a.dx != 0 {
				shape = RailFlat + 2 // east_west
			}
			setSectionBlock(ch, lx, a.y, lz, shape, true)
		}
	}
}

// ---- surface ruins -------------------------------------------------------------

// stampRuins writes small broken stone-brick shells on the surface.
func (g *Generator) stampRuins(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range [][2]int{{0, 0}, {ruinCell, 0}, {-ruinCell, 0}, {0, ruinCell}, {0, -ruinCell}} {
		ox, oz := cellOrigin(baseX+8+off[0], ruinCell), cellOrigin(baseZ+8+off[1], ruinCell)
		if hash01(g.seed, ox, oz, 0x2E11) >= ruinOdds {
			continue
		}
		rx := ox + 12 + int(hash01(g.seed, ox, oz, 0x2E12)*float64(ruinCell-24))
		rz := oz + 12 + int(hash01(g.seed, ox, oz, 0x2E13)*float64(ruinCell-24))
		surf := g.Height(rx, rz)
		if surf <= SeaLevel+1 || surf >= 96 {
			continue // ruins on habitable land only
		}
		half := 2 + int(hash01(g.seed, ox, oz, 0x2E14)*2) // 5x5 or 7x7 shell
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				wx, wz := baseX+lx, baseZ+lz
				dx, dz := wx-rx, wz-rz
				if dx < -half || dx > half || dz < -half || dz > half {
					continue
				}
				onWall := dx == -half || dx == half || dz == -half || dz == half
				if !onWall {
					continue
				}
				// Broken wall: height 0-3 varying along the perimeter.
				hgt := int(hash01(g.seed, wx, wz, 0x2E15) * 4)
				floorY := g.Height(wx, wz)
				for y := 0; y < hgt; y++ {
					b := StoneBricks
					switch r := hash01(g.seed, wx, wz+y*31, 0x2E16); {
					case r < 0.3:
						b = MossyStoneBricks
					case r < 0.5:
						b = CrackedStoneBricks
					}
					setSectionBlock(ch, lx, floorY+y, lz, b, true)
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
	g.stampRuins(ch, cx, cz)
	g.stampVillages(ch, cx, cz)
	g.stampStrongholds(ch, cx, cz)
	g.stampDesertTemples(ch, cx, cz)
	g.stampRuinedPortals(ch, cx, cz)
	g.stampOutposts(ch, cx, cz)
	g.stampAncientCity(ch, cx, cz)
	g.stampTrialChambers(ch, cx, cz)
	g.stampShipwreck(ch, cx, cz)
	g.stampBuriedTreasure(ch, cx, cz)
	g.stampMonument(ch, cx, cz)
	g.stampIgloo(ch, cx, cz)
}
