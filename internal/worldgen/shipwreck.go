package worldgen

// Shipwrecks and buried treasure. The wreck is stamped from a REAL vanilla
// shipwreck template (one of the 20 rightsideup/sideways/upsidedown ± mast ±
// degraded variants) at a random rotation, resting on the ocean floor with its
// supply/map/treasure chests carrying the template's own loot markers. Buried
// treasure is a single chest sunk under a beach (vanilla has no template for
// it — it is literally one chest). Loot routes to the real vanilla tables in
// the server (structloot.go).

const (
	shipwreckCell = 320
	shipwreckOdds = 0.5
	buriedCell    = 288
	buriedOdds    = 0.5
)

// shipwreckTemplates are the ocean wreck variants (all 20 baked forms).
var shipwreckTemplates = []string{
	"shipwreck/with_mast", "shipwreck/with_mast_degraded",
	"shipwreck/sideways_full", "shipwreck/sideways_full_degraded",
	"shipwreck/sideways_fronthalf", "shipwreck/sideways_fronthalf_degraded",
	"shipwreck/sideways_backhalf", "shipwreck/sideways_backhalf_degraded",
	"shipwreck/rightsideup_full", "shipwreck/rightsideup_full_degraded",
	"shipwreck/rightsideup_fronthalf", "shipwreck/rightsideup_fronthalf_degraded",
	"shipwreck/rightsideup_backhalf", "shipwreck/rightsideup_backhalf_degraded",
	"shipwreck/upsidedown_full", "shipwreck/upsidedown_full_degraded",
	"shipwreck/upsidedown_fronthalf", "shipwreck/upsidedown_fronthalf_degraded",
	"shipwreck/upsidedown_backhalf", "shipwreck/upsidedown_backhalf_degraded",
}

// ShipChest is a placed wreck chest with the loot table its template marker set.
type ShipChest struct {
	X, Y, Z int
	Table   string
}

// Shipwreck is a placed wreck (or the zero value): a template name + rotation
// stamped with its min corner at (X,Y,Z) on the ocean floor.
type Shipwreck struct {
	X, Y, Z int // min-corner origin; Y is the seafloor it rests on
	Tmpl    string
	Rot     int
	Chests  []ShipChest
	Exists  bool
}

// ShipwreckIn returns the wreck owning the cell containing (wx,wz), if the site
// is submerged ocean floor.
func (g *Generator) ShipwreckIn(wx, wz int) Shipwreck {
	ox, oz := cellOrigin(wx, shipwreckCell), cellOrigin(wz, shipwreckCell)
	if hash01(g.seed, ox, oz, 0x5A00) >= shipwreckOdds {
		return Shipwreck{}
	}
	x := ox + 32 + int(hash01(g.seed, ox, oz, 0x5A01)*float64(shipwreckCell-64))
	z := oz + 32 + int(hash01(g.seed, ox, oz, 0x5A02)*float64(shipwreckCell-64))
	floor := g.Height(x, z)
	if floor >= SeaLevel-2 || floor < 35 { // must sit under a few blocks of water
		return Shipwreck{}
	}
	name := shipwreckTemplates[int(hash01(g.seed, ox, oz, 0x5A03)*float64(len(shipwreckTemplates)))]
	t := TemplateByName(name)
	if t == nil {
		return Shipwreck{}
	}
	rot := int(hash01(g.seed, ox, oz, 0x5A04)*4) & 3
	s := Shipwreck{X: x, Y: floor, Z: z, Tmpl: name, Rot: rot, Exists: true}
	for i, c := range t.Chests {
		rx, ry, rz := t.rotatePos(c[0], c[1], c[2], rot)
		tbl := "chests/shipwreck_supply"
		if i < len(t.ChestLoot) && t.ChestLoot[i] != "" {
			tbl = t.ChestLoot[i]
		}
		s.Chests = append(s.Chests, ShipChest{x + rx, floor + ry, z + rz, tbl})
	}
	return s
}

