package worldgen

import (
	"strings"
	"sync"
)

// Structure sites in a vanilla-generator world: each structure query the
// engine's structure code asks (VillageIn, DesertTempleIn, …) answers from
// the vanilla placement (vanillastructs.go) — the structure set's grid
// cell, its one chunk, the set's pick and the biome check — and the engine
// builds the structure there. A native world never reaches this file: the
// queries test vanillaPlacerOf first, which is nil without the vanilla
// generator.

// vpSiteKey is one grid cell of one structure set.
type vpSiteKey struct {
	set    string
	gx, gz int32
}

type vpSite struct {
	st VanillaStart
	ok bool
}

var (
	vpSiteMu    sync.Mutex
	vpSiteCache = map[*vanillaPlacer]map[vpSiteKey]vpSite{}
)

// siteIn is the start of the set's grid cell holding block (wx, wz), if
// the cell has one.
func (p *vanillaPlacer) siteIn(set string, wx, wz int) (VanillaStart, bool) {
	s := p.structs.set(set)
	if s == nil || s.Type != "random_spread" {
		return VanillaStart{}, false
	}
	gx, gz := vpFloorDiv(int32(wx>>4), s.Spacing), vpFloorDiv(int32(wz>>4), s.Spacing)
	k := vpSiteKey{set, gx, gz}
	vpSiteMu.Lock()
	m := vpSiteCache[p]
	if m == nil {
		m = map[vpSiteKey]vpSite{}
		vpSiteCache[p] = m
	}
	if v, ok := m[k]; ok {
		vpSiteMu.Unlock()
		return v.st, v.ok
	}
	vpSiteMu.Unlock()
	cx, cz := p.structs.potentialChunk(s, gx*s.Spacing, gz*s.Spacing)
	st, ok := p.structs.SetStartIn(s, cx, cz)
	vpSiteMu.Lock()
	if len(m) > 1<<16 {
		m = map[vpSiteKey]vpSite{}
		vpSiteCache[p] = m
	}
	m[k] = vpSite{st, ok}
	vpSiteMu.Unlock()
	return st, ok
}

// startsNear lists the set's starts whose grid cells come within reach
// blocks of chunk (cx, cz): what a chunk must stamp of the set.
func (p *vanillaPlacer) startsNear(set string, cx, cz int32, reach int) []VanillaStart {
	s := p.structs.set(set)
	if s == nil {
		return nil
	}
	cell := int(s.Spacing) * 16
	x0, z0 := int(cx)*16-reach, int(cz)*16-reach
	x1, z1 := int(cx)*16+15+reach, int(cz)*16+15+reach
	var out []VanillaStart
	for gx := floorDiv(x0, cell); gx <= floorDiv(x1, cell); gx++ {
		for gz := floorDiv(z0, cell); gz <= floorDiv(z1, cell); gz++ {
			if st, ok := p.siteIn(set, gx*cell, gz*cell); ok {
				out = append(out, st)
			}
		}
	}
	return out
}

// ---- the structure queries ---------------------------------------------------

func (g *Generator) vanillaVillage(vp *vanillaPlacer, wx, wz int) Village {
	st, ok := vp.siteIn("villages", wx, wz)
	if !ok {
		return Village{}
	}
	return Village{X: st.X, Z: st.Z, Y: g.Height(st.X, st.Z),
		Variant: strings.TrimPrefix(st.Structure, "village_"), Exists: true}
}

func (g *Generator) vanillaDesertTemple(vp *vanillaPlacer, wx, wz int) DesertTemple {
	st, ok := vp.siteIn("desert_pyramids", wx, wz)
	if !ok {
		return DesertTemple{}
	}
	x, z := int(st.ChunkX)*16, int(st.ChunkZ)*16
	lowest := 1 << 30
	for dx := 0; dx < templeWidth; dx++ {
		for dz := 0; dz < templeDepth; dz++ {
			if h := g.Height(x+dx, z+dz); h < lowest {
				lowest = h
			}
		}
	}
	r := newJigsawRNG(g.seed, x^0x7E000000, z)
	d := DesertTemple{X: x, Z: z, Dir: r.intn(4), Exists: true}
	d.Y = lowest - r.intn(3)
	d.Sus = g.templeSuspiciousSand(d, r)
	return d
}

func (g *Generator) vanillaJungleTemple(vp *vanillaPlacer, wx, wz int) JungleTemple {
	st, ok := vp.siteIn("jungle_temples", wx, wz)
	if !ok {
		return JungleTemple{}
	}
	x, z := int(st.ChunkX)*16, int(st.ChunkZ)*16
	r := newJigsawRNG(g.seed, x^0x3A000000, z)
	p := scatteredPiece{X: x, Z: z, W: jungleTempleWidth, D: jungleTempleDepth, Dir: r.intn(4), Exists: true}
	p.Y = g.averageGround(p)
	return JungleTemple{p}
}

