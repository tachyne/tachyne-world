package worldgen

// Villages: a well at the centre, plank houses on a ring around it, wheat
// farms, and dirt paths — placed only on flat land, all deterministic from
// the seed like every other structure.

const (
	villageCell = 384
	villageOdds = 0.85 // most cells roll a village (they were far too rare to ever find)
	villageFlat = 14   // max height spread across the site — houses terrace on the slope

	// Protected spawn zone: Legion's castle is here and the migrated (larger)
	// jigsaw village would crowd it, so no village generates within this radius.
	villGuardX, villGuardZ = 33, -95
	villGuardR2            = 256 * 256
)

var (
	//                    (each builds its own foundation), so a looser gate just means a
	//                    hillside village, not floating houses. Was 6, which rejected ~87%
	//                    of sites on the varied biome terrain → nearest village ~940 blocks.

	DirtPath  = blockBase("dirt_path")
	Farmland  = blockBase("farmland") + 7 // fully moist
	Wheat     = blockBase("wheat")        // + age 0..7
	Torch     = blockBase("torch")
	Bell      = blockBase("bell") + 1
	GlassPane = blockBase("glass_pane") + 31 // unconnected pane

	// Furniture: a bed every house + one profession workstation.
	BedFootNorth = blockBase("red_bed") + 3 // red_bed, facing north, foot (head one block north)
	BedHeadNorth = blockBase("red_bed") + 2 // red_bed, facing north, head

	// Peaked-roof + door parts.
	OakStairsNorth = blockBase("oak_stairs") + 11 // oak_stairs facing north, bottom
	OakStairsSouth = blockBase("oak_stairs") + 31 // oak_stairs facing south, bottom
	OakSlab        = blockBase("oak_slab") + 3    // oak_slab bottom (roof ridge)
	OakDoorLowerS  = blockBase("oak_door") + 27   // oak_door lower, facing south
	OakDoorUpperS  = blockBase("oak_door") + 19   // oak_door upper, facing south
)

// workstations are the profession job-site blocks scattered one-per-house.
var workstations = []uint32{
	4341,  // crafting_table
	4359,  // furnace
	20400, // composter (farmer)
	19476, // lectern (librarian)
	19432, // barrel (fisherman)
	19459, // cartography_table
	19489, // smithing_table
	19460, // fletching_table
	19427, // loom (shepherd)
	19490, // stonecutter (mason)
	19465, // grindstone (weaponsmith)
	19452, // blast_furnace (armorer)
	19444, // smoker (butcher)
	8181,  // brewing_stand (cleric-ish)
}

// House is one village building (server spawns villagers at houses).
type House struct {
	X, Z int // floor centre
	Y    int // floor height (ground at centre)
	Half int // half-extent (2 → 5x5)
}

// Village is the exported layout for one cell.
type Village struct {
	X, Z    int // well centre
	Y       int
	Variant string // biome variant: plains | desert | savanna | snowy | taiga
	Houses  []House
	Farms   []House // reuse the box shape: X,Z centre + Half
	Exists  bool
}

// villageVariant maps a site biome to its vanilla village style. Village
// placement stays flatness/dry-gated (not biome-gated) as tachyne always has;
// this only picks which template set stamps, so a desert biome gets a sandstone
// village instead of oak cottages. Anything without a village style falls back
// to plains (vanilla's default and the widest template set).
func villageVariant(biome string) string {
	switch biome {
	case "minecraft:desert":
		return "desert"
	case "minecraft:savanna", "minecraft:savanna_plateau":
		return "savanna"
	case "minecraft:snowy_plains", "minecraft:ice_spikes", "minecraft:snowy_slopes":
		return "snowy"
	case "minecraft:taiga", "minecraft:snowy_taiga",
		"minecraft:old_growth_pine_taiga", "minecraft:old_growth_spruce_taiga":
		return "taiga"
	default:
		return "plains"
	}
}