// stampShipwreck stamps the wreck template overlapping this chunk.
func (g *Generator) stampShipwreck(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	// A wreck can straddle chunk borders; the 32-block cell margin keeps it in
	// its own cell, so the chunk-centre cell is the only owner to check.
	s := g.ShipwreckIn(baseX+8, baseZ+8)
	if !s.Exists {
		return
	}
	if t := TemplateByName(s.Tmpl); t != nil {
		t.StampTemplate(ch, cx, cz, s.X, s.Y, s.Z, s.Rot)
	}
}

// shipwreck_beached: half the wrecks of vanilla's shipwrecks set lie on a
// beach instead of the sea floor — upright or on their side, never upside
// down, sunk to half their height (and up to two more) under the lowest
// ground of their footprint, the sand left standing inside the hull. They
// have their own grid, so no submerged wreck moved when they arrived.
const (
	beachedCell = 384
	beachedOdds = 0.5
	// beachedTries is how many points of a cell are tried for a beach: the
	// beaches are thin, and one point would almost never land on one.
	beachedTries = 24
)

// beachedTemplates are ShipwreckPieces' beached variants.
var beachedTemplates = []string{
	"shipwreck/with_mast", "shipwreck/sideways_full", "shipwreck/sideways_fronthalf",
	"shipwreck/sideways_backhalf", "shipwreck/rightsideup_full", "shipwreck/rightsideup_fronthalf",
	"shipwreck/rightsideup_backhalf", "shipwreck/with_mast_degraded", "shipwreck/rightsideup_full_degraded",
	"shipwreck/rightsideup_fronthalf_degraded", "shipwreck/rightsideup_backhalf_degraded",
}

func isBeach(name string) bool { return name == "minecraft:beach" || name == "minecraft:snowy_beach" }

// BeachedShipwreckIn returns the beached wreck owning the cell containing
// (wx, wz), if the cell's site is a beach.
func (g *Generator) BeachedShipwreckIn(wx, wz int) Shipwreck {
	ox, oz := cellOrigin(wx, beachedCell), cellOrigin(wz, beachedCell)
	if hash01(g.seed, ox, oz, 0x5B00) >= beachedOdds {
		return Shipwreck{}
	}
	name := beachedTemplates[int(hash01(g.seed, ox, oz, 0x5B03)*float64(len(beachedTemplates)))]
	t := TemplateByName(name)
	if t == nil {
		return Shipwreck{}
	}
	rot := int(hash01(g.seed, ox, oz, 0x5B04)*4) & 3
	fx, fz := t.Size[0], t.Size[2]
	if rot&1 == 1 {
		fx, fz = fz, fx
	}
	for i := 0; i < beachedTries; i++ {
		x := ox + 32 + int(hash01(g.seed, ox+i, oz, 0x5B01)*float64(beachedCell-64))
		z := oz + 32 + int(hash01(g.seed, ox, oz+i, 0x5B02)*float64(beachedCell-64))
		if !isBeach(g.resolveBiome(x, z).Name) {
			continue
		}
		// WORLD_SURFACE_WG over the footprint: the first cell above the
		// ground or the water, whichever is higher; the wreck sinks from the
		// lowest.
		low := 1 << 30
		for dx := 0; dx < fx; dx++ {
			for dz := 0; dz < fz; dz++ {
				if h := maxInt(g.Height(x+dx, z+dz), SeaLevel); h < low {
					low = h
				}
			}
		}
		y := low - t.Size[1]/2 - int(hash01(g.seed, ox, oz, 0x5B05)*3)
		s := Shipwreck{X: x, Y: y, Z: z, Tmpl: name, Rot: rot, Exists: true}
		for j, c := range t.Chests {
			rx, ry, rz := t.rotatePos(c[0], c[1], c[2], rot)
			tbl := "chests/shipwreck_supply"
			if j < len(t.ChestLoot) && t.ChestLoot[j] != "" {
				tbl = t.ChestLoot[j]
			}
			s.Chests = append(s.Chests, ShipChest{x + rx, y + ry, z + rz, tbl})
		}
		return s
	}
	return Shipwreck{}
}

