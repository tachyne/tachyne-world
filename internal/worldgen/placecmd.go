package worldgen

import "strings"

// The generator's half of /place (PlaceCommand): a template's blocks as a
// list of cells, a jigsaw grown from a pool with its start anchored on a
// named jigsaw, and the jigsaw-built structures (and igloos) started from a
// chosen chunk instead of where the seed puts them. The server writes the
// result into the live world through its ordinary block edits.

// Template mirror modes, as the command names them (Mirror).
const (
	MirrorNone      = mirNone
	MirrorLeftRight = mirLR
	MirrorFrontBack = mirFB
)

// TemplateCell is one block a template places.
type TemplateCell struct {
	X, Y, Z int
	State   uint32
}

// LootChest is a container a placed structure stocks, with the loot table
// its template names ("" when the structure's code assigns it).
type LootChest struct {
	X, Y, Z int
	Table   string
}

// PlaceCells is StructureTemplate.placeInWorld's block list: every block of
// the template, mirrored then rotated about the template's origin (pivot 0,
// as the command places it) and offset to (px,py,pz). Jigsaw and structure
// blocks are placed as the template holds them, their orientation turned
// with the rest. keep (nil = every block) is asked once per block, in the
// template's order: the command's BlockRotProcessor integrity roll.
func (t *Template) PlaceCells(px, py, pz, rot, mir int, keep func() bool) []TemplateCell {
	out := make([]TemplateCell, 0, len(t.Blocks))
	for _, b := range t.Blocks {
		p := t.Palette[b[3]]
		var state uint32
		switch trimNS(p.Name) {
		case "structure_void":
			continue
		case "jigsaw", "structure_block":
			state = markerState(p, rot, mir)
		default:
			state = resolveStateM(p, rot, mir)
		}
		if keep != nil && !keep() {
			continue
		}
		if state == tmplSkip {
			continue
		}
		tx, ty, tz := transformPos(b[0], b[1], b[2], rot, mir)
		out = append(out, TemplateCell{px + tx, py + ty, pz + tz, state})
	}
	return out
}

// PlaceChests is where PlaceCells puts the template's loot containers.
func (t *Template) PlaceChests(px, py, pz, rot, mir int) []LootChest {
	var out []LootChest
	for i, c := range t.Chests {
		tx, ty, tz := transformPos(c[0], c[1], c[2], rot, mir)
		lc := LootChest{X: px + tx, Y: py + ty, Z: pz + tz}
		if i < len(t.ChestLoot) {
			lc.Table = t.ChestLoot[i]
		}
		out = append(out, lc)
	}
	return out
}

// markerState resolves a jigsaw or structure block with its properties; a
// jigsaw's orientation (FrontAndTop, "north_up") turns with the template.
func markerState(p paletteEntry, rot, mir int) uint32 {
	base := safeBase(trimNS(p.Name))
	if base == tmplSkip {
		return tmplSkip
	}
	info, ok := InfoForState(base)
	if !ok {
		return base
	}
	state := base
	for k, v := range p.Props {
		if k == "orientation" {
			if front, top, found := strings.Cut(v, "_"); found {
				v = rotDir(mirrorDir(front, mir), rot) + "_" + rotDir(mirrorDir(top, mir), rot)
			}
		}
		if info.HasProperty(k) {
			state = SetProperty(info, state, k, v)
		}
	}
	return state
}

// mirrorDir is Mirror.mirror on a direction.
func mirrorDir(d string, mir int) string {
	switch {
	case mir == mirLR && d == "north":
		return "south"
	case mir == mirLR && d == "south":
		return "north"
	case mir == mirFB && d == "east":
		return "west"
	case mir == mirFB && d == "west":
		return "east"
	}
	return d
}

// HasPool reports whether a template pool exists (namespace optional).
func HasPool(name string) bool {
	p := pools[trimNS(name)]
	return p != nil
}

// PlaceJigsawPieces is JigsawPlacement.generateJigsaw: a random element of
// the pool, turned a random quarter, set so one of its jigsaws named target
// lands at (x,y,z) — then, as the start piece's ground level delta of one
// moves it, a block lower — and grown to maxDepth. ok is false when the
// pool's pick is no template or carries no such jigsaw.
func (g *Generator) PlaceJigsawPieces(pool, target string, x, y, z, maxDepth int, seed int64) ([]PlacedPiece, bool) {
	sp := pools[trimNS(pool)]
	if sp == nil || len(sp.Elements) == 0 {
		return nil, false
	}
	prng := newJigsawRNG(seed, x, z)
	rot := prng.intn(4)
	loc := weightedPick(sp, prng)
	start := templates[loc]
	if start == nil {
		return nil, false
	}
	target = trimNS(target)
	var named []jigsawBlock
	for _, j := range start.Jigsaws {
		if trimNS(j.Name) == target {
			named = append(named, j)
		}
	}
	if len(named) == 0 {
		return nil, false
	}
	j := named[prng.intn(len(named))]
	rx, ry, rz := start.rotatePos(j.Pos[0], j.Pos[1], j.Pos[2], rot)
	ox, oy, oz := x-rx, y-ry-1, z-rz
	sw, sh, sd := start.rotatedSize(rot)
	first := &PlacedPiece{Tmpl: start, OX: ox, OY: oy, OZ: oz, Rot: rot, Proc: sp.procFor(loc),
		x1: ox + sw, y1: oy + sh, z1: oz + sd, gld: 1}
	return g.growJigsaw(first, prng, maxDepth, false, nil, nil), true
}

