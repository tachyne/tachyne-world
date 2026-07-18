package worldgen

// Shipwrecks and buried treasure. A compact plank hull rests on the ocean floor
// with supply/treasure/map chests; buried treasure is a single chest sunk under
// a beach. Faithful-feel stand-ins (not template ports); loot routes to the real
// vanilla tables in the server (structloot.go).

const (
	shipwreckCell = 320
	shipwreckOdds = 0.5
	buriedCell    = 288
	buriedOdds    = 0.5

	shipLen  = 9 // along X
	shipWide = 4 // along Z
	shipTall = 3
)

// Shipwreck is a placed wreck (or the zero value). Chests holds up to 3 chest
// cells as {x,y,z,kind} with kind 0=supply 1=treasure 2=map.
type Shipwreck struct {
	X, Y, Z int // min-corner origin; Y is the seafloor surface it rests on
	Chests  [3][4]int
	N       int
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
	s := Shipwreck{X: x, Y: floor, Z: z, Exists: true}
	// Three chests down the centreline at deck level.
	cz := z + shipWide/2
	for i, cx := range []int{x + 1, x + shipLen/2, x + shipLen - 2} {
		s.Chests[i] = [4]int{cx, floor + 2, cz, i}
	}
	s.N = 3
	return s
}

// stampShipwreck stamps the hull portion overlapping this chunk.
func (g *Generator) stampShipwreck(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	// A wreck can straddle chunk borders; the 32-block cell margin keeps it in
	// its own cell, so the chunk-centre cell is the only owner to check.
	s := g.ShipwreckIn(baseX+8, baseZ+8)
	if !s.Exists {
		return
	}
	planks := OakPlanks
	log := blockBase("oak_log")
	for dx := 0; dx < shipLen; dx++ {
		for dz := 0; dz < shipWide; dz++ {
			wx, wz := s.X+dx, s.Z+dz
			lx, lz := wx-baseX, wz-baseZ
			edge := dx == 0 || dx == shipLen-1 || dz == 0 || dz == shipWide-1
			for dy := 1; dy <= shipTall; dy++ {
				wy := s.Y + dy
				switch {
				case dy == 1: // hull bottom
					setSectionBlock(ch, lx, wy, lz, planks, true)
				case edge: // side/end walls
					setSectionBlock(ch, lx, wy, lz, planks, true)
				default: // hollow the cabin (swim-through)
					setSectionBlock(ch, lx, wy, lz, Air, true)
				}
			}
		}
	}
	// A short mast amidships, rooted on the hull bottom so it isn't culled as a
	// floating fragment.
	mx, mz := s.X+shipLen/2, s.Z+shipWide/2
	for dy := 1; dy <= shipTall+3; dy++ {
		setSectionBlock(ch, mx-baseX, s.Y+dy, mz-baseZ, log, true)
	}
	// Chests on the deck floor.
	for i := 0; i < s.N; i++ {
		c := s.Chests[i]
		setSectionBlock(ch, c[0]-baseX, c[1], c[2]-baseZ, ChestNorth, true)
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
