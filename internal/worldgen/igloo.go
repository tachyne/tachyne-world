package worldgen

import "strings"

// Igloos — the first structure placed from REAL vanilla NBT templates
// (igloo/top + optional basement via igloo/bottom & igloo/middle ladder). The
// assembly offsets and odds are the decompiled IglooPieces values, so the room
// is byte-for-byte the vanilla layout. Rotation is fixed at 0 for now (pivot-
// based multi-piece rotation lands with the jigsaw work).

const (
	iglooCell = 320
	iglooOdds = 0.4 // of snowy cells
)

// Igloo is a placed igloo (or the zero value). Basement pieces + chest are set
// when the 50 % basement roll succeeds.
type Igloo struct {
	X, Y, Z                int // top-piece min corner (Y = surface)
	Basement               bool
	Depth                  int // ladder depth 4..11
	ChestX, ChestY, ChestZ int
	Exists                 bool
}

func isSnowyBiome(name string) bool {
	return strings.Contains(name, "snowy") || strings.Contains(name, "frozen") ||
		name == "minecraft:grove" || name == "minecraft:ice_spikes"
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
	ig := Igloo{X: x, Y: surf - 1, Z: z, Exists: true} // floor rests on the ground
	// IglooPieces.addPieces: 50% basement, depth = nextInt(8)+4.
	if hash01(g.seed, ox, oz, 0x1603) < 0.5 {
		ig.Basement = true
		ig.Depth = 4 + int(hash01(g.seed, ox, oz, 0x1604)*8)
		// LABORATORY offset (0,-3,-2) below depth*3; chest local [1,1,6].
		bx, by, bz := ig.X+0, ig.Y-3-ig.Depth*3, ig.Z-2
		ig.ChestX, ig.ChestY, ig.ChestZ = bx+1, by+1, bz+6
	}
	return ig
}

// stampIgloo stamps the igloo pieces overlapping this chunk from real templates.
func (g *Generator) stampIgloo(ch *Chunk, cx, cz int32) {
	top := TemplateByName("igloo/top")
	if top == nil {
		return
	}
	baseX, baseZ := int(cx)*16, int(cz)*16
	ig := g.IglooIn(baseX+8, baseZ+8)
	if !ig.Exists {
		return
	}
	top.StampTemplate(ch, cx, cz, ig.X, ig.Y, ig.Z, 0)
	if !ig.Basement {
		return
	}
	if bot := TemplateByName("igloo/bottom"); bot != nil {
		bot.StampTemplate(ch, cx, cz, ig.X+0, ig.Y-3-ig.Depth*3, ig.Z-2, 0)
	}
	if mid := TemplateByName("igloo/middle"); mid != nil {
		for i := 0; i < ig.Depth-1; i++ { // LADDER offset (2,-3,4) below i*3
			mid.StampTemplate(ch, cx, cz, ig.X+2, ig.Y-3-i*3, ig.Z+4, 0)
		}
	}
}
