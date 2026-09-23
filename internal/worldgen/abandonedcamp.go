package worldgen

import (
	"strings"
	"sync"
)

// Abandoned camps (26.3): a tent with a campsite beside it — a campfire,
// seats, a chest or barrel of camp loot — and a tree or two, assembled from
// the real templates of the camp's own biome (structure/abandoned_camp_*,
// eighteen variants, one start pool each). One candidate per 37-chunk cell
// (the random_spread placement: spacing 37, separation 8), on dry land in one
// of the camp biomes; the structure set picks the variant whose biome the
// site stands in. The start piece is projected to the surface with vanilla's
// ground-level delta of one, so a template's bottom layer IS the ground.

const (
	campCell   = 37 * 16
	campSpread = (37 - 8) * 16 // a start chunk falls in the first spacing-separation chunks
	campDepth  = 2             // the structures' size
)

// campBiomes are the biomes with a camp variant (each variant's
// has_structure tag names exactly its own biome).
var campBiomes = map[string]bool{
	"bamboo_jungle": true, "birch_forest": true, "cherry_grove": true, "dappled_forest": true,
	"flower_forest": true, "forest": true, "meadow": true, "old_growth_birch_forest": true,
	"old_growth_pine_taiga": true, "old_growth_spruce_taiga": true, "pale_garden": true,
	"savanna": true, "snowy_taiga": true, "sparse_jungle": true, "swamp": true, "taiga": true,
	"windswept_forest": true, "wooded_badlands": true,
}

// AbandonedCamp is a placed camp (or the zero value): the start piece's
// origin and the biome variant it was built from.
type AbandonedCamp struct {
	X, Y, Z int
	Biome   string // variant, without namespace
	Exists  bool
}

// AbandonedCampIn returns the camp owning (wx,wz)'s cell, if its site falls
// on dry land in a camp biome.
func (g *Generator) AbandonedCampIn(wx, wz int) AbandonedCamp {
	if g.nether || g.end {
		return AbandonedCamp{}
	}
	ox, oz := cellOrigin(wx, campCell), cellOrigin(wz, campCell)
	x := ox + int(hash01(g.seed, ox, oz, 91231127)*campSpread) + 8
	z := oz + int(hash01(g.seed, ox, oz, 91231128)*campSpread) + 8
	biome := strings.TrimPrefix(g.BiomeName(x, z), "minecraft:")
	if !campBiomes[biome] {
		return AbandonedCamp{}
	}
	surf := g.Height(x, z)
	if surf <= SeaLevel {
		return AbandonedCamp{} // not on the water: a swamp's pools stay clear
	}
	return AbandonedCamp{X: x, Y: surf - 1, Z: z, Biome: biome, Exists: true}
}

type campKey struct {
	seed int64
	x, z int
}

var (
	campCache = map[campKey][]PlacedPiece{}
	campMu    sync.Mutex
)

// AssembleAbandonedCamp assembles (and caches) the camp's pieces from its
// biome's tent pool.
func (g *Generator) AssembleAbandonedCamp(c AbandonedCamp) []PlacedPiece {
	k := campKey{g.seed, c.X, c.Z}
	campMu.Lock()
	p, ok := campCache[k]
	campMu.Unlock()
	if ok {
		return p
	}
	rng := newJigsawRNG(g.seed, c.X^0x0CA3B000, c.Z)
	// Each piece is set on the ground under it and bearded (beard_thin), the
	// way the camps' terrain adaptation meets the land in vanilla.
	p = g.AssembleJigsawTerrain("abandoned_camp/tent/"+c.Biome, c.X, c.Y, c.Z, rng, campDepth)
	for i := range p {
		p[i].Beard = p[i].Tmpl != nil && !p[i].TerrainMatch
	}
	campMu.Lock()
	campCache[k] = p
	campMu.Unlock()
	return p
}

// stampAbandonedCamps stamps the camp pieces overlapping this chunk. A camp
// reaches at most 80 blocks from its start, so the neighbouring cells are
// checked too.
func (g *Generator) stampAbandonedCamps(ch *Chunk, cx, cz int32) {
	if g.nether || g.end {
		return
	}
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(campCell) {
		c := g.AbandonedCampIn(baseX+8+off[0], baseZ+8+off[1])
		if !c.Exists {
			continue
		}
		g.StampPieces(ch, cx, cz, g.AssembleAbandonedCamp(c))
	}
}

// CampChest is a camp container (chest or barrel) and its loot table.
type CampChest struct {
	X, Y, Z int
	Table   string
}

// AbandonedCampChests returns every container of the camps whose pieces
// could reach (wx, wz), with the loot table its template names.
func (g *Generator) AbandonedCampChests(wx, wz int) []CampChest {
	var out []CampChest
	for _, off := range cellNeighbours(campCell) {
		c := g.AbandonedCampIn(wx+off[0], wz+off[1])
		if !c.Exists {
			continue
		}
		for _, pc := range g.AssembleAbandonedCamp(c) {
			if pc.Tmpl == nil {
				continue // a tree
			}
			for i, cc := range pc.Tmpl.Chests {
				if i >= len(pc.Tmpl.ChestLoot) || pc.Tmpl.ChestLoot[i] == "" {
					continue
				}
				rx, ry, rz := pc.Tmpl.rotatePos(cc[0], cc[1], cc[2], pc.Rot)
				out = append(out, CampChest{pc.OX + rx, pc.OY + ry, pc.OZ + rz, pc.Tmpl.ChestLoot[i]})
			}
		}
	}
	return out
}

// Each variant is also a structure of its own name (abandoned_camp_swamp …),
// which is what an explorer map's destination names.
func init() {
	for b := range campBiomes {
		biome := b
		structureLocators["abandoned_camp_"+biome] = structureLocator{0, campCell, func(g *Generator, wx, wz int) (int, int, bool) {
			c := g.AbandonedCampIn(wx, wz)
			return c.X, c.Z, c.Exists && c.Biome == biome
		}}
	}
}
