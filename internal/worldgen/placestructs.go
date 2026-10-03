package worldgen

import "strings"

// /place structure for the structures the engine builds in code rather
// than from a jigsaw: each is laid out from the chunk the command names
// (StructureStart at that chunk, as PlaceCommand.placeStructure starts it,
// with every biome accepted) and stamped chunk by chunk the way generation
// stamps it. The result carries what generation also leaves there: the
// loot containers with their tables, the chest minecarts, the spawners'
// mobs, and the mobs the structure's pieces spawn (a swamp hut's witch and
// cat, a monument's elders, a mansion's illagers, an ocean ruin's drowned).

// StructureStamp is a code-built structure /place lays out.
type StructureStamp struct {
	X0, Z0, X1, Z1 int // the block columns it reaches (inclusive)
	// Stamp writes the structure's blocks that fall in one chunk.
	Stamp    func(ch *Chunk, cx, cz int32)
	Chests   []LootChest        // chest-like containers and their tables
	Bins     []LootChest        // dispensers and their tables (a jungle temple's traps)
	Carts    []LootChest        // chest minecarts, standing on a rail there
	Spawners []StructureSpawner // monster spawners and the mob each spawns
	Mobs     []StructureMob     // the mobs the structure's generation places
}

// StructureSpawner is a monster spawner a structure placed.
type StructureSpawner struct {
	X, Y, Z int
	Entity  string // entity type, namespaced
}

// StructureMob is a mob a structure's generation places, in its block.
type StructureMob struct {
	X, Y, Z int
	Entity  string // entity type without namespace ("elytra_frame": the end ship's item frame)
	Rot     int    // the piece's rotation (the elytra frame's facing)
}

// PlaceStampNames are the structures PlaceStructureStamp lays out.
var PlaceStampNames = []string{
	"swamp_hut", "desert_pyramid", "jungle_pyramid", "shipwreck", "shipwreck_beached", "buried_treasure",
	"ruined_portal", "ruined_portal_desert", "ruined_portal_jungle", "ruined_portal_mountain",
	"ruined_portal_ocean", "ruined_portal_swamp", "ruined_portal_nether", "ocean_ruin_cold", "ocean_ruin_warm",
	"mansion", "monument", "fortress", "stronghold", "mineshaft", "mineshaft_mesa",
}

// portalVariantSetups are the ruined portal structures' setups by id.
var portalVariantSetups = map[string][]portalSetup{
	"ruined_portal": portalStandard, "ruined_portal_desert": portalDesert, "ruined_portal_jungle": portalJungle,
	"ruined_portal_mountain": portalMountain, "ruined_portal_ocean": portalOcean, "ruined_portal_swamp": portalSwamp,
}