// VillageIn rolls the village for the cell containing (wx,wz).
func (g *Generator) VillageIn(wx, wz int) Village {
	ox, oz := cellOrigin(wx, villageCell), cellOrigin(wz, villageCell)
	if hash01(g.seed, ox, oz, 0x71A6E) >= villageOdds {
		return Village{}
	}
	cx := ox + villageCell/4 + int(hash01(g.seed, ox, oz, 0x71)*float64(villageCell/2))
	cz := oz + villageCell/4 + int(hash01(g.seed, ox, oz, 0x72)*float64(villageCell/2))
	// Protected zone: no village near the spawn area / Legion's castle, where the
	// migrated (larger) jigsaw village would crowd an established player build.
	if dx, dz := cx-villGuardX, cz-villGuardZ; dx*dx+dz*dz < villGuardR2 {
		return Village{}
	}
	// Flatness + dry-land gate: sample the site.
	lo, hi := 1<<30, -(1 << 30)
	for _, d := range [][2]int{{0, 0}, {24, 0}, {-24, 0}, {0, 24}, {0, -24}, {16, 16}, {-16, -16}} {
		h := g.Height(cx+d[0], cz+d[1])
		if h < lo {
			lo = h
		}
		if h > hi {
			hi = h
		}
	}
	if hi-lo > villageFlat || lo <= SeaLevel+1 {
		return Village{}
	}
	// Site only — the buildings, beds, job-sites and bell come from the real
	// vanilla templates via AssembleVillage (village_jigsaw.go). The variant
	// (biome style) is fixed by the biome at the well.
	return Village{X: cx, Z: cz, Y: g.Height(cx, cz),
		Variant: villageVariant(g.BiomeName(cx, cz)), Exists: true}
}

// stampVillages stamps the real vanilla village pieces overlapping this chunk.
func (g *Generator) stampVillages(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for ddx := -1; ddx <= 1; ddx++ {
		for ddz := -1; ddz <= 1; ddz++ {
			v := g.VillageIn(baseX+8+ddx*villageCell, baseZ+8+ddz*villageCell)
			if !v.Exists {
				continue
			}
			g.StampPieces(ch, cx, cz, g.AssembleVillage(v))
		}
	}
}

// stampWell: a 4x4 cobble collar around a 2x2 water shaft, plus the bell.
func (g *Generator) stampWell(ch *Chunk, baseX, baseZ int, v Village) {
	for dx := -2; dx <= 1; dx++ {
		for dz := -2; dz <= 1; dz++ {
			wx, wz := v.X+dx, v.Z+dz
			lx, lz := wx-baseX, wz-baseZ
			inner := dx >= -1 && dx <= 0 && dz >= -1 && dz <= 0
			if inner {
				for y := v.Y - 8; y < v.Y; y++ { // the shaft
					setSectionBlock(ch, lx, y, lz, Water, true)
				}
				setSectionBlock(ch, lx, v.Y, lz, Air, true)
			} else {
				setSectionBlock(ch, lx, v.Y-1, lz, Cobblestone, true)
				setSectionBlock(ch, lx, v.Y, lz, Cobblestone, true)
			}
		}
	}
	setSectionBlock(ch, v.X+2-baseX, v.Y, v.Z-baseZ, Bell, true)
}

// stampHouse builds a plains-style cottage: cobble foundation, plank floor, log
// corner posts, plank walls with glass windows, a real oak door, a peaked
// stair roof — and furniture inside (a bed + one profession workstation).
func (g *Generator) stampHouse(ch *Chunk, baseX, baseZ int, h House) {
	half := h.Half
	put := func(dx, y, dz int, b uint32) { setSectionBlock(ch, h.X+dx-baseX, y, h.Z+dz-baseZ, b, true) }

	for dx := -half; dx <= half; dx++ {
		for dz := -half; dz <= half; dz++ {
			wx, wz := h.X+dx, h.Z+dz
			// Foundation down to terrain + a plank floor.
			for y := g.Height(wx, wz) - 1; y < h.Y; y++ {
				put(dx, y, dz, Cobblestone)
			}
			put(dx, h.Y-1, dz, OakPlanks)
			wall := dx == -half || dx == half || dz == -half || dz == half
			corner := (dx == -half || dx == half) && (dz == -half || dz == half)
			door := dx == 0 && dz == half // south face centre
			for y := h.Y; y < h.Y+3; y++ {
				switch {
				case door && y < h.Y+2:
					put(dx, y, dz, Air) // doorway (the door itself is placed below)
				case corner:
					put(dx, y, dz, OakLog)
				case wall:
					b := OakPlanks
					if y == h.Y+1 && (dx == -half || dx == half) && dz == 0 {
						b = GlassPane // a window on each side wall
					}
					put(dx, y, dz, b)
				default:
					put(dx, y, dz, Air) // hollow interior
				}
			}
		}
	}

	// A real oak door in the south doorway.
	put(0, h.Y, half, OakDoorLowerS)
	put(0, h.Y+1, half, OakDoorUpperS)

	// Peaked gable roof: rows slope up (stairs) to a plank ridge at dz=0. It
	// overhangs the walls by one block in X for eaves.
	ridgeY := h.Y + 3
	for dx := -half - 1; dx <= half+1; dx++ {
		for dz := -half; dz <= half; dz++ {
			var y int
			var b uint32
			switch {
			case dz == 0: // ridge
				y, b = ridgeY+half, OakSlab
			case dz < 0: // north slope rises toward the ridge
				y, b = ridgeY+(half+dz), OakStairsSouth
			default: // south slope
				y, b = ridgeY+(half-dz), OakStairsNorth
			}
			put(dx, y, dz, b)
			// Fill the gable-end triangles solid so there's no gap under the slope.
			if dx == -half || dx == half {
				for fy := h.Y + 3; fy < y; fy++ {
					put(dx, fy, dz, OakPlanks)
				}
			}
		}
	}

	put(0, h.Y+2, 0, Torch) // interior light hung from the ridge
	g.furnishHouse(baseX, baseZ, ch, h)
}

