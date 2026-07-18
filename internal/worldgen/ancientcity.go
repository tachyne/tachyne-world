package worldgen

import "sync"

// Ancient City — now assembled from the REAL vanilla ancient_city jigsaw
// templates (city_center → walls, structures, sculk floors and ~26 loot
// chests), replacing the hand-built vault. Sited where the deep_dark biome
// generates; the server's sculk scan picks up the can-summon shriekers so the
// Warden loop still triggers. The city is large, so the assembly is cached per
// site (it is otherwise re-run for every chunk it touches).

const (
	ancientCityCell = 512  // one candidate per 512-block cell
	ancientCityOdds = 0.55 // of qualifying (deep_dark) cells
	ancientCityY    = -51  // start depth: deep in tachyne's deep_dark band; the city rises from here
)

// AncientCity is a placed city site (or the zero value).
type AncientCity struct {
	X, Y, Z int
	Exists  bool
}

// AncientCityIn returns the city owning (wx,wz)'s cell, where deep_dark lives.
func (g *Generator) AncientCityIn(wx, wz int) AncientCity {
	ox, oz := cellOrigin(wx, ancientCityCell), cellOrigin(wz, ancientCityCell)
	if hash01(g.seed, ox, oz, 0xAC00) >= ancientCityOdds {
		return AncientCity{}
	}
	x := ox + 128 + int(hash01(g.seed, ox, oz, 0xAC01)*float64(ancientCityCell-256))
	z := oz + 128 + int(hash01(g.seed, ox, oz, 0xAC02)*float64(ancientCityCell-256))
	if g.caveBiome(x, z, ancientCityY) != "minecraft:deep_dark" {
		return AncientCity{} // only where deep_dark actually generates
	}
	return AncientCity{X: x, Y: ancientCityY, Z: z, Exists: true}
}

type acKey struct {
	seed int64
	x, z int
}

var (
	acCache = map[acKey][]PlacedPiece{}
	acMu    sync.Mutex
)

// AssembleAncientCity assembles (and caches) the city's jigsaw pieces from the
// city_center start pool. Deterministic per site.
func (g *Generator) AssembleAncientCity(a AncientCity) []PlacedPiece {
	k := acKey{g.seed, a.X, a.Z}
	acMu.Lock()
	p, ok := acCache[k]
	acMu.Unlock()
	if ok {
		return p
	}
	rng := newJigsawRNG(g.seed, a.X, a.Z)
	p = g.AssembleJigsaw("ancient_city/city_center", a.X, a.Y, a.Z, rng, 7)
	acMu.Lock()
	acCache[k] = p
	acMu.Unlock()
	return p
}

// stampAncientCity stamps the city pieces overlapping this chunk. The 128-block
// cell margin keeps a city clear of cell borders, so the chunk-centre cell owns
// it.
func (g *Generator) stampAncientCity(ch *Chunk, cx, cz int32) {
	a := g.AncientCityIn(int(cx)*16+8, int(cz)*16+8)
	if !a.Exists {
		return
	}
	g.StampPieces(ch, cx, cz, g.AssembleAncientCity(a))
}

// AncientCityChests returns the world positions of the city's loot chests.
func (g *Generator) AncientCityChests(a AncientCity) [][3]int {
	var out [][3]int
	for _, pc := range g.AssembleAncientCity(a) {
		for _, c := range pc.Tmpl.Chests {
			rx, ry, rz := pc.Tmpl.rotatePos(c[0], c[1], c[2], pc.Rot)
			out = append(out, [3]int{pc.OX + rx, pc.OY + ry, pc.OZ + rz})
		}
	}
	return out
}