// PlaceStructureStamp lays out a code-built structure (vanilla's id,
// namespace optional) from the chunk holding (x,z). ok is false for a
// structure it does not build, or one whose own placement rules refuse the
// spot (a desert or jungle temple below sea level, a mansion below y=60, a
// Nether portal with no floor).
func (g *Generator) PlaceStructureStamp(name string, x, z int) (StructureStamp, bool) {
	name = trimNS(name)
	bx, bz := x>>4<<4, z>>4<<4
	switch name {
	case "swamp_hut":
		r := newJigsawRNG(g.seed, bx^0x5A000000, bz)
		p := scatteredPiece{X: bx, Z: bz, W: swampHutWidth, D: swampHutDepth, Dir: r.intn(4), Exists: true}
		p.Y = g.averageGround(p)
		st := scatteredStamp(p, func(s *pieceStamp) { s.buildSwampHut() })
		hx, hy, hz := SwampHut{p}.Home()
		st.Mobs = []StructureMob{{X: hx, Y: hy, Z: hz, Entity: "witch"}, {X: hx, Y: hy, Z: hz, Entity: "cat"}}
		return st, true
	case "jungle_pyramid":
		if g.lowestGround(bx, bz, jungleTempleWidth, jungleTempleDepth) < SeaLevel {
			return StructureStamp{}, false // SinglePieceStructure: not below sea level
		}
		r := newJigsawRNG(g.seed, bx^0x3A000000, bz)
		p := scatteredPiece{X: bx, Z: bz, W: jungleTempleWidth, D: jungleTempleDepth, Dir: r.intn(4), Exists: true}
		p.Y = g.averageGround(p)
		t := JungleTemple{p}
		st := scatteredStamp(p, func(s *pieceStamp) {
			rr := newJigsawRNG(g.seed, t.X^0x3A100000, t.Z)
			s.buildJungleTemple(func() uint32 { // MossStoneSelector
				if rr.intn(10) < 4 {
					return cobblestone
				}
				return mossyCobblestone
			})
		})
		for _, c := range t.Chests() {
			st.Chests = append(st.Chests, LootChest{c[0], c[1], c[2], "chests/jungle_temple"})
		}
		for _, d := range t.Dispensers() {
			st.Bins = append(st.Bins, LootChest{d[0], d[1], d[2], "chests/jungle_temple_dispenser"})
		}
		return st, true
	case "desert_pyramid":
		lowest := g.lowestGround(bx, bz, templeWidth, templeDepth)
		if lowest < SeaLevel {
			return StructureStamp{}, false
		}
		r := newJigsawRNG(g.seed, bx^0x7E000000, bz)
		d := DesertTemple{X: bx, Z: bz, Dir: r.intn(4), Exists: true}
		d.Y = lowest - r.intn(3)
		d.Sus = g.templeSuspiciousSand(d, r)
		st := StructureStamp{X0: d.X, Z0: d.Z, X1: d.X + templeWidth - 1, Z1: d.Z + templeDepth - 1}
		st.Stamp = func(ch *Chunk, cx, cz int32) {
			baseX, baseZ := int(cx)*16, int(cz)*16
			if d.X >= baseX+16 || d.X+templeWidth <= baseX || d.Z >= baseZ+16 || d.Z+templeDepth <= baseZ {
				return
			}
			s := &templeStamp{g: g, d: d, ch: ch, baseX: baseX, baseZ: baseZ, sus: map[[3]int]bool{}}
			for _, c := range d.Sus {
				s.sus[c] = true
			}
			s.build()
		}
		for _, c := range d.Chests() {
			st.Chests = append(st.Chests, LootChest{c[0], c[1], c[2], "chests/desert_pyramid"})
		}
		return st, true
	case "shipwreck":
		tn := shipwreckTemplates[int(hash01(g.seed, bx, bz, 0x5A03)*float64(len(shipwreckTemplates)))]
		t := TemplateByName(tn)
		if t == nil {
			return StructureStamp{}, false
		}
		s := shipwreckAt(t, bx, g.Height(bx, bz), bz, tn, int(hash01(g.seed, bx, bz, 0x5A04)*4)&3)
		st := templateStamp(t, s.X, s.Z, s.Rot, func(ch *Chunk, cx, cz int32) {
			t.StampTemplate(ch, cx, cz, s.X, s.Y, s.Z, s.Rot)
		})
		st.Chests = shipChests(s.Chests)
		return st, true
	case "shipwreck_beached":
		tn := beachedTemplates[int(hash01(g.seed, bx, bz, 0x5B03)*float64(len(beachedTemplates)))]
		t := TemplateByName(tn)
		if t == nil {
			return StructureStamp{}, false
		}
		rot := int(hash01(g.seed, bx, bz, 0x5B04)*4) & 3
		fx, _, fz := t.rotatedSize(rot)
		low := 1 << 30
		for dx := 0; dx < fx; dx++ {
			for dz := 0; dz < fz; dz++ {
				low = min(low, max(g.Height(bx+dx, bz+dz), SeaLevel))
			}
		}
		y := low - t.Size[1]/2 - int(hash01(g.seed, bx, bz, 0x5B05)*3)
		s := shipwreckAt(t, bx, y, bz, tn, rot)
		st := templateStamp(t, s.X, s.Z, s.Rot, func(ch *Chunk, cx, cz int32) {
			t.StampTemplateProcSus(ch, cx, cz, s.X, s.Y, s.Z, s.Rot, nil, true, nil)
		})
		st.Chests = shipChests(s.Chests)
		return st, true
	case "buried_treasure":
		b := BuriedTreasure{X: bx + 9, Y: g.Height(bx+9, bz+9) - 3, Z: bz + 9, Exists: true}
		st := StructureStamp{X0: b.X, Z0: b.Z, X1: b.X, Z1: b.Z}
		st.Stamp = func(ch *Chunk, cx, cz int32) {
			if lx, lz := b.X-int(cx)*16, b.Z-int(cz)*16; lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
				setSectionBlock(ch, lx, b.Y, lz, ChestNorth, true)
			}
		}
		st.Chests = []LootChest{{b.X, b.Y, b.Z, "chests/buried_treasure"}}
		return st, true
	case "ruined_portal_nether":
		p := g.ruinedPortalNetherFrom(bx, bz, bx, bz)
		t := TemplateByName(p.Tmpl)
		if !p.Exists || t == nil {
			return StructureStamp{}, false
		}
		st := portalStamp(t, p, func(ch *Chunk, cx, cz int32) {
			t.StampTemplateRotRemap(ch, cx, cz, p.X, p.Y, p.Z, p.Rot, g.seed, p.Integrity, blackstoneRemap)
		})
		return st, true
	case "ocean_ruin_cold", "ocean_ruin_warm":
		site := g.oceanRuinSite(bx, bz, name == "ocean_ruin_warm")
		if len(site.Pieces) == 0 {
			return StructureStamp{}, false
		}
		st := StructureStamp{X0: 1 << 30, Z0: 1 << 30, X1: -(1 << 30), Z1: -(1 << 30)}
		for _, p := range site.Pieces {
			t := TemplateByName(p.Tmpl)
			if t == nil {
				continue
			}
			sx, _, sz := t.rotatedSize(p.Rot)
			st.X0, st.Z0 = min(st.X0, p.X), min(st.Z0, p.Z)
			st.X1, st.Z1 = max(st.X1, p.X+sx-1), max(st.Z1, p.Z+sz-1)
			st.Chests = append(st.Chests, shipChests(p.Chests)...)
			for _, d := range p.Drowned {
				st.Mobs = append(st.Mobs, StructureMob{X: d[0], Y: d[1], Z: d[2], Entity: "drowned"})
			}
		}
		if st.X0 > st.X1 {
			return StructureStamp{}, false
		}
		st.Stamp = func(ch *Chunk, cx, cz int32) {
			for i := range site.Pieces {
				g.stampRuinPiece(ch, cx, cz, site.Warm, &site.Pieces[i])
			}
		}
		return st, true
	case "mansion":
		// getLowestYIn5by5BoxOffset7Blocks: the lowest of the box's corners,
		// and no mansion below y=60.
		y := 1 << 30
		for _, d := range [4][2]int{{7, 7}, {12, 7}, {7, 12}, {12, 12}} {
			y = min(y, g.Height(bx+d[0], bz+d[1]))
		}
		if y < 60 {
			return StructureStamp{}, false
		}
		m := Mansion{X: bx + 7, Y: y, Z: bz + 7, Exists: true}
		pieces := g.AssembleMansion(m)
		if len(pieces) == 0 {
			return StructureStamp{}, false
		}
		st := StructureStamp{X0: 1 << 30, Z0: 1 << 30, X1: -(1 << 30), Z1: -(1 << 30)}
		for _, pc := range pieces {
			t := TemplateByName("woodland_mansion/" + pc.tmpl)
			if t == nil {
				continue
			}
			for _, c := range [4][2]int{{0, 0}, {t.Size[0] - 1, 0}, {0, t.Size[2] - 1}, {t.Size[0] - 1, t.Size[2] - 1}} {
				tx, _, tz := transformPos(c[0], 0, c[1], pc.rot, pc.mir)
				st.X0, st.Z0 = min(st.X0, pc.pos[0]+tx), min(st.Z0, pc.pos[2]+tz)
				st.X1, st.Z1 = max(st.X1, pc.pos[0]+tx), max(st.Z1, pc.pos[2]+tz)
			}
		}
		if st.X0 > st.X1 {
			return StructureStamp{}, false
		}
		st.Stamp = func(ch *Chunk, cx, cz int32) {
			for _, pc := range pieces {
				if t := TemplateByName("woodland_mansion/" + pc.tmpl); t != nil {
					t.StampAt(ch, cx, cz, pc.pos[0], pc.pos[1], pc.pos[2], pc.rot, pc.mir)
				}
			}
		}
		for _, c := range g.MansionChests(m) {
			st.Chests = append(st.Chests, LootChest{c[0], c[1], c[2], "chests/woodland_mansion"})
		}
		illager := [3]string{"evoker", "vindicator", "allay"}
		for _, s := range g.MansionMobs(m) {
			if s.Type >= 0 && s.Type < len(illager) {
				st.Mobs = append(st.Mobs, StructureMob{X: s.X, Y: s.Y, Z: s.Z, Entity: illager[s.Type]})
			}
		}
		return st, true
	case "monument":
		m := Monument{X: bx + 8, Y: g.Height(bx+8, bz+8), Z: bz + 8, Exists: true}
		reach := monumentHalf + 6 // the building, and the moat round it
		st := StructureStamp{X0: m.X - reach, Z0: m.Z - reach, X1: m.X + reach, Z1: m.Z + reach}
		st.Stamp = func(ch *Chunk, cx, cz int32) { g.stampMonumentAt(ch, cx, cz, m) }
		for _, p := range g.MonumentElders(m) {
			st.Mobs = append(st.Mobs, StructureMob{X: p[0], Y: p[1], Z: p[2], Entity: "elder_guardian"})
		}
		return st, true
	case "fortress":
		f := Fortress{X: bx + 2, Z: bz + 2, Exists: true}
		pieces := g.assembleFortress(f)
		if len(pieces) == 0 {
			return StructureStamp{}, false
		}
		b := pieces[0].box
		for _, p := range pieces {
			b = b.union(p.box)
		}
		st := StructureStamp{X0: b.x0, Z0: b.z0, X1: b.x1, Z1: b.z1}
		st.Stamp = func(ch *Chunk, cx, cz int32) {
			chunk := fbox{int(cx) * 16, MinY, int(cz) * 16, int(cx)*16 + 15, MinY + len(ch.Sections)*16 - 1, int(cz)*16 + 15}
			s := &fstamp{ch: ch, chunk: chunk}
			for _, p := range pieces {
				if p.box.intersects(chunk) {
					s.p = p
					s.postProcess()
				}
			}
		}
		for _, c := range g.FortressChests(f) {
			st.Chests = append(st.Chests, LootChest{c[0], c[1], c[2], "chests/nether_bridge"})
		}
		for _, c := range g.FortressSpawners(f) {
			st.Spawners = append(st.Spawners, StructureSpawner{c[0], c[1], c[2], "minecraft:blaze"})
		}
		return st, true
	case "stronghold":
		sh, ok := g.strongholdFrom(bx>>4, bz>>4)
		if !ok {
			return StructureStamp{}, false
		}
		x0, _, z0, x1, _, z1 := sh.Bounds()
		st := StructureStamp{X0: x0, Z0: z0, X1: x1, Z1: z1}
		st.Stamp = func(ch *Chunk, cx, cz int32) {
			s := newChunkStamp(g, ch, cx, cz)
			for i, p := range sh.pieces {
				if !p.box.intersects(s.cb) {
					continue
				}
				s.p, s.salt = &p.opiece, 0x5700+uint64(i)*0x9E37
				s.drawStronghold(p)
			}
			s.reshape()
		}
		for _, c := range sh.Chests() {
			st.Chests = append(st.Chests, LootChest{c.X, c.Y, c.Z, c.Table})
		}
		if p, ok := sh.Spawner(); ok {
			st.Spawners = append(st.Spawners, StructureSpawner{p[0], p[1], p[2], "minecraft:silverfish"})
		}
		return st, true
	case "mineshaft", "mineshaft_mesa":
		pieces := g.assembleMineshaft(bx>>4, bz>>4, name == "mineshaft_mesa")
		if len(pieces) == 0 {
			return StructureStamp{}, false
		}
		room := pieces[0]
		m := Mineshaft{X: room.box.x0, Y: room.box.y0, Z: room.box.z0, Mesa: name == "mineshaft_mesa", Exists: true, pieces: pieces}
		b := pieces[0].box
		for _, p := range pieces {
			b = b.union(p.box)
		}
		st := StructureStamp{X0: b.x0, Z0: b.z0, X1: b.x1, Z1: b.z1}
		st.Stamp = func(ch *Chunk, cx, cz int32) {
			s := newChunkStamp(g, ch, cx, cz)
			g.stampMineshaftIn(s, m)
			s.keep = nil
			s.reshape()
		}
		for _, p := range g.MineshaftSpawners(m) {
			st.Spawners = append(st.Spawners, StructureSpawner{p[0], p[1], p[2], "minecraft:cave_spider"})
		}
		for _, p := range g.MineshaftCarts(m) {
			st.Carts = append(st.Carts, LootChest{p[0], p[1], p[2], MineshaftCartTable})
		}
		return st, true
	}
	if setups, ok := portalVariantSetups[name]; ok {
		p := g.ruinedPortalFrom(bx, bz, bx, bz, setups)
		t := TemplateByName(p.Tmpl)
		if !p.Exists || t == nil {
			return StructureStamp{}, false
		}
		st := portalStamp(t, p, func(ch *Chunk, cx, cz int32) { g.stampRuinedPortalVariant(ch, cx, cz, p, t) })
		return st, true
	}
	return StructureStamp{}, false
}

