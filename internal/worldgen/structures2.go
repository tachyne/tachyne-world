package worldgen

// Extra overworld structures — desert temples and ruined portals — following
// the existing query+stamp pattern (a seed-deterministic XIn() query and a
// stampX() that writes the parts falling inside a chunk). Dimensions/loot follow
// vanilla (facts from the wiki/datapack); the layout is reimplemented, not the
// jigsaw engine. Server fills the chests on first open.

const (
	templeCell = 336 // one desert temple per ~336-block cell (when it lands on desert)
	portalCell = 208 // one ruined portal per ~208-block cell
)

var (
	TNTBlock           = blockBase("tnt")                      // minecraft:tnt (unstable=false)
	StonePressurePlate = blockBase("stone_pressure_plate") + 1 // stone_pressure_plate (unpowered)
	GoldBlock          = blockBase("gold_block")               // gold_block
	CryingObsidian     = blockBase("crying_obsidian")          // crying_obsidian
	BlueTerracotta     = blockBase("blue_terracotta")          // blue_terracotta
	ChiseledSandstone  = blockBase("chiseled_sandstone")
	CutSandstone       = blockBase("cut_sandstone")
)

// ---- desert temple ------------------------------------------------------------

// DesertTemple is a sandstone pyramid over a buried, TNT-trapped loot chamber.
type DesertTemple struct {
	X, Y, Z int // base centre, at the surface
	Exists  bool
}

const templeHalf = 6 // pyramid half-width (13×13 base)

// chamberY is the buried loot chamber's floor height.
func (d DesertTemple) chamberY() int { return d.Y - 13 }

// Chests returns the four loot-chest positions on the chamber floor.
func (d DesertTemple) Chests() [4][3]int {
	y := d.chamberY()
	return [4][3]int{{d.X - 2, y, d.Z}, {d.X + 2, y, d.Z}, {d.X, y, d.Z - 2}, {d.X, y, d.Z + 2}}
}

// DesertTempleIn returns the temple whose cell contains (wx,wz), if the roll
// succeeds and the site is dry desert.
func (g *Generator) DesertTempleIn(wx, wz int) DesertTemple {
	ox, oz := cellOrigin(wx, templeCell), cellOrigin(wz, templeCell)
	if hash01(g.seed, ox, oz, 0x7E01) >= 0.5 {
		return DesertTemple{}
	}
	x := ox + 20 + int(hash01(g.seed, ox, oz, 0x7E02)*float64(templeCell-40))
	z := oz + 20 + int(hash01(g.seed, ox, oz, 0x7E03)*float64(templeCell-40))
	if name := g.BiomeName(x, z); name != "minecraft:desert" {
		return DesertTemple{}
	}
	return DesertTemple{X: x, Y: g.Height(x, z), Z: z, Exists: true}
}

func (g *Generator) stampDesertTemples(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(templeCell) {
		d := g.DesertTempleIn(baseX+8+off[0], baseZ+8+off[1])
		if !d.Exists {
			continue
		}
		chest := d.Chests()
		chY := d.chamberY()
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				wx, wz := baseX+lx, baseZ+lz
				dx, dz := wx-d.X, wz-d.Z
				adx, adz := abs(dx), abs(dz)
				// Stepped sandstone pyramid: each Chebyshev ring is one step lower.
				if adx <= templeHalf && adz <= templeHalf {
					ring := adx
					if adz > ring {
						ring = adz
					}
					top := d.Y + (templeHalf - ring)
					for wy := d.Y; wy <= top; wy++ {
						b := Sandstone
						if wy == top && ring%2 == 0 { // orange banding on the steps
							b = OrangeTerracotta
						}
						setSectionBlock(ch, lx, wy, lz, b, true)
					}
				}
				// Buried 5×5×4 loot chamber below the centre.
				if adx <= 2 && adz <= 2 {
					wall := adx == 2 || adz == 2
					for wy := chY - 1; wy <= chY+4; wy++ {
						switch {
						case wy == chY-1: // 3×3 of TNT beneath the pressure plate
							if adx <= 1 && adz <= 1 {
								setSectionBlock(ch, lx, wy, lz, TNTBlock, true)
							} else {
								setSectionBlock(ch, lx, wy, lz, Sandstone, true)
							}
						case wy == chY: // floor: pressure plate over the TNT, tiled otherwise
							switch {
							case dx == 0 && dz == 0:
								setSectionBlock(ch, lx, wy, lz, StonePressurePlate, true)
							case isChest(wx, wy, wz, chest):
								setSectionBlock(ch, lx, wy, lz, ChestNorth, true)
							case (dx+dz)&1 == 0:
								setSectionBlock(ch, lx, wy, lz, BlueTerracotta, true)
							default:
								setSectionBlock(ch, lx, wy, lz, OrangeTerracotta, true)
							}
						case wy == chY+4 || wall: // ceiling + walls
							setSectionBlock(ch, lx, wy, lz, CutSandstone, true)
						default:
							setSectionBlock(ch, lx, wy, lz, Air, true)
						}
					}
				}
			}
		}
	}
}