func (g *Generator) vanillaSwampHut(vp *vanillaPlacer, wx, wz int) SwampHut {
	st, ok := vp.siteIn("swamp_huts", wx, wz)
	if !ok {
		return SwampHut{}
	}
	x, z := int(st.ChunkX)*16, int(st.ChunkZ)*16
	r := newJigsawRNG(g.seed, x^0x5A000000, z)
	p := scatteredPiece{X: x, Z: z, W: swampHutWidth, D: swampHutDepth, Dir: r.intn(4), Exists: true}
	p.Y = g.averageGround(p)
	return SwampHut{p}
}

// vanillaIgloo is IglooStructure's start: the chunk's corner, the
// rotation and the basement (and its depth) drawn as IglooPieces draws
// them from the structure's random.
func (g *Generator) vanillaIgloo(vp *vanillaPlacer, wx, wz int) Igloo {
	st, ok := vp.siteIn("igloos", wx, wz)
	if !ok {
		return Igloo{}
	}
	r := newVWLegacy(0)
	r.setLargeFeatureSeed(g.seed, st.ChunkX, st.ChunkZ)
	ig := Igloo{X: int(st.ChunkX) * 16, Z: int(st.ChunkZ) * 16, Exists: true}
	ig.Rot = int(r.nextIntN(4))
	if r.nextDouble() < 0.5 {
		ig.Basement = true
		ig.Depth = int(r.nextIntN(8)) + 4
	}
	ex, ez := rotAboutPivot(3, 0, ig.Rot, iglooTopPivot)
	ig.Y = g.Height(ig.X+ex, ig.Z+ez) - 1
	ig.setChest()
	return ig
}

func (g *Generator) vanillaOutpost(vp *vanillaPlacer, wx, wz int) PillagerOutpost {
	st, ok := vp.siteIn("pillager_outposts", wx, wz)
	if !ok {
		return PillagerOutpost{}
	}
	return PillagerOutpost{X: st.X, Y: g.Height(st.X, st.Z), Z: st.Z, Exists: true}
}

func (g *Generator) vanillaMansion(vp *vanillaPlacer, wx, wz int) Mansion {
	st, ok := vp.siteIn("woodland_mansions", wx, wz)
	if !ok {
		return Mansion{}
	}
	return Mansion{X: st.X, Y: g.Height(st.X, st.Z), Z: st.Z, Exists: true}
}

func (g *Generator) vanillaMonument(vp *vanillaPlacer, wx, wz int) Monument {
	st, ok := vp.siteIn("ocean_monuments", wx, wz)
	if !ok {
		return Monument{}
	}
	return Monument{X: st.X, Y: g.Height(st.X, st.Z), Z: st.Z, Exists: true}
}

func (g *Generator) vanillaAncientCity(vp *vanillaPlacer, wx, wz int) AncientCity {
	st, ok := vp.siteIn("ancient_cities", wx, wz)
	if !ok {
		return AncientCity{}
	}
	return AncientCity{X: st.X, Y: ancientCityY, Z: st.Z, Exists: true}
}

func (g *Generator) vanillaTrialChamber(vp *vanillaPlacer, wx, wz int) TrialChamber {
	st, ok := vp.siteIn("trial_chambers", wx, wz)
	if !ok {
		return TrialChamber{}
	}
	return TrialChamber{X: st.X, Y: st.Y, Z: st.Z, Exists: true}
}

func (g *Generator) vanillaTrailRuins(vp *vanillaPlacer, wx, wz int) TrailRuins {
	st, ok := vp.siteIn("trail_ruins", wx, wz)
	if !ok {
		return TrailRuins{}
	}
	return TrailRuins{X: st.X, Y: g.Height(st.X+2, st.Z+2) + trailRuinsDepth, Z: st.Z, Exists: true}
}

func (g *Generator) vanillaAbandonedCamp(vp *vanillaPlacer, wx, wz int) AbandonedCamp {
	st, ok := vp.siteIn("abandoned_camp", wx, wz)
	if !ok {
		return AbandonedCamp{}
	}
	return AbandonedCamp{X: st.X, Y: g.Height(st.X, st.Z) - 1, Z: st.Z,
		Biome: strings.TrimPrefix(st.Structure, "abandoned_camp_"), Exists: true}
}

func (g *Generator) vanillaOceanRuins(vp *vanillaPlacer, wx, wz int) OceanRuins {
	st, ok := vp.siteIn("ocean_ruins", wx, wz)
	if !ok {
		return OceanRuins{}
	}
	return g.oceanRuinSite(st.X, st.Z, st.Structure == "ocean_ruin_warm")
}

