package worldgen

// The Nether: a `nether` mode on the Generator. Same -64..384 canvas as the
// overworld (the dimension registry declares matching bounds, so the chunk
// pipeline is untouched) but the terrain is a cavern sponge: 3D noise density
// carves interlocking caverns through netherrack between the bedrock floor
// and a natural ceiling around y=120, a lava sea floods everything below
// y=-32, and glowstone blobs, soul sand and quartz break up the walls.

// Nether block states (1.21.5).
const (
	NetherLavaSea = -16 // lava floods below this
	NetherCeiling = 120 // caverns close up here; above is open dark void
)

var (
	Netherrack      = blockBase("netherrack")
	SoulSand        = blockBase("soul_sand")
	Glowstone       = blockBase("glowstone")
	NetherQuartzOre = blockBase("nether_quartz_ore")
	NetherPortal    = blockBase("nether_portal") // axis=x (6044 = axis z)
	NetherWart      = blockBase("nether_wart")   // + age 0..3
	Obsidian        = blockBase("obsidian")
)

// NewNetherGenerator builds a Generator in nether mode (same seed noise
// family, different assembly).
func NewNetherGenerator(seed int64) *Generator {
	g := NewGenerator(seed ^ 0x6E7BE7) // distinct noise from the overworld
	g.nether = true
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
		return -1 // open void above the ceiling
	}
	return d
}

// netherBlock assembles one nether column cell.
func (g *Generator) netherBlock(x, y, z int) uint32 {
	if y < MinY+4 { // bedrock floor with a ragged top
		if y <= MinY || hash01(g.seed, x+y*31, z, 0xBED) < 0.5 {
			return Bedrock
		}
	}
	if g.netherDensity(x, y, z) > 0.15 {
		// Solid: mostly netherrack with ore/soul-sand/glowstone variety.
		switch r := hash01(g.seed, x+y*257, z, 0x6E1); {
		case r < 0.02 && y < 100:
			return NetherQuartzOre
		case r < 0.10 && g.netherDensity(x, y+1, z) <= 0.15:
			return SoulSand // floor patches (open cavern above)
		case r < 0.115 && g.netherDensity(x, y-1, z) <= 0.15 && y > 40:
			return Glowstone // glowing crust on cavern ceilings (open below)
		}
		return Netherrack
	}
	if y <= NetherLavaSea {
		return Lava
	}
	return Air
}

// generateNetherChunk fills a chunk in nether mode.
func (g *Generator) generateNetherChunk(cx, cz int32) *Chunk {
	ch := &Chunk{}
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := int(cx)*16+lx, int(cz)*16+lz
			prev := uint32(Bedrock)
			for s := 0; s < SectionCount; s++ {
				for ly := 0; ly < 16; ly++ {
					wy := MinY + s*16 + ly
					b := g.netherBlock(wx, wy, wz)
					// Wild nether wart sprouts on soul-sand floors.
					if b == Air && prev == SoulSand && hash01(g.seed, wx, wz, 0x3A57) < 0.4 {
						b = NetherWart + uint32(hash01(g.seed, wx, wz, 0x3A58)*4)
					}
					ch.Sections[s][(ly*16+lz)*16+lx] = b
					prev = b
				}
			}
		}
	}
	for s := 0; s < SectionCount; s++ {
		ch.Biomes[s] = "minecraft:nether_wastes"
	}
	ch.computeHeightmap()
	return ch
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
