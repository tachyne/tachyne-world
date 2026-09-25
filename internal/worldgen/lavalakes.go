package worldgen

// Lava lakes: vanilla's lake_lava_surface (one chunk in 200, on the
// surface) and lake_lava_underground (one chunk in 9, anywhere from y=0 up,
// dropped to the first floor below and at least five blocks under the
// ground), both the LakeFeature with lava and a stone barrier. The lake is
// a union of four to seven ellipsoids in a 16×8×16 box around its origin:
// the lower half lava, the upper half air, and the rock around the lava
// (and, at random, around the air) turned to stone. A lake whose lower rim
// is open to air or whose upper rim touches a fluid is not placed.
//
// These are vanilla's lakes; tachyne's own grid lakes (structures.go) stay
// as they are in land already explored.
//
// A lake's box reaches into the chunks around its origin, so every chunk
// pass replays the lakes of its 3×3 neighbourhood. The lake's shape and its
// placement check read the terrain model, never the chunk being built, so
// every pass agrees; each pass writes only its own cells.
//
// Build guard: a lake with a player's build in its box is not placed.

const (
	lavaLakeSurfaceRarity     = 200
	lavaLakeUndergroundRarity = 9
	lavaLakeGuardMargin       = 2
	lavaLakeSurfaceSalt       = 0x1A7A_5A5
	lavaLakeUndergroundSalt   = 0x1A7A_0D0
)

// lavaLake is one lake, drawn whole: its box corner and filled cells.
type lavaLake struct {
	x, y, z int        // the box's low corner (origin offset -8, -4, -8)
	grid    [2048]bool // (xx*16+zz)*8+yy: the lake's cells
	barrier [2048]bool // cells around it that turn to stone when solid
}

// lakeModel reads the terrain model: noise terrain and noise caves, the
// same in every chunk pass.
type lakeModel struct {
	g    *Generator
	cols map[[2]int]column
}

func (m *lakeModel) at(x, y, z int) uint32 {
	if y < MinY || y >= m.g.Ceiling() {
		return Air
	}
	k := [2]int{x, z}
	c, ok := m.cols[k]
	if !ok {
		c = m.g.columnAt(x, z)
		m.cols[k] = c
	}
	return m.g.terrainCell(c, x, y, z)
}

// placeLavaLakes stamps the lava lakes of this chunk and its neighbours.
func (g *Generator) placeLavaLakes(ch *Chunk, cx, cz int32) {
	if g.nether || g.end {
		return
	}
	m := &lakeModel{g: g, cols: map[[2]int]column{}}
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			// The biome's feature list runs the underground lake first.
			for _, surface := range [2]bool{false, true} {
				if l := g.lavaLakeAt(m, cx+dx, cz+dz, surface); l != nil {
					l.stamp(ch, cx, cz)
				}
			}
		}
	}
}