// stampBeachedShipwreck stamps the beached wreck overlapping this chunk,
// leaving the template's air out (the hull stays full of sand), unless a
// player has built in its box.
func (g *Generator) stampBeachedShipwreck(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	s := g.BeachedShipwreckIn(baseX+8, baseZ+8)
	if !s.Exists {
		return
	}
	t := TemplateByName(s.Tmpl)
	if t == nil {
		return
	}
	fx, fz := t.Size[0], t.Size[2]
	if s.Rot&1 == 1 {
		fx, fz = fz, fx
	}
	if s.X > baseX+15 || s.X+fx-1 < baseX || s.Z > baseZ+15 || s.Z+fz-1 < baseZ {
		return
	}
	if g.builtIn(s.X, s.Y, s.Z, s.X+fx-1, s.Y+t.Size[1], s.Z+fz-1) {
		return
	}
	t.StampTemplateProcSus(ch, cx, cz, s.X, s.Y, s.Z, s.Rot, nil, true, nil)
}

// BuriedTreasure is a single beach-buried chest.
type BuriedTreasure struct {
	X, Y, Z int
	Exists  bool
}

// BuriedTreasureIn returns the buried chest owning (wx,wz)'s cell, on beaches.
func (g *Generator) BuriedTreasureIn(wx, wz int) BuriedTreasure {
	ox, oz := cellOrigin(wx, buriedCell), cellOrigin(wz, buriedCell)
	if hash01(g.seed, ox, oz, 0xB700) >= buriedOdds {
		return BuriedTreasure{}
	}
	x := ox + 32 + int(hash01(g.seed, ox, oz, 0xB701)*float64(buriedCell-64))
	z := oz + 32 + int(hash01(g.seed, ox, oz, 0xB702)*float64(buriedCell-64))
	surf := g.Height(x, z)
	if surf < SeaLevel-1 || surf > SeaLevel+2 { // beach band only
		return BuriedTreasure{}
	}
	return BuriedTreasure{X: x, Y: surf - 3, Z: z, Exists: true}
}

// stampBuriedTreasure sinks the chest (surrounded by sand) into the beach.
func (g *Generator) stampBuriedTreasure(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	b := g.BuriedTreasureIn(baseX+8, baseZ+8)
	if !b.Exists {
		return
	}
	setSectionBlock(ch, b.X-baseX, b.Y, b.Z-baseZ, ChestNorth, true)
}

// NearestShipwreck finds the closest wreck to (wx, wz) within radius blocks
// (vanilla's dolphin looks 50 chunks), scanning the placement cells around
// the point. ok=false when none lies within reach.
func (g *Generator) NearestShipwreck(wx, wz, radius int) (x, z int, ok bool) {
	bestD := radius * radius
	cells := radius/shipwreckCell + 1
	ox, oz := cellOrigin(wx, shipwreckCell), cellOrigin(wz, shipwreckCell)
	for cx := -cells; cx <= cells; cx++ {
		for cz := -cells; cz <= cells; cz++ {
			s := g.ShipwreckIn(ox+cx*shipwreckCell, oz+cz*shipwreckCell)
			if !s.Exists {
				continue
			}
			dx, dz := s.X-wx, s.Z-wz
			if d := dx*dx + dz*dz; d < bestD {
				x, z, ok, bestD = s.X, s.Z, true, d
			}
		}
	}
	// #shipwreck holds the beached wrecks too.
	cells = radius/beachedCell + 1
	ox, oz = cellOrigin(wx, beachedCell), cellOrigin(wz, beachedCell)
	for cx := -cells; cx <= cells; cx++ {
		for cz := -cells; cz <= cells; cz++ {
			s := g.BeachedShipwreckIn(ox+cx*beachedCell, oz+cz*beachedCell)
			if !s.Exists {
				continue
			}
			dx, dz := s.X-wx, s.Z-wz
			if d := dx*dx + dz*dz; d < bestD {
				x, z, ok, bestD = s.X, s.Z, true, d
			}
		}
	}
	return x, z, ok
}
