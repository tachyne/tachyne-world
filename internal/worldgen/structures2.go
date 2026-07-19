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

// RuinedPortal is a broken portal placed from a REAL vanilla ruined_portal
// template (10 standard + 3 giant variants), rotated, with the vanilla
// BlockRotProcessor decay (integrity) applied at stamp time and its own loot
// chest.
type RuinedPortal struct {
	X, Y, Z   int // template min corner (Y = surface it settles on)
	Tmpl      string
	Rot       int
	Integrity float64
	Chests    [][3]int
	Exists    bool
}

// ruinedPortalTemplates: the standard portals are common; the giant portals are
// the rare (~5 %) big variant, matching vanilla's weighting.
var ruinedPortalStd = []string{
	"ruined_portal/portal_1", "ruined_portal/portal_2", "ruined_portal/portal_3",
	"ruined_portal/portal_4", "ruined_portal/portal_5", "ruined_portal/portal_6",
	"ruined_portal/portal_7", "ruined_portal/portal_8", "ruined_portal/portal_9",
	"ruined_portal/portal_10",
}
var ruinedPortalGiant = []string{
	"ruined_portal/giant_portal_1", "ruined_portal/giant_portal_2", "ruined_portal/giant_portal_3",
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
	name := ruinedPortalStd[int(hash01(g.seed, ox, oz, 0x9F04)*float64(len(ruinedPortalStd)))]
	if hash01(g.seed, ox, oz, 0x9F05) < 0.05 { // rare giant portal
		name = ruinedPortalGiant[int(hash01(g.seed, ox, oz, 0x9F06)*float64(len(ruinedPortalGiant)))]
	}
	t := TemplateByName(name)
	if t == nil {
		return RuinedPortal{}
	}
	rot := int(hash01(g.seed, ox, oz, 0x9F07)*4) & 3
	// Vanilla mossiness → integrity in roughly [0.7, 0.9]: a moderately broken
	// frame, not obliterated.
	integ := 0.7 + hash01(g.seed, ox, oz, 0x9F08)*0.2
	p := RuinedPortal{X: x, Y: y - 1, Z: z, Tmpl: name, Rot: rot, Integrity: integ, Exists: true}
	for _, c := range t.Chests {
		rx, ry, rz := t.rotatePos(c[0], c[1], c[2], rot)
		p.Chests = append(p.Chests, [3]int{p.X + rx, p.Y + ry, p.Z + rz})
	}
	return p
}

func (g *Generator) stampRuinedPortals(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(portalCell) {
		p := g.RuinedPortalIn(baseX+8+off[0], baseZ+8+off[1])
		if !p.Exists {
			continue
		}
		if t := TemplateByName(p.Tmpl); t != nil {
			t.StampTemplateRot(ch, cx, cz, p.X, p.Y, p.Z, p.Rot, g.seed, p.Integrity)
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