// ---- ruined portal ------------------------------------------------------------

// RuinedPortal is a broken obsidian portal frame on a scorched netherrack patch
// with a loot chest.
type RuinedPortal struct {
	X, Y, Z                int // frame base (bottom-left of the standing frame)
	ChestX, ChestY, ChestZ int
	Exists                 bool
}

func (g *Generator) RuinedPortalIn(wx, wz int) RuinedPortal {
	ox, oz := cellOrigin(wx, portalCell), cellOrigin(wz, portalCell)
	if hash01(g.seed, ox, oz, 0x9F01) >= 0.4 {
		return RuinedPortal{}
	}
	x := ox + 16 + int(hash01(g.seed, ox, oz, 0x9F02)*float64(portalCell-32))
	z := oz + 16 + int(hash01(g.seed, ox, oz, 0x9F03)*float64(portalCell-32))
	y := g.Height(x, z)
	if y <= SeaLevel { // not underwater
		return RuinedPortal{}
	}
	p := RuinedPortal{X: x, Y: y, Z: z, Exists: true}
	p.ChestX, p.ChestY, p.ChestZ = x+3, y, z
	return p
}

func (g *Generator) stampRuinedPortals(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(portalCell) {
		p := g.RuinedPortalIn(baseX+8+off[0], baseZ+8+off[1])
		if !p.Exists {
			continue
		}
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				wx, wz := baseX+lx, baseZ+lz
				fx, fz := wx-p.X, wz-p.Z // frame is 4 wide along X, in the plane z==0
				// Scorched netherrack apron under and around the frame.
				if fz == 0 && fx >= -1 && fx <= 4 {
					setSectionBlock(ch, lx, p.Y-1, lz, Netherrack, true)
				}
				if fz == 0 && fx >= 0 && fx <= 3 { // the 4-wide × 5-tall frame
					for fy := 0; fy <= 4; fy++ {
						edge := fx == 0 || fx == 3 || fy == 0 || fy == 4
						if !edge {
							continue // portal interior stays open
						}
						// ~1 in 4 frame blocks are broken away (ruined look).
						if hash01(g.seed, wx*7+fy, wz*13, 0x9F10) < 0.25 {
							continue
						}
						b := Obsidian
						if hash01(g.seed, wx+fy, wz, 0x9F11) < 0.3 {
							b = CryingObsidian
						}
						setSectionBlock(ch, lx, p.Y+fy, lz, b, true)
					}
				}
				// A block of gold ore-block and the loot chest beside the portal.
				if wx == p.X+2 && wz == p.Z-1 {
					setSectionBlock(ch, lx, p.Y, lz, GoldBlock, true)
				}
				if wx == p.ChestX && wz == p.ChestZ {
					setSectionBlock(ch, lx, p.ChestY, lz, ChestNorth, true)
				}
			}
		}
	}
}

// ---- shared helpers -----------------------------------------------------------

// cellNeighbours are the nine cell offsets to test so a structure straddling a
// cell boundary still stamps into an adjacent chunk.
func cellNeighbours(cell int) [9][2]int {
	return [9][2]int{{0, 0}, {cell, 0}, {-cell, 0}, {0, cell}, {0, -cell},
		{cell, cell}, {cell, -cell}, {-cell, cell}, {-cell, -cell}}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func isChest(wx, wy, wz int, chests [4][3]int) bool {
	for _, c := range chests {
		if wx == c[0] && wy == c[1] && wz == c[2] {
			return true
		}
	}
	return false
}