// furnishHouse places a bed and one profession workstation inside a cottage,
// picked deterministically so every house is equipped but they vary.
func (g *Generator) furnishHouse(baseX, baseZ int, ch *Chunk, h House) {
	put := func(dx, y, dz int, b uint32) { setSectionBlock(ch, h.X+dx-baseX, y, h.Z+dz-baseZ, b, true) }
	// Bed against the west interior wall: foot at dz=+1, head at dz=0 (faces north).
	put(-1, h.Y, 1, BedFootNorth)
	put(-1, h.Y, 0, BedHeadNorth)
	// Workstation in one corner, a loot chest in the free corner.
	put(1, h.Y, -1, g.HouseWorkstation(h))
	put(-1, h.Y, -1, ChestNorth)
}

// houseWorkstationIdx is the deterministic workstation slot for a house — the
// single source of truth for both furnishing and the loot-table lookup.
func (g *Generator) houseWorkstationIdx(h House) int {
	return int(hash01(g.seed, h.X, h.Z, 0x7B)*float64(len(workstations))) % len(workstations)
}

// HouseWorkstation is the workstation block a house is furnished with (used by
// the server to pick that house's chest loot table).
func (g *Generator) HouseWorkstation(h House) uint32 {
	return workstations[g.houseWorkstationIdx(h)]
}

// HouseChest is the world position of a house's loot chest (the free interior
// corner, opposite the bed).
func (g *Generator) HouseChest(h House) (x, y, z int) {
	return h.X - 1, h.Y, h.Z - 1
}

// stampPath: an L-shaped dirt path from the well to a house door.
func (g *Generator) stampPath(ch *Chunk, baseX, baseZ int, v Village, h House) {
	put := func(wx, wz int) {
		y := g.Height(wx, wz) - 1
		setSectionBlock(ch, wx-baseX, y, wz-baseZ, DirtPath, true)
		setSectionBlock(ch, wx-baseX, y+1, wz-baseZ, Air, false)
	}
	x, z := v.X, v.Z
	for x != h.X {
		put(x, z)
		if h.X > x {
			x++
		} else {
			x--
		}
	}
	for z != h.Z+h.Half+1 && z != h.Z-h.Half-1 && z != h.Z {
		put(x, z)
		if h.Z > z {
			z++
		} else {
			z--
		}
	}
}

// stampFarm: farmland rows with wheat, split by a water channel.
func (g *Generator) stampFarm(ch *Chunk, baseX, baseZ int, f House) {
	for dx := -f.Half; dx <= f.Half; dx++ {
		for dz := -2; dz <= 2; dz++ {
			wx, wz := f.X+dx, f.Z+dz
			lx, lz := wx-baseX, wz-baseZ
			if dx == 0 { // channel
				setSectionBlock(ch, lx, f.Y-1, lz, Water, true)
				setSectionBlock(ch, lx, f.Y, lz, Air, true)
				continue
			}
			setSectionBlock(ch, lx, f.Y-1, lz, Farmland, true)
			age := uint32(hash01(g.seed, wx, wz, 0x78) * 8)
			setSectionBlock(ch, lx, f.Y, lz, Wheat+age, true)
			setSectionBlock(ch, lx, f.Y+1, lz, Air, true)
		}
	}
}

// cos01/sin01 take a turn fraction [0,1) instead of radians.
func cos01(t float64) float64 { return cosApprox(t) }
func sin01(t float64) float64 { return cosApprox(t - 0.25) }

// cosApprox: cheap cosine of a turn via a parabola pair — plenty for layout.
func cosApprox(t float64) float64 {
	t -= float64(int(t))
	if t < 0 {
		t++
	}
	// Map to [-1,1] triangle then smooth.
	x := 4*t - 2
	if x < 0 {
		x = -x
	}
	x -= 1 // cos-shaped in [-1,1]
	return x * (2 - absF(x))
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