// lowestGround is SinglePieceStructure.getLowestY: the lowest surface of a
// footprint's four corners from (x,z).
func (g *Generator) lowestGround(x, z, w, d int) int {
	return min(min(g.Height(x, z), g.Height(x+w-1, z)), min(g.Height(x, z+d-1), g.Height(x+w-1, z+d-1))) // the package's min takes two
}

// scatteredStamp stamps one scattered piece wherever its footprint reaches.
func scatteredStamp(p scatteredPiece, build func(s *pieceStamp)) StructureStamp {
	sx, sz := p.W, p.D
	if p.Dir >= 2 {
		sx, sz = p.D, p.W
	}
	return StructureStamp{X0: p.X, Z0: p.Z, X1: p.X + sx - 1, Z1: p.Z + sz - 1,
		Stamp: func(ch *Chunk, cx, cz int32) {
			baseX, baseZ := int(cx)*16, int(cz)*16
			if !p.overlaps(baseX, baseZ) {
				return
			}
			build(&pieceStamp{p: p, ch: ch, baseX: baseX, baseZ: baseZ})
		}}
}

// templateStamp is one rotated template's footprint from its min corner.
func templateStamp(t *Template, x, z, rot int, stamp func(ch *Chunk, cx, cz int32)) StructureStamp {
	sx, _, sz := t.rotatedSize(rot)
	return StructureStamp{X0: x, Z0: z, X1: x + sx - 1, Z1: z + sz - 1, Stamp: stamp}
}

