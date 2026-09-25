package worldgen

// The Nether: a `nether` mode on the Generator. Same -64..384 canvas as the
// overworld (the dimension registry declares matching bounds, so the chunk
// pipeline is untouched) but the terrain is a cavern sponge: 3D noise density
// carves interlocking caverns through netherrack between the bedrock floor
// and a natural ceiling around y=120, netherrack then fills up to vanilla's
// bedrock roof at y=127 (open air above it), a lava sea floods everything
// below y=-32, and glowstone blobs, soul sand and quartz break up the walls.

// Nether block states (1.21.5).
const (
	NetherLavaSea = -16 // lava floods below this
	NetherCeiling = 120 // caverns close up here; netherrack above, to the roof
	// NetherRoof is the bedrock roof (bedrock_roof): solid bedrock at y=127,
	// thinning to nothing over the five blocks under it, with open air above
	// as in vanilla. Between the caverns' top and the roof is netherrack.
	NetherRoof = 127
	// netherRoofGuardY and netherRoofGuardReach: the roof came late, and a
	// column with a player's block at or above y=115, or within two columns
	// of one, keeps the open void it had (nether.go netherRoofGuard).
	netherRoofGuardY     = NetherCeiling - 5
	netherRoofGuardReach = 2
)

var (
	Netherrack      = blockBase("netherrack")
	SoulSand        = blockBase("soul_sand")
	Glowstone       = blockBase("glowstone")
	NetherQuartzOre = blockBase("nether_quartz_ore")
	NetherGoldOre   = blockBase("nether_gold_ore")
	NetherPortal    = blockBase("nether_portal") // axis=x (6044 = axis z)
	NetherWart      = blockBase("nether_wart")   // + age 0..3
	Obsidian        = blockBase("obsidian")
)

// NewNetherGenerator builds a Generator in nether mode (same seed noise
// family, different assembly).
func NewNetherGenerator(seed int64) *Generator {
	g := NewGenerator(seed ^ 0x6E7BE7) // distinct noise from the overworld
	g.nether = true
	g.netherN = newNetherNoises(seed ^ 0x6E7BE7)
	return g
}

// netherDensity: >0 = solid netherrack. Two stacked fBm fields make bulbous
// caverns; a floor/ceiling gradient closes the world at both ends.
func (g *Generator) netherDensity(x, y, z int) float64 {
	fx, fy, fz := float64(x)/90, float64(y)/60, float64(z)/90
	d := g.caveA.Noise3(fx, fy, fz) + 0.5*g.caveA.Noise3(fx*2, fy*2, fz*2)
	d += 0.5 * g.caveB.Noise3(fx*2.1, fy*1.7, fz*2.1)
	// Push solid near the floor and the ceiling so caverns stay enclosed.
	if y < MinY+12 {
		d += float64(MinY+12-y) * 0.1
	}
	if y > NetherCeiling-16 {
		d += float64(y-(NetherCeiling-16)) * 0.08
	}
	if y > NetherCeiling {
		return -1 // the caverns end at the ceiling (netherCell fills to the roof)
	}
	return d
}

// netherBlock assembles one nether column cell.
func (g *Generator) netherBlock(x, y, z int) uint32 { return g.netherCell(x, y, z, true) }

// netherCell is netherBlock with the roof optional: roof=false is the open
// void above the caverns that the Nether had before it had a roof, which a
// column players built in keeps.
func (g *Generator) netherCell(x, y, z int, roof bool) uint32 {
	if roof && y > NetherCeiling {
		switch {
		case y > NetherRoof:
			return Air
		case y == NetherRoof:
			return Bedrock
		case y > NetherRoof-5 && hash01(g.seed, x+y*31, z, 0xBED7) < float64(y-(NetherRoof-5))/5:
			return Bedrock // the vertical gradient: one in five at 123, four in five at 126
		}
		return Netherrack
	}
	if y < MinY+4 { // bedrock floor with a ragged top
		if y <= MinY || hash01(g.seed, x+y*31, z, 0xBED) < 0.5 {
			return Bedrock
		}
	}
	if g.netherDensity(x, y, z) > 0.15 {
		return Netherrack // ores, soul sand and glowstone come from vanilla's features (netherfeatures.go)
	}
	if y <= NetherLavaSea {
		return Lava
	}
	return Air
}

