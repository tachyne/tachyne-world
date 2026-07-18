package worldgen

// Pillager outpost — now assembled from the REAL vanilla pillager_outpost jigsaw
// templates (base_plate → watchtower + feature plates/tents/cages/targets) via
// the jigsaw assembler, so the layout is the exact vanilla structure. The server
// still garrisons a captain + pillager squad on approach (see updateOutposts).
// Outposts sit on dry land and keep clear of villages, mirroring vanilla's
// shared structure set.

const (
	outpostCell = 400 // one outpost per ~400-block cell (when the roll + siting pass)
)

// PillagerOutpost marks a placed outpost site. The watchtower + feature plates
// are assembled from the real vanilla jigsaw templates (see AssembleOutpost);
// X,Y,Z is the site centre at the surface (the server garrisons pillagers here).
type PillagerOutpost struct {
	X, Y, Z int
	Exists  bool
}

// OutpostIn returns the outpost whose cell contains (wx,wz), if the roll passes
// and the site is dry land clear of any village.
func (g *Generator) OutpostIn(wx, wz int) PillagerOutpost {
	ox, oz := cellOrigin(wx, outpostCell), cellOrigin(wz, outpostCell)
	if hash01(g.seed, ox, oz, 0x0057) >= 0.45 {
		return PillagerOutpost{}
	}
	x := ox + 40 + int(hash01(g.seed, ox, oz, 0x0058)*float64(outpostCell-80))
	z := oz + 40 + int(hash01(g.seed, ox, oz, 0x0059)*float64(outpostCell-80))
	y := g.Height(x, z)
	if y <= SeaLevel { // dry land only
		return PillagerOutpost{}
	}
	// Keep clear of villages (vanilla shares the village structure set with a
	// min separation, so the two never overlap).
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if g.VillageIn(x+dx*384, z+dz*384).Exists {
				return PillagerOutpost{}
			}
		}
	}
	return PillagerOutpost{X: x, Y: y, Z: z, Exists: true}
}

func (g *Generator) stampOutposts(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(outpostCell) {
		p := g.OutpostIn(baseX+8+off[0], baseZ+8+off[1])
		if !p.Exists {
			continue
		}
		g.StampPieces(ch, cx, cz, g.AssembleOutpost(p))
	}
}

// AssembleOutpost builds the outpost's jigsaw pieces (deterministic per site):
// the base plate centred on the site at the surface, then the watchtower + the
// feature plates the assembler attaches. Same result for every chunk the outpost
// touches, so the pieces align.
func (g *Generator) AssembleOutpost(p PillagerOutpost) []PlacedPiece {
	rng := newJigsawRNG(g.seed, p.X, p.Z)
	// base_plate is 16 wide; centre it on the site, foundation one below surface.
	return g.AssembleJigsaw("pillager_outpost/base_plates", p.X-8, p.Y-1, p.Z-8, rng, 7)
}

// OutpostChests returns the world positions of an outpost's loot chests (for
// loot routing), by assembling it and rotating each piece's chest cells.
func (g *Generator) OutpostChests(p PillagerOutpost) [][3]int {
	var out [][3]int
	for _, pc := range g.AssembleOutpost(p) {
		for _, c := range pc.Tmpl.Chests {
			rx, ry, rz := pc.Tmpl.rotatePos(c[0], c[1], c[2], pc.Rot)
			out = append(out, [3]int{pc.OX + rx, pc.OY + ry, pc.OZ + rz})
		}
	}
	return out
}
