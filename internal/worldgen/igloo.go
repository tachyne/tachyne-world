package worldgen

import "strings"

// Igloos — the first structure placed from REAL vanilla NBT templates
// (igloo/top + optional basement via igloo/bottom & igloo/middle ladder). The
// assembly offsets and odds match the vanilla generator's values, so the room
// is byte-for-byte the vanilla layout.
//
// Each igloo faces one of the four rotations, drawn per igloo as vanilla's
// IglooStructure does, every piece turned about its own pivot (IglooPieces'
// PIVOTS) so the trapdoor, the ladder shaft and the basement's ladder stay
// on one column whichever way it faces. The igloo stands on the ground at
// that column (vanilla's entrance height).
//
// Until GenVersion 26 every igloo faced rotation 0 and stood on the ground at
// its corner. An igloo a player has touched — a build block or a dug-out
// cell anywhere in either layout's box, by the build guard's frozen edits —
// keeps that old layout, so nobody's looted or remodelled igloo turns under
// them.

const (
	iglooCell = 320
	iglooOdds = 0.4 // of snowy cells
)

// Igloo is a placed igloo (or the zero value). Basement pieces + chest are set
// when the 50 % basement roll succeeds.
type Igloo struct {
	X, Y, Z                int // the structure's position (the top piece's corner before rotation; Y = its floor)
	Rot                    int // clockwise quarter turns
	Legacy                 bool
	Basement               bool
	Depth                  int // ladder depth 4..11
	ChestX, ChestY, ChestZ int
	Exists                 bool
}

func isSnowyBiome(name string) bool {
	return strings.Contains(name, "snowy") || strings.Contains(name, "frozen") ||
		name == "minecraft:grove" || name == "minecraft:ice_spikes"
}

// The igloo pieces' pivots and offsets from the structure position
// (IglooPieces' PIVOTS and OFFSETS), x and z only: rotation is about y.
var (
	iglooTopPivot    = [2]int{3, 5}
	iglooMiddlePivot = [2]int{1, 1}
	iglooBottomPivot = [2]int{3, 7}
	iglooMiddleOff   = [3]int{2, -3, 4}
	iglooBottomOff   = [3]int{0, -3, -2}
	iglooChestLocal  = [3]int{1, 1, 6} // in igloo/bottom
)

// rotAboutPivot is StructureTemplate.calculateRelativePosition for a y
// rotation: a template-local (x, z) turned about the pivot.
func rotAboutPivot(x, z, rot int, pivot [2]int) (int, int) {
	tx, _, tz := transformPos(x-pivot[0], 0, z-pivot[1], rot, mirNone)
	return pivot[0] + tx, pivot[1] + tz
}

// iglooPiece is one placed template: StampAt's origin for it, so that its
// local cells land at origin + transformPos(local).
type iglooPiece struct {
	name       string
	px, py, pz int
}

// pieceAt places a piece whose unrotated corner is at (x, y, z), turned
// about its pivot.
func iglooPieceAt(name string, x, y, z, rot int, pivot [2]int) iglooPiece {
	// origin + R(local) == corner + pivot + R(local - pivot)
	tx, _, tz := transformPos(pivot[0], 0, pivot[1], rot, mirNone)
	return iglooPiece{name, x + pivot[0] - tx, y, z + pivot[1] - tz}
}

// pieces lists the igloo's placed templates, top first.
func (ig Igloo) pieces() []iglooPiece {
	ps := []iglooPiece{iglooPieceAt("igloo/top", ig.X, ig.Y, ig.Z, ig.Rot, iglooTopPivot)}
	if !ig.Basement {
		return ps
	}
	o := iglooBottomOff
	ps = append(ps, iglooPieceAt("igloo/bottom", ig.X+o[0], ig.Y+o[1]-ig.Depth*3, ig.Z+o[2], ig.Rot, iglooBottomPivot))
	o = iglooMiddleOff
	for i := 0; i < ig.Depth-1; i++ {
		ps = append(ps, iglooPieceAt("igloo/middle", ig.X+o[0], ig.Y+o[1]-i*3, ig.Z+o[2], ig.Rot, iglooMiddlePivot))
	}
	return ps
}