// generateNetherChunk fills a chunk in nether mode.
func (g *Generator) generateNetherChunk(cx, cz int32) *Chunk {
	ch := NewChunk(g.sections)
	voidCols := g.netherRoofGuard(cx, cz)
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := int(cx)*16+lx, int(cz)*16+lz
			col := g.netherColumnRoof(wx, wz, !voidCols[lz*16+lx]) // the terrain dressed by its biome's surface rules
			for s := 0; s < len(ch.Sections); s++ {
				for ly := 0; ly < 16; ly++ {
					wy := MinY + s*16 + ly
					ch.Sections[s][(ly*16+lz)*16+lx] = col[wy-MinY]
				}
			}
		}
	}
	g.stampNetherPortals(ch, cx, cz) // ruined portals stand on the cavern floors too
	g.stampBastions(ch, cx, cz)
	g.stampFortress(ch, cx, cz)
	g.stampNetherFossils(ch, cx, cz) // bone-block fossils in the soul sand valley
	biome := g.netherBiome(int(cx)*16+8, int(cz)*16+8)
	for s := 0; s < len(ch.Sections); s++ {
		ch.Biomes[s] = biome
	}
	g.decorateNether(ch, cx, cz) // the forests' fungi, roots and vines
	ch.computeHeightmap()
	return ch
}

// netherRoofGuard marks the chunk's columns (lz*16+lx) that keep the open
// void: the roof arrived after players had been up there, so any column with
// a build block at or above netherRoofGuardY, or within netherRoofGuardReach
// columns of one, is generated as it was before (buildguard.go) — a build on
// the old ceiling is not sealed into netherrack and bedrock.
func (g *Generator) netherRoofGuard(cx, cz int32) (voidCols [256]bool) {
	if g.editsIn == nil {
		return voidCols
	}
	baseX, baseZ := int(cx)*16, int(cz)*16
	for dx := int32(-1); dx <= 1; dx++ {
		for dz := int32(-1); dz <= 1; dz++ {
			g.editsIn(cx+dx, cz+dz, func(x, y, z int, s uint32) {
				if y < netherRoofGuardY || !isBuildBlock(s) {
					return
				}
				for ox := -netherRoofGuardReach; ox <= netherRoofGuardReach; ox++ {
					for oz := -netherRoofGuardReach; oz <= netherRoofGuardReach; oz++ {
						lx, lz := x+ox-baseX, z+oz-baseZ
						if lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
							voidCols[lz*16+lx] = true
						}
					}
				}
			})
		}
	}
	return voidCols
}

// NetherFloor finds a standable cavern floor near a column: the lowest air
// cell above the lava sea with solid ground under it (for portals/spawns).
func (g *Generator) NetherFloor(x, z int) int {
	y, _ := g.netherFloorOK(x, z)
	return y
}

// NetherFloorOK is NetherFloor with an honest miss signal (callers that
// spawn/land must not trust the fallback height).
func (g *Generator) NetherFloorOK(x, z int) (int, bool) { return g.netherFloorOK(x, z) }

func (g *Generator) netherFloorOK(x, z int) (int, bool) {
	for y := NetherLavaSea + 1; y < NetherCeiling-4; y++ {
		if g.netherBlock(x, y, z) == Air && g.netherBlock(x, y+1, z) == Air &&
			g.netherBlock(x, y-1, z) != Air && g.netherBlock(x, y-1, z) != Lava {
			return y, true
		}
	}
	return 32, false // no natural cavern here — callers carve a refuge
}

// NetherLanding picks a genuinely walkable portal spot near (x,z): spiral out
// looking for a 3x3 solid floor with 3 blocks of headroom, nowhere touching
// lava. ok=false means no natural spot within range — carve a refuge instead.
func (g *Generator) NetherLanding(x, z int) (int, int, int, bool) {
	for r := 0; r <= 96; r += 4 {
		for dx := -r; dx <= r; dx += 4 {
			for dz := -r; dz <= r; dz += 4 {
				if absInt(dx) != r && absInt(dz) != r {
					continue // ring only
				}
				cx, cz := x+dx, z+dz
				y, ok := g.netherFloorOK(cx, cz)
				if !ok {
					continue
				}
				good := true
				for ax := -1; ax <= 1 && good; ax++ {
					for az := -1; az <= 1 && good; az++ {
						floor := g.netherBlock(cx+ax, y-1, cz+az)
						if floor == Air || floor == Lava {
							good = false // hole or lava shore under the pad
						}
						for ay := 0; ay < 3; ay++ {
							if b := g.netherBlock(cx+ax, y+ay, cz+az); b != Air {
								good = false
							}
						}
					}
				}
				if good {
					return cx, y, cz, true
				}
			}
		}
	}
	return x, NetherLavaSea + 1, z, false // refuge: an obsidian island on the sea
}
