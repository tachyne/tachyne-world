package worldgen

// Ruined portals (desert temples moved to deserttemple.go), following the
// query+stamp pattern (a seed-deterministic XIn() query and a stampX() that
// writes the parts falling inside a chunk). Server fills the chests on first
// open.

const portalCell = 208 // one ruined portal per ~208-block cell

var (
	GoldBlock      = blockBase("gold_block")      // gold_block
	CryingObsidian = blockBase("crying_obsidian") // crying_obsidian
)

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

// RuinedPortalNetherIn is the Nether variant (RuinedPortalStructure's
// "nether" setup: the same templates, no mossiness, an air pocket, and the
// BlackstoneReplaceProcessor), standing on a cavern floor above the lava sea.
func (g *Generator) RuinedPortalNetherIn(wx, wz int) RuinedPortal {
	ox, oz := cellOrigin(wx, portalCell), cellOrigin(wz, portalCell)
	if hash01(g.seed, ox, oz, 0x9F11) >= 0.4 {
		return RuinedPortal{}
	}
	x := ox + 16 + int(hash01(g.seed, ox, oz, 0x9F12)*float64(portalCell-32))
	z := oz + 16 + int(hash01(g.seed, ox, oz, 0x9F13)*float64(portalCell-32))
	y, ok := g.netherFloorOK(x, z)
	if !ok {
		return RuinedPortal{}
	}
	name := ruinedPortalStd[int(hash01(g.seed, ox, oz, 0x9F14)*float64(len(ruinedPortalStd)))]
	if hash01(g.seed, ox, oz, 0x9F15) < 0.05 {
		name = ruinedPortalGiant[int(hash01(g.seed, ox, oz, 0x9F16)*float64(len(ruinedPortalGiant)))]
	}
	t := TemplateByName(name)
	if t == nil {
		return RuinedPortal{}
	}
	rot := int(hash01(g.seed, ox, oz, 0x9F17)*4) & 3
	integ := 0.7 + hash01(g.seed, ox, oz, 0x9F18)*0.2
	p := RuinedPortal{X: x, Y: y - 1, Z: z, Tmpl: name, Rot: rot, Integrity: integ, Exists: true}
	for _, c := range t.Chests {
		rx, ry, rz := t.rotatePos(c[0], c[1], c[2], rot)
		p.Chests = append(p.Chests, [3]int{p.X + rx, p.Y + ry, p.Z + rz})
	}
	return p
}

// blackstoneRemap is BlackstoneReplaceProcessor: the stone-brick and stone
// family of a portal's masonry becomes its polished-blackstone counterpart,
// keeping each block's orientation (same property layout, base swap).
var blackstoneRemap = func() func(uint32) uint32 {
	pairs := [][2]string{
		{"stone_bricks", "polished_blackstone_bricks"}, {"mossy_stone_bricks", "polished_blackstone_bricks"},
		{"cracked_stone_bricks", "cracked_polished_blackstone_bricks"}, {"chiseled_stone_bricks", "chiseled_polished_blackstone"},
		{"stone_brick_stairs", "polished_blackstone_brick_stairs"}, {"mossy_stone_brick_stairs", "polished_blackstone_brick_stairs"},
		{"stone_brick_slab", "polished_blackstone_brick_slab"}, {"mossy_stone_brick_slab", "polished_blackstone_brick_slab"},
		{"stone_brick_wall", "polished_blackstone_brick_wall"}, {"mossy_stone_brick_wall", "polished_blackstone_brick_wall"},
		{"stone", "blackstone"}, {"stone_slab", "blackstone_slab"}, {"stone_stairs", "blackstone_stairs"},
	}
	type span struct{ lo, hi, to uint32 }
	var spans []span
	for _, pr := range pairs {
		lo, hi, ok := BlockRangeOK(pr[0])
		to, _, ok2 := BlockRangeOK(pr[1])
		if ok && ok2 {
			spans = append(spans, span{lo, hi, to})
		}
	}
	return func(state uint32) uint32 {
		for _, sp := range spans {
			if state >= sp.lo && state <= sp.hi {
				return sp.to + (state - sp.lo)
			}
		}
		return state
	}
}()

// stampNetherPortals stamps the Nether variant into a nether chunk.
func (g *Generator) stampNetherPortals(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(portalCell) {
		p := g.RuinedPortalNetherIn(baseX+8+off[0], baseZ+8+off[1])
		if !p.Exists {
			continue
		}
		if t := TemplateByName(p.Tmpl); t != nil {
			t.StampTemplateRotRemap(ch, cx, cz, p.X, p.Y, p.Z, p.Rot, g.seed, p.Integrity, blackstoneRemap)
		}
	}
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