func (g *Generator) vanillaShipwreck(vp *vanillaPlacer, wx, wz int, beached bool) Shipwreck {
	st, ok := vp.siteIn("shipwrecks", wx, wz)
	if !ok || (st.Structure == "shipwreck_beached") != beached {
		return Shipwreck{}
	}
	x, z := int(st.ChunkX)*16, int(st.ChunkZ)*16
	r := newJigsawRNG(g.seed, x^0x5A0E0000, z)
	list := shipwreckTemplates
	if beached {
		list = beachedTemplates
	}
	name := list[r.intn(len(list))]
	t := TemplateByName(name)
	if t == nil {
		return Shipwreck{}
	}
	rot := r.intn(4)
	if !beached {
		return shipwreckAt(t, x, g.Height(st.X, st.Z), z, name, rot)
	}
	fx, fz := t.Size[0], t.Size[2]
	if rot&1 == 1 {
		fx, fz = fz, fx
	}
	low := 1 << 30
	for dx := 0; dx < fx; dx++ {
		for dz := 0; dz < fz; dz++ {
			if h := maxInt(g.Height(x+dx, z+dz), SeaLevel); h < low {
				low = h
			}
		}
	}
	return shipwreckAt(t, x, low-t.Size[1]/2-r.intn(3), z, name, rot)
}

func (g *Generator) vanillaBuriedTreasure(vp *vanillaPlacer, wx, wz int) BuriedTreasure {
	st, ok := vp.siteIn("buried_treasures", wx, wz)
	if !ok {
		return BuriedTreasure{}
	}
	// BuriedTreasurePieces: the chest at the chunk's (9, 9), sunk under the
	// sand.
	x, z := int(st.ChunkX)*16+9, int(st.ChunkZ)*16+9
	return BuriedTreasure{X: x, Y: g.Height(x, z) - 3, Z: z, Exists: true}
}

func (g *Generator) vanillaRuinedPortal(vp *vanillaPlacer, wx, wz int) RuinedPortal {
	st, ok := vp.siteIn("ruined_portals", wx, wz)
	if !ok || st.Structure == "ruined_portal_nether" {
		return RuinedPortal{}
	}
	x, z := int(st.ChunkX)*16, int(st.ChunkZ)*16
	return g.ruinedPortalFrom(x, z, x, z, nil)
}

// vanillaMineshaftAt is the mineshaft starting in chunk (cx, cz): the set's
// chunk (every chunk is a cell; the frequency thins them) and its pick.
func (g *Generator) vanillaMineshaftAt(vp *vanillaPlacer, cx, cz int) Mineshaft {
	s := vp.structs.set("mineshafts")
	if s == nil {
		return Mineshaft{}
	}
	st, ok := vp.structs.SetStartIn(s, int32(cx), int32(cz))
	if !ok {
		return Mineshaft{}
	}
	mesa := st.Structure == "mineshaft_mesa"
	pieces := g.assembleMineshaft(cx, cz, mesa)
	room := pieces[0]
	return Mineshaft{X: room.box.x0, Y: room.box.y0, Z: room.box.z0, Mesa: mesa, Exists: true, pieces: pieces}
}

// vanillaStronghold is the stronghold whose ring position falls in the
// engine's stronghold cell holding (wx, wz).
func (g *Generator) vanillaStronghold(vp *vanillaPlacer, wx, wz int) Stronghold {
	ox, oz := cellOrigin(wx, strongholdCell), cellOrigin(wz, strongholdCell)
	set := vp.structs.set("strongholds")
	if set == nil {
		return Stronghold{}
	}
	half := int32(strongholdCell / 32)
	for _, c := range vp.structs.ringsNear(set, int32((ox+strongholdCell/2)>>4), int32((oz+strongholdCell/2)>>4), half) {
		bx, bz := int(c[0])*16, int(c[1])*16
		if cellOrigin(bx, strongholdCell) != ox || cellOrigin(bz, strongholdCell) != oz {
			continue
		}
		if !vp.structs.d.Structures["stronghold"].Biomes[vp.noiseBiome(bx>>2, 0, bz>>2)] {
			continue
		}
		if sh, ok := g.strongholdFrom(int(c[0]), int(c[1])); ok {
			return sh
		}
	}
	return Stronghold{}
}

// ---- stamping -------------------------------------------------------------------