// portalStamp is a ruined portal's footprint (its processors reach a few
// blocks past the template, so the bounds are widened), and its chests.
func portalStamp(t *Template, p RuinedPortal, stamp func(ch *Chunk, cx, cz int32)) StructureStamp {
	st := StructureStamp{X0: 1 << 30, Z0: 1 << 30, X1: -(1 << 30), Z1: -(1 << 30), Stamp: stamp}
	for _, c := range [4][2]int{{0, 0}, {t.Size[0] - 1, 0}, {0, t.Size[2] - 1}, {t.Size[0] - 1, t.Size[2] - 1}} {
		rx, _, rz := t.placePos(c[0], 0, c[1], p.Rot, p.Mir)
		st.X0, st.Z0 = min(st.X0, p.X+rx-4), min(st.Z0, p.Z+rz-4)
		st.X1, st.Z1 = max(st.X1, p.X+rx+4), max(st.Z1, p.Z+rz+4)
	}
	for _, c := range p.Chests {
		st.Chests = append(st.Chests, LootChest{c[0], c[1], c[2], "chests/ruined_portal"})
	}
	return st
}

func shipChests(cs []ShipChest) []LootChest {
	out := make([]LootChest, len(cs))
	for i, c := range cs {
		out[i] = LootChest{c.X, c.Y, c.Z, c.Table}
	}
	return out
}