// box is the igloo's bounding box over all its pieces, inclusive.
func (ig Igloo) box() (x0, y0, z0, x1, y1, z1 int) {
	first := true
	for _, p := range ig.pieces() {
		t := TemplateByName(p.name)
		if t == nil {
			continue
		}
		for _, c := range [4][2]int{{0, 0}, {t.Size[0] - 1, 0}, {0, t.Size[2] - 1}, {t.Size[0] - 1, t.Size[2] - 1}} {
			tx, _, tz := transformPos(c[0], 0, c[1], ig.Rot, mirNone)
			x, z := p.px+tx, p.pz+tz
			if first {
				x0, x1, z0, z1, y0, y1 = x, x, z, z, p.py, p.py+t.Size[1]-1
				first = false
			}
			x0, x1, z0, z1 = min(x0, x), max(x1, x), min(z0, z), max(z1, z)
			y0, y1 = min(y0, p.py), max(y1, p.py+t.Size[1]-1)
		}
	}
	return
}

// setChest fills in the basement chest's world position.
func (ig *Igloo) setChest() {
	if !ig.Basement {
		return
	}
	o := iglooBottomOff
	p := iglooPieceAt("igloo/bottom", ig.X+o[0], ig.Y+o[1]-ig.Depth*3, ig.Z+o[2], ig.Rot, iglooBottomPivot)
	tx, ty, tz := transformPos(iglooChestLocal[0], iglooChestLocal[1], iglooChestLocal[2], ig.Rot, mirNone)
	ig.ChestX, ig.ChestY, ig.ChestZ = p.px+tx, p.py+ty, p.pz+tz
}

// IglooIn returns the igloo owning (wx,wz)'s cell, on snowy land.
func (g *Generator) IglooIn(wx, wz int) Igloo {
	ox, oz := cellOrigin(wx, iglooCell), cellOrigin(wz, iglooCell)
	if hash01(g.seed, ox, oz, 0x1600) >= iglooOdds {
		return Igloo{}
	}
	x := ox + 32 + int(hash01(g.seed, ox, oz, 0x1601)*float64(iglooCell-64))
	z := oz + 32 + int(hash01(g.seed, ox, oz, 0x1602)*float64(iglooCell-64))
	if !isSnowyBiome(g.BiomeName(x, z)) {
		return Igloo{}
	}
	surf := g.Height(x, z)
	if surf < SeaLevel || surf > 130 {
		return Igloo{}
	}
	legacy := Igloo{X: x, Y: surf - 1, Z: z, Exists: true, Legacy: true} // floor rests on the ground
	// IglooPieces.addPieces: 50% basement, depth = nextInt(8)+4.
	if hash01(g.seed, ox, oz, 0x1603) < 0.5 {
		legacy.Basement = true
		legacy.Depth = 4 + int(hash01(g.seed, ox, oz, 0x1604)*8)
	}
	legacy.setChest()
	// IglooStructure: Rotation.getRandom. IglooPiece.postProcess stands
	// every piece on the ground at the entrance column, the top piece's
	// (3, 0, 0) turned about its pivot.
	ig := legacy
	ig.Legacy = false
	ig.Rot = int(hash01(g.seed, ox, oz, 0x1605) * 4)
	ex, ez := rotAboutPivot(3, 0, ig.Rot, iglooTopPivot)
	ig.Y = g.Height(x+ex, z+ez) - 1
	ig.setChest()
	if ig.Rot == legacy.Rot && ig.Y == legacy.Y {
		return ig
	}
	if g.touchedIn(legacy.box()) || g.touchedIn(ig.box()) {
		return legacy
	}
	return ig
}

// stampIgloo stamps the igloo pieces overlapping this chunk from real templates.
func (g *Generator) stampIgloo(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	ig := g.IglooIn(baseX+8, baseZ+8)
	if !ig.Exists || TemplateByName("igloo/top") == nil {
		return
	}
	for _, p := range ig.pieces() {
		if t := TemplateByName(p.name); t != nil {
			t.StampAt(ch, cx, cz, p.px, p.py, p.pz, ig.Rot, mirNone)
		}
	}
	// IglooPiece.postProcess: with nothing but ground under the trapdoor
	// (no basement), a snow block closes the floor instead. The trapdoor
	// sits on the top piece's pivot column whatever the rotation.
	if !ig.Legacy && !ig.Basement {
		tx, tz := ig.X+iglooTopPivot[0], ig.Z+iglooTopPivot[1]
		if lx, lz := tx-baseX, tz-baseZ; lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
			if below := sectionBlockAt(ch, lx, ig.Y-1, lz); below != Air && !isLadder(below) {
				setSectionBlock(ch, lx, ig.Y, lz, SnowBlock, true)
			}
		}
	}
}

// isLadder reports a ladder in any facing.
func isLadder(s uint32) bool {
	lo, hi := BlockRange("ladder")
	return s >= lo && s <= hi
}