// stampVanillaStructures is stampStructures in a vanilla-generator world:
// the engine's structure builders on the vanilla sites. Structures whose
// builders look only at the chunk's own cell are stamped here from every
// start whose cell comes near (a vanilla start sits anywhere in its cell,
// so its pieces cross into the next one); the engine's own lakes and old
// dungeons are not vanilla and stay out.
func (g *Generator) stampVanillaStructures(ch *Chunk, cx, cz int32) {
	vp := g.vanillaPlacerOf()
	if vp == nil {
		return
	}
	g.adaptTerrain(ch, cx, cz)
	g.stampMineshafts(ch, cx, cz)
	g.stampVillages(ch, cx, cz)
	g.stampStrongholds(ch, cx, cz)
	g.stampDesertTemples(ch, cx, cz)
	g.stampRuinedPortals(ch, cx, cz)
	g.stampOutposts(ch, cx, cz)
	for _, st := range vp.startsNear("ancient_cities", cx, cz, 128) {
		g.StampPieces(ch, cx, cz, g.AssembleAncientCity(AncientCity{X: st.X, Y: ancientCityY, Z: st.Z, Exists: true}))
	}
	g.stampTrailRuins(ch, cx, cz)
	g.stampAbandonedCamps(ch, cx, cz)
	for _, st := range vp.startsNear("trial_chambers", cx, cz, 128) {
		g.StampPieces(ch, cx, cz, g.AssembleTrialChamber(TrialChamber{X: st.X, Y: st.Y, Z: st.Z, Exists: true}))
	}
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, st := range vp.startsNear("shipwrecks", cx, cz, 32) {
		s := g.vanillaShipwreck(vp, int(st.ChunkX)*16, int(st.ChunkZ)*16, st.Structure == "shipwreck_beached")
		t := TemplateByName(s.Tmpl)
		if !s.Exists || t == nil {
			continue
		}
		if st.Structure != "shipwreck_beached" {
			t.StampTemplate(ch, cx, cz, s.X, s.Y, s.Z, s.Rot)
			continue
		}
		fx, fz := t.Size[0], t.Size[2]
		if s.Rot&1 == 1 {
			fx, fz = fz, fx
		}
		if s.X > baseX+15 || s.X+fx-1 < baseX || s.Z > baseZ+15 || s.Z+fz-1 < baseZ ||
			g.builtIn(s.X, s.Y, s.Z, s.X+fx-1, s.Y+t.Size[1], s.Z+fz-1) {
			continue
		}
		t.StampTemplateProcSus(ch, cx, cz, s.X, s.Y, s.Z, s.Rot, nil, true, nil)
	}
	g.stampOceanRuins(ch, cx, cz)
	if b := g.vanillaBuriedTreasure(vp, baseX, baseZ); b.Exists {
		setSectionBlock(ch, b.X-baseX, b.Y, b.Z-baseZ, ChestNorth, true)
	}
	for _, st := range vp.startsNear("ocean_monuments", cx, cz, 64) {
		g.stampMonumentAt(ch, cx, cz, Monument{X: st.X, Y: g.Height(st.X, st.Z), Z: st.Z, Exists: true})
	}
	for _, st := range vp.startsNear("igloos", cx, cz, 16) {
		g.stampIglooAt(ch, cx, cz, g.vanillaIgloo(vp, int(st.ChunkX)*16, int(st.ChunkZ)*16))
	}
	for _, st := range vp.startsNear("woodland_mansions", cx, cz, 96) {
		m := Mansion{X: st.X, Y: g.Height(st.X, st.Z), Z: st.Z, Exists: true}
		for _, pc := range g.AssembleMansion(m) {
			if t := TemplateByName("woodland_mansion/" + pc.tmpl); t != nil {
				t.StampAt(ch, cx, cz, pc.pos[0], pc.pos[1], pc.pos[2], pc.rot, pc.mir)
			}
		}
	}
	g.stampJungleTemples(ch, cx, cz)
	g.stampSwampHuts(ch, cx, cz)
}

// stampIglooAt stamps an igloo's part in this chunk (stampIgloo's body for
// a given igloo).
func (g *Generator) stampIglooAt(ch *Chunk, cx, cz int32, ig Igloo) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	if !ig.Exists || TemplateByName("igloo/top") == nil {
		return
	}
	for _, p := range ig.pieces() {
		if t := TemplateByName(p.name); t != nil {
			t.StampAt(ch, cx, cz, p.px, p.py, p.pz, ig.Rot, mirNone)
		}
	}
	if !ig.Basement {
		tx, tz := ig.X+iglooTopPivot[0], ig.Z+iglooTopPivot[1]
		if lx, lz := tx-baseX, tz-baseZ; lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
			if below := sectionBlockAt(ch, lx, ig.Y-1, lz); below != Air && !isLadder(below) {
				setSectionBlock(ch, lx, ig.Y, lz, SnowBlock, true)
			}
		}
	}
}

// vanillaLocateCell is the grid a structure id's vanilla set is laid on,
// in blocks (0: not a random-spread set of this dimension).
func (vp *vanillaPlacer) locateCell(name string) int {
	for _, s := range vp.structs.sets {
		for _, e := range s.Structures {
			if e.Name == name || strings.HasPrefix(e.Name, name+"_") || name == s.Name {
				if s.Type == "random_spread" {
					return int(s.Spacing) * 16
				}
			}
		}
	}
	return 0
}
