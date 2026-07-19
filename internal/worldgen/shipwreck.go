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