// lavaLakeAt draws chunk (scx, scz)'s surface or underground lava lake, or
// nil when the chunk has none (its rarity roll, its placement or its check
// failed, or a build is in its box).
func (g *Generator) lavaLakeAt(m *lakeModel, scx, scz int32, surface bool) *lavaLake {
	salt, rarity := int64(lavaLakeUndergroundSalt), lavaLakeUndergroundRarity
	if surface {
		salt, rarity = lavaLakeSurfaceSalt, lavaLakeSurfaceRarity
	}
	r := newTreeRNG(g.seed^salt, int(scx)*16, int(scz)*16).(*hashRNG)
	if r.Intn(rarity) != 0 {
		return nil
	}
	x, z := int(scx)*16+r.Intn(16), int(scz)*16+r.Intn(16)
	h := g.Height(x, z)
	var y int
	if surface {
		y = max(h, SeaLevel) // WORLD_SURFACE_WG: over the sea, its surface
	} else {
		// height_range 0..top, then scan down (≤32) for a non-air cell with
		// five blocks of world under it, at least five under the sea floor.
		y = r.Intn(g.Ceiling())
		found := false
		for i := 0; ; i++ {
			if m.at(x, y, z) != Air && y-5 >= MinY {
				found = true
				break
			}
			if i >= 32 {
				break
			}
			y--
			if y < MinY {
				break
			}
		}
		if !found || y-h > -5 || g.caveBiomeAt(x, y, z) == "minecraft:deep_dark" {
			return nil
		}
	}
	if y <= MinY+4 {
		return nil
	}
	l := &lavaLake{x: x - 8, y: y - 4, z: z - 8}
	spots := r.Intn(4) + 4
	for i := 0; i < spots; i++ {
		xr := r.Float64()*6 + 3
		yr := r.Float64()*4 + 2
		zr := r.Float64()*6 + 3
		xp := r.Float64()*(16-xr-2) + 1 + xr/2
		yp := r.Float64()*(8-yr-4) + 2 + yr/2
		zp := r.Float64()*(16-zr-2) + 1 + zr/2
		for xx := 1; xx < 15; xx++ {
			for zz := 1; zz < 15; zz++ {
				for yy := 1; yy < 7; yy++ {
					xd, yd, zd := (float64(xx)-xp)/(xr/2), (float64(yy)-yp)/(yr/2), (float64(zz)-zp)/(zr/2)
					if xd*xd+yd*yd+zd*zd < 1 {
						l.grid[(xx*16+zz)*8+yy] = true
					}
				}
			}
		}
	}
	// The rim check, against the terrain model.
	for xx := 0; xx < 16; xx++ {
		for zz := 0; zz < 16; zz++ {
			for yy := 0; yy < 8; yy++ {
				if !l.rim(xx, yy, zz) {
					continue
				}
				s := m.at(l.x+xx, l.y+yy, l.z+zz)
				if yy >= 4 && IsFluid(s) {
					return nil
				}
				if yy < 4 && !solid(s) && s != Lava {
					return nil
				}
			}
		}
	}
	// The barrier's draws: below the surface every rim cell, above it half.
	for xx := 0; xx < 16; xx++ {
		for zz := 0; zz < 16; zz++ {
			for yy := 0; yy < 8; yy++ {
				if l.rim(xx, yy, zz) && (yy < 4 || r.Intn(2) != 0) {
					l.barrier[(xx*16+zz)*8+yy] = true
				}
			}
		}
	}
	if g.builtIn(l.x-lavaLakeGuardMargin, l.y-lavaLakeGuardMargin, l.z-lavaLakeGuardMargin,
		l.x+15+lavaLakeGuardMargin, l.y+7+lavaLakeGuardMargin, l.z+15+lavaLakeGuardMargin) {
		return nil
	}
	return l
}

// rim reports whether box cell (xx, yy, zz) is outside the lake but touches it.
func (l *lavaLake) rim(xx, yy, zz int) bool {
	g := &l.grid
	return !g[(xx*16+zz)*8+yy] &&
		(xx < 15 && g[((xx+1)*16+zz)*8+yy] ||
			xx > 0 && g[((xx-1)*16+zz)*8+yy] ||
			zz < 15 && g[(xx*16+zz+1)*8+yy] ||
			zz > 0 && g[(xx*16+zz-1)*8+yy] ||
			yy < 7 && g[(xx*16+zz)*8+yy+1] ||
			yy > 0 && g[(xx*16+zz)*8+yy-1])
}

// stamp writes the lake's cells that fall in chunk (cx, cz).
func (l *lavaLake) stamp(ch *Chunk, cx, cz int32) {
	bx, bz := int(cx)*16, int(cz)*16
	for xx := 0; xx < 16; xx++ {
		lx := l.x + xx - bx
		if lx < 0 || lx > 15 {
			continue
		}
		for zz := 0; zz < 16; zz++ {
			lz := l.z + zz - bz
			if lz < 0 || lz > 15 {
				continue
			}
			for yy := 0; yy < 8; yy++ {
				y := l.y + yy
				if y < MinY || y >= MinY+len(ch.Sections)*16 {
					continue
				}
				i := (xx*16+zz)*8 + yy
				s := sectionBlockAt(ch, lx, y, lz)
				switch {
				case l.grid[i] && s != Bedrock:
					fill := Lava
					if yy >= 4 {
						fill = Air
					}
					setSectionBlock(ch, lx, y, lz, fill, true)
				case l.barrier[i] && solid(s) && s != Bedrock && !IsLog(s) && !IsLeaves(s):
					setSectionBlock(ch, lx, y, lz, Stone, true)
				}
			}
		}
	}
}
