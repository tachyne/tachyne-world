package worldgen

// Pillager outpost — the dark-oak watchtower, reimplemented from the 1.21.5
// pillager_outpost jigsaw feature (facts: dimensions, dark-oak/cobblestone
// palette, an upper loot chest, and the ominous banner on top). We build a
// single procedural watchtower rather than the full jigsaw (tents/cages/targets
// omitted). The server populates it with a captain + pillager squad on approach
// (see updateOutposts) and fills the chest on first open. Outposts sit on dry
// land and keep clear of villages, mirroring vanilla's shared structure set.

const (
	outpostCell = 400 // one outpost per ~400-block cell (when the roll + siting pass)
)

var (
	DarkOakPlanks = blockBase("dark_oak_planks")     // dark_oak_planks
	DarkOakFence  = blockBase("dark_oak_fence") + 31 // dark_oak_fence (default: no connections)
	WhiteBanner   = blockBase("white_banner")        // white_banner rotation=0 (stands in for the ominous banner)
)

// PillagerOutpost is a dark-oak watchtower with an upper loot chest.
type PillagerOutpost struct {
	X, Y, Z                int // tower base centre, at the surface
	ChestX, ChestY, ChestZ int
	Exists                 bool
}

// OutpostIn returns the outpost whose cell contains (wx,wz), if the roll passes
// and the site is dry land clear of any village.
func (g *Generator) OutpostIn(wx, wz int) PillagerOutpost {
	ox, oz := cellOrigin(wx, outpostCell), cellOrigin(wz, outpostCell)
	if hash01(g.seed, ox, oz, 0x0057) >= 0.45 {
		return PillagerOutpost{}
	}
	x := ox + 40 + int(hash01(g.seed, ox, oz, 0x0058)*float64(outpostCell-80))
	z := oz + 40 + int(hash01(g.seed, ox, oz, 0x0059)*float64(outpostCell-80))
	y := g.Height(x, z)
	if y <= SeaLevel { // dry land only
		return PillagerOutpost{}
	}
	// Keep clear of villages (vanilla shares the village structure set with a
	// min separation, so the two never overlap).
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if g.VillageIn(x+dx*384, z+dz*384).Exists {
				return PillagerOutpost{}
			}
		}
	}
	p := PillagerOutpost{X: x, Y: y, Z: z, Exists: true}
	p.ChestX, p.ChestY, p.ChestZ = x+1, y+13, z+1 // on the cabin floor up top
	return p
}

func (g *Generator) stampOutposts(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(outpostCell) {
		p := g.OutpostIn(baseX+8+off[0], baseZ+8+off[1])
		if !p.Exists {
			continue
		}
		g.stampOutpost(ch, baseX, baseZ, p)
	}
}

// stampOutpost writes the parts of the watchtower that fall inside this chunk:
// a 7×7 cobblestone footing, four dark-oak corner posts, two plank floors, an
// overhanging 9×9 platform with a fence railing, a roofed cabin, an upper loot
// chest, and the ominous banner on the roof.
func (g *Generator) stampOutpost(ch *Chunk, baseX, baseZ int, p PillagerOutpost) {
	const (
		base = 3 // 7×7 foundation half-width
		plat = 4 // 9×9 top platform half-width
		cab  = 2 // 5×5 cabin half-width
	)
	top := p.Y + 12 // platform floor level

	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := baseX+lx, baseZ+lz
			dx, dz := wx-p.X, wz-p.Z
			adx, adz := abs(dx), abs(dz)
			corner := adx == base && adz == base

			// Foundation slab, with cobblestone legs dropped at the corners so
			// the tower stands level on a slope.
			if adx <= base && adz <= base {
				setSectionBlock(ch, lx, p.Y, lz, Cobblestone, true)
				if corner {
					for wy := p.Y - 1; wy >= p.Y-4; wy-- {
						setSectionBlock(ch, lx, wy, lz, Cobblestone, true)
					}
				}
			}

			// Corner posts, plank floors, hollow interior between them.
			if adx <= base && adz <= base {
				for wy := p.Y + 1; wy < top; wy++ {
					switch {
					case wy == p.Y+4 || wy == p.Y+8: // interior floors
						setSectionBlock(ch, lx, wy, lz, DarkOakPlanks, true)
					case corner:
						setSectionBlock(ch, lx, wy, lz, DarkOakLog, true)
					default:
						setSectionBlock(ch, lx, wy, lz, Air, true)
					}
				}
			}

			// Overhanging platform + fence railing around its edge.
			if adx <= plat && adz <= plat {
				setSectionBlock(ch, lx, top, lz, DarkOakPlanks, true)
				if adx == plat || adz == plat {
					setSectionBlock(ch, lx, top+1, lz, DarkOakFence, true)
				}
			}

			// Cabin: 5×5 plank walls (doorway on the +Z face) and a flat roof.
			if adx <= cab && adz <= cab {
				wall := adx == cab || adz == cab
				for wy := top + 1; wy <= top+3; wy++ {
					switch {
					case wall && dz == cab && dx == 0 && wy <= top+2: // doorway gap
						setSectionBlock(ch, lx, wy, lz, Air, true)
					case wall:
						setSectionBlock(ch, lx, wy, lz, DarkOakPlanks, true)
					default:
						setSectionBlock(ch, lx, wy, lz, Air, true)
					}
				}
				setSectionBlock(ch, lx, top+4, lz, DarkOakPlanks, true) // roof
			}

			// Loot chest inside the cabin, banner on the roof, an interior torch.
			if wx == p.ChestX && wz == p.ChestZ {
				setSectionBlock(ch, lx, p.ChestY, lz, ChestNorth, true)
			}
			if dx == 0 && dz == 0 {
				setSectionBlock(ch, lx, top+5, lz, WhiteBanner, true) // ominous banner
				setSectionBlock(ch, lx, p.Y+9, lz, Torch, true)       // interior light
			}
		}
	}
}