// PlaceStructureNames are the structures PlaceStructurePieces can start.
var PlaceStructureNames = func() []string {
	out := []string{"igloo", "pillager_outpost", "ancient_city", "bastion_remnant", "trail_ruins", "trial_chambers"}
	for _, v := range []string{"desert", "plains", "savanna", "snowy", "taiga"} {
		out = append(out, "village_"+v)
	}
	for b := range campBiomes {
		out = append(out, "abandoned_camp_"+b)
	}
	return out
}()

// PlaceStructurePieces is Structure.generate for /place structure: the
// named structure (vanilla's id, namespace optional) laid out from the chunk
// holding (x,z) at the height its structure definition starts it, the way
// the engine lays that structure out when the seed places it — with no
// biome or spacing check, as the command skips them. ok is false for a
// structure the engine cannot start on demand, or an empty layout.
func (g *Generator) PlaceStructurePieces(name string, x, z int) ([]PlacedPiece, bool) {
	name = trimNS(name)
	bx, bz := x>>4<<4, z>>4<<4
	var p []PlacedPiece
	switch {
	case name == "igloo":
		p = g.iglooPieces(bx, bz)
	case strings.HasPrefix(name, "village_"):
		variant := strings.TrimPrefix(name, "village_")
		switch variant {
		case "desert", "plains", "savanna", "snowy", "taiga":
		default:
			return nil, false
		}
		p = g.AssembleVillage(Village{X: bx, Y: g.Height(bx, bz), Z: bz, Variant: variant, Exists: true})
	case name == "pillager_outpost":
		p = g.AssembleOutpost(PillagerOutpost{X: bx + 8, Y: g.Height(bx+8, bz+8), Z: bz + 8, Exists: true})
	case name == "ancient_city":
		p = g.AssembleAncientCity(AncientCity{X: bx, Y: ancientCityY, Z: bz, Exists: true})
	case name == "bastion_remnant":
		p = g.AssembleBastion(Bastion{X: bx, Y: bastionY, Z: bz, Exists: true})
	case name == "trail_ruins":
		p = g.AssembleTrailRuins(TrailRuins{X: bx, Y: g.Height(bx+2, bz+2) + trailRuinsDepth, Z: bz, Exists: true})
	case name == "trial_chambers":
		y := -40 + int(hash01(g.seed, bx, bz, 0x7C03)*20) // uniform in [-40,-20]
		p = g.AssembleTrialChamber(TrialChamber{X: bx, Y: y, Z: bz, Exists: true})
	case strings.HasPrefix(name, "abandoned_camp_"):
		biome := strings.TrimPrefix(name, "abandoned_camp_")
		if !campBiomes[biome] {
			return nil, false
		}
		p = g.AssembleAbandonedCamp(AbandonedCamp{X: bx, Y: g.Height(bx, bz) - 1, Z: bz, Biome: biome, Exists: true})
	default:
		return nil, false
	}
	return p, len(p) > 0
}

// iglooPieces is IglooPieces.addPieces at a chunk corner: the top on the
// ground and, half the time, the ladder shaft and the laboratory below.
func (g *Generator) iglooPieces(x, z int) []PlacedPiece {
	top := TemplateByName("igloo/top")
	if top == nil {
		return nil
	}
	y := g.Height(x, z) - 1
	piece := func(t *Template, ox, oy, oz int) PlacedPiece {
		return PlacedPiece{Tmpl: t, OX: ox, OY: oy, OZ: oz,
			x1: ox + t.Size[0], y1: oy + t.Size[1], z1: oz + t.Size[2]}
	}
	out := []PlacedPiece{piece(top, x, y, z)}
	if hash01(g.seed, x, z, 0x1603) >= 0.5 {
		return out
	}
	depth := 4 + int(hash01(g.seed, x, z, 0x1604)*8)
	if bot := TemplateByName("igloo/bottom"); bot != nil {
		out = append(out, piece(bot, x, y-3-depth*3, z-2))
	}
	if mid := TemplateByName("igloo/middle"); mid != nil {
		for i := 0; i < depth-1; i++ {
			out = append(out, piece(mid, x+2, y-3-i*3, z+4))
		}
	}
	return out
}

// PiecesBounds is the block columns a set of pieces covers (inclusive),
// feature pieces widened by a tree's reach.
func PiecesBounds(pieces []PlacedPiece) (x0, z0, x1, z1 int, ok bool) {
	for i, p := range pieces {
		px0, pz0, px1, pz1 := p.OX, p.OZ, p.x1-1, p.z1-1
		if p.Feature != "" {
			px0, pz0, px1, pz1 = px0-8, pz0-8, px1+8, pz1+8
		}
		if i == 0 {
			x0, z0, x1, z1 = px0, pz0, px1, pz1
			continue
		}
		x0, z0, x1, z1 = min(x0, px0), min(z0, pz0), max(x1, px1), max(z1, pz1)
	}
	return x0, z0, x1, z1, len(pieces) > 0
}

// PieceLootChests is where StampPieces puts the pieces' loot containers,
// with the table each template names.
func (g *Generator) PieceLootChests(pieces []PlacedPiece) []LootChest {
	var out []LootChest
	for i := range pieces {
		p := &pieces[i]
		if p.Tmpl == nil {
			continue
		}
		for k, c := range p.Tmpl.Chests {
			rx, ry, rz := p.Tmpl.rotatePos(c[0], c[1], c[2], p.Rot)
			lc := LootChest{X: p.OX + rx, Y: p.OY + ry, Z: p.OZ + rz}
			if p.TerrainMatch {
				lc.Y = g.SurfaceWG(lc.X, lc.Z) - 1 + ry
			}
			if k < len(p.Tmpl.ChestLoot) {
				lc.Table = p.Tmpl.ChestLoot[k]
			}
			out = append(out, lc)
		}
	}
	return out
}