// strongholdFrom is the stronghold whose stairs start in chunk (cx,cz),
// found by its portal room.
func (g *Generator) strongholdFrom(cx, cz int) (Stronghold, bool) {
	pieces := g.strongholdPieces(cx, cz)
	st := Stronghold{Exists: true, pieces: pieces, LocX: cx << 4, LocZ: cz << 4}
	for _, p := range pieces {
		if p.kind == shPortalRoom {
			st.room = p
			c := p.world(5, 3, 10)
			st.X, st.Y, st.Z = c[0], c[1], c[2]
			return st, true
		}
	}
	return Stronghold{}, false
}

// PieceMobs are the entities a set of jigsaw pieces' templates carry (an
// igloo's villager and zombie villager, an outpost's caged golem and
// allays), at their blocks, by entity name, with the template's villager
// data, age and persistence.
func PieceMobs(pieces []PlacedPiece) []VillageMob {
	var out []VillageMob
	seen := map[string]int{}
	for i := range pieces {
		p := &pieces[i]
		if p.Tmpl == nil || p.TerrainMatch {
			continue
		}
		for _, m := range p.Tmpl.Mobs {
			w := wp(p, m.Pos[0], m.Pos[1], m.Pos[2])
			y := float64(w[1])
			if len(m.At) == 3 {
				y = float64(p.OY) + m.At[1]
			}
			collar := -1
			if m.Collar != nil {
				collar = *m.Collar
			}
			vm := VillageMob{
				Type: strings.TrimPrefix(m.Type, "minecraft:"), X: float64(w[0]) + 0.5, Y: y, Z: float64(w[2]) + 0.5,
				BX: w[0], BY: w[1], BZ: w[2],
				Prof: m.Prof, VType: m.VType, Level: m.Level, Age: m.Age, Persist: m.Persist, Collar: collar,
			}
			base := vm.Key()
			vm.N = seen[base]
			seen[base]++
			out = append(out, vm)
		}
	}
	return out
}
