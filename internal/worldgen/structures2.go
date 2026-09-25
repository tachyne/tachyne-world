package worldgen

import "sync"

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
	Mir       int // mirNone or mirFB: vanilla mirrors half of them front-to-back
	Integrity float64
	Chests    [][3]int
	Exists    bool
	Props     portalProps // the biome's setup, rolled (overworld)
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
	k := portalKey{g.seed, ox, oz, g.editsIn != nil} // the guard's answer is part of it
	portalMu.Lock()
	p, ok := portalCache[k]
	portalMu.Unlock()
	if ok {
		return p
	}
	p = g.ruinedPortalAt(ox, oz)
	portalMu.Lock()
	portalCache[k] = p
	portalMu.Unlock()
	return p
}

type portalKey struct {
	seed    int64
	ox, oz  int
	guarded bool
}

var (
	portalCache = map[portalKey]RuinedPortal{}
	portalMu    sync.Mutex
)

func (g *Generator) ruinedPortalAt(ox, oz int) RuinedPortal {
	if hash01(g.seed, ox, oz, 0x9F01) >= 0.4 {
		return RuinedPortal{}
	}
	x := ox + 16 + int(hash01(g.seed, ox, oz, 0x9F02)*float64(portalCell-32))
	z := oz + 16 + int(hash01(g.seed, ox, oz, 0x9F03)*float64(portalCell-32))
	name := ruinedPortalStd[int(hash01(g.seed, ox, oz, 0x9F04)*float64(len(ruinedPortalStd)))]
	if hash01(g.seed, ox, oz, 0x9F05) < 0.05 { // rare giant portal
		name = ruinedPortalGiant[int(hash01(g.seed, ox, oz, 0x9F06)*float64(len(ruinedPortalGiant)))]
	}
	t := TemplateByName(name)
	if t == nil {
		return RuinedPortal{}
	}
	rot := int(hash01(g.seed, ox, oz, 0x9F07)*4) & 3
	mir := mirNone
	if hash01(g.seed, ox, oz, 0x9F0D) < 0.5 {
		mir = mirFB
	}
	// The placed template's footprint: its four bottom corners, and the
	// centre the surface is read at (RuinedPortalStructure.findGenerationPoint
	// reads getBaseHeight at boundingBox.getCenter()).
	minX, maxX, minZ, maxZ := 1<<30, -(1 << 30), 1<<30, -(1 << 30)
	for _, c := range [][2]int{{0, 0}, {t.Size[0] - 1, 0}, {0, t.Size[2] - 1}, {t.Size[0] - 1, t.Size[2] - 1}} {
		rx, _, rz := t.placePos(c[0], 0, c[1], rot, mir)
		minX, maxX = min(minX, x+rx), max(maxX, x+rx)
		minZ, maxZ = min(minZ, z+rz), max(maxZ, z+rz)
	}
	centreX, centreZ := minX+(maxX-minX+1)/2, minZ+(maxZ-minZ+1)/2
	y := g.Height(centreX, centreZ)
	setups := portalSetupsFor(g.BiomeName(centreX, centreZ))
	setup := pickPortalSetup(setups, hash01(g.seed, ox, oz, 0x9F09))
	if y <= SeaLevel && setup.placement != plOceanFloor { // only the ocean's and the swamp's stand under water
		return RuinedPortal{}
	}
	// Vanilla mossiness → integrity in roughly [0.7, 0.9]: a moderately broken
	// frame, not obliterated.
	integ := 0.7 + hash01(g.seed, ox, oz, 0x9F08)*0.2
	_, ySpan, _ := t.rotatedSize(rot)
	props := portalProps{
		airPocket: setup.airPocket == 1 || (setup.airPocket > 0 && hash01(g.seed, ox, oz, 0x9F0A) < setup.airPocket),
		mossiness: setup.mossiness, overgrown: setup.overgrown, vines: setup.vines, placement: setup.placement,
	}
	py := portalY(setup.placement, y-1, ySpan, MinY, hash01(g.seed, ox, oz, 0x9F0B), hash01(g.seed, ox, oz, 0x9F0C))
	// Build guard: a portal that would sink into a player's build stays
	// where it stood before it learned to settle (GenVersion 24).
	if settled := g.portalSettle(py, setup.placement, [4][2]int{{minX, minZ}, {maxX, minZ}, {minX, maxZ}, {maxX, maxZ}}); settled == py ||
		!g.builtIn(minX, settled, minZ, maxX, py+ySpan-1, maxZ) {
		py = settled
	}
	props.cold = setup.canBeCold && g.coldEnoughToSnow(g.BiomeName(x, z), x, py, z)
	p := RuinedPortal{X: x, Y: py, Z: z, Tmpl: name, Rot: rot, Mir: mir, Integrity: integ, Exists: true, Props: props}
	for _, c := range t.Chests {
		rx, ry, rz := t.placePos(c[0], c[1], c[2], rot, mir)
		p.Chests = append(p.Chests, [3]int{p.X + rx, p.Y + ry, p.Z + rz})
	}
	return p
}

// portalSettle is findSuitableY's descent: from the placement's height the
// portal goes down until three of its four bottom corners stand in solid
// base terrain — anything but air, or, on the ocean floor, anything that
// blocks motion — so a portal on a cliff edge sinks into the cliff rather
// than hanging off it.
func (g *Generator) portalSettle(y, placement int, corners [4][2]int) int {
	var cols [4]column
	for i, c := range corners {
		cols[i] = g.columnAt(c[0], c[1])
	}
	solid := func(b uint32) bool {
		if placement == plOceanFloor { // OCEAN_FLOOR_WG: MATERIAL_MOTION_BLOCKING
			return b != Air && !IsFluid(b)
		}
		return b != Air // WORLD_SURFACE_WG: NOT_AIR
	}
	for ; y > MinY+15; y-- {
		n := 0
		for i, c := range corners {
			if solid(g.terrainCell(cols[i], c[0], y, c[1])) {
				if n++; n == 3 {
					return y
				}
			}
		}
	}
	return y
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
	mir := mirNone
	if hash01(g.seed, ox, oz, 0x9F19) < 0.5 {
		mir = mirFB
	}
	p := RuinedPortal{X: x, Y: y - 1, Z: z, Tmpl: name, Rot: rot, Mir: mir, Integrity: integ, Exists: true}
	for _, c := range t.Chests {
		rx, ry, rz := t.placePos(c[0], c[1], c[2], rot, mir)
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
			g.stampRuinedPortalVariant(ch, cx, cz, p, t) // the biome's setup, aged, on its netherrack
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
