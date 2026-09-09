package worldgen

import "sync"

// Trail ruins: vanilla's buried jigsaw structure (structure/trail_ruins) —
// a tower start piece from trail_ruins/tower with halls, roads, buildings
// and decor attached, assembled from the real templates and started fifteen
// blocks under the surface so the tower's top is what shows above ground.
// The pool processors turn some gravel to dirt and coarse dirt and some
// mud bricks to packed mud, and the capped archaeology rules pick the
// suspicious gravel per piece: six common and three rare finds in a house
// piece, two common in a road or tower top (the loot tables are vanilla's
// trail_ruins_common/rare). One candidate per 34-chunk cell, in taigas, old
// growth birch forest and jungle.

const (
	trailRuinsCell  = 544 // spacing 34 chunks
	trailRuinsDepth = -15 // start_height relative to WORLD_SURFACE_WG
)

var trailRuinsBiomes = map[string]bool{
	"minecraft:taiga": true, "minecraft:snowy_taiga": true,
	"minecraft:old_growth_pine_taiga": true, "minecraft:old_growth_spruce_taiga": true,
	"minecraft:old_growth_birch_forest": true, "minecraft:jungle": true,
}

// TrailRuins is a placed site (or the zero value): the start piece's origin.
type TrailRuins struct {
	X, Y, Z int
	Exists  bool
}

// TrailRuinsIn returns the site owning (wx,wz)'s cell, if it fell on a
// trail-ruins biome above the sea.
func (g *Generator) TrailRuinsIn(wx, wz int) TrailRuins {
	if g.nether || g.end {
		return TrailRuins{}
	}
	ox, oz := cellOrigin(wx, trailRuinsCell), cellOrigin(wz, trailRuinsCell)
	x := ox + 96 + int(hash01(g.seed, ox, oz, 0x7A01)*float64(trailRuinsCell-192))
	z := oz + 96 + int(hash01(g.seed, ox, oz, 0x7A02)*float64(trailRuinsCell-192))
	if !trailRuinsBiomes[g.BiomeName(x, z)] {
		return TrailRuins{}
	}
	surf := g.Height(x+2, z+2)
	if surf <= SeaLevel {
		return TrailRuins{}
	}
	return TrailRuins{X: x, Y: surf + trailRuinsDepth, Z: z, Exists: true}
}

type trKey struct {
	seed int64
	x, z int
}

var (
	trCache = map[trKey][]PlacedPiece{}
	trMu    sync.Mutex
)

// AssembleTrailRuins assembles (and caches) the site's pieces from the
// tower start pool, with every capped archaeology pick resolved.
func (g *Generator) AssembleTrailRuins(t TrailRuins) []PlacedPiece {
	k := trKey{g.seed, t.X, t.Z}
	trMu.Lock()
	p, ok := trCache[k]
	trMu.Unlock()
	if ok {
		return p
	}
	rng := newJigsawRNG(g.seed, t.X^0x7A000000, t.Z)
	p = g.AssembleJigsaw("trail_ruins/tower", t.X, t.Y, t.Z, rng, 7)
	g.attachCappedPicks(p, 0x7A10)
	trMu.Lock()
	trCache[k] = p
	trMu.Unlock()
	return p
}

// stampTrailRuins stamps the pieces overlapping this chunk. Roads run up to
// 80 blocks from the tower, so the neighbouring cells are checked too.
func (g *Generator) stampTrailRuins(ch *Chunk, cx, cz int32) {
	if g.nether || g.end {
		return
	}
	baseX, baseZ := int(cx)*16, int(cz)*16
	for _, off := range cellNeighbours(trailRuinsCell) {
		t := g.TrailRuinsIn(baseX+8+off[0], baseZ+8+off[1])
		if !t.Exists {
			continue
		}
		g.StampPieces(ch, cx, cz, g.AssembleTrailRuins(t))
	}
}

// TrailRuinsSus returns every suspicious-gravel cell of the site with the
// archaeology table it holds.
func (g *Generator) TrailRuinsSus(t TrailRuins) []SusCell {
	var out []SusCell
	for _, p := range g.AssembleTrailRuins(t) {
		out = append(out, p.Sus...)
	}
	return out
}

// TrailRuinsNear returns the sites whose pieces could reach (wx, wz): the
// owning cell's and its neighbours'.
func (g *Generator) TrailRuinsNear(wx, wz int) []TrailRuins {
	var out []TrailRuins
	for _, off := range cellNeighbours(trailRuinsCell) {
		if t := g.TrailRuinsIn(wx+off[0], wz+off[1]); t.Exists {
			out = append(out, t)
		}
	}
	return out
}
