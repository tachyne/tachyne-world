package worldgen

import "sync"

// Bastion remnants — assembled from the real vanilla bastion jigsaw pools
// (four start variants: a housing unit, a hoglin stable, a treasure room
// and a bridge), with the pools' rule processors degrading the blackstone
// as vanilla does. Sited in the Nether at the vanilla start height, in
// every nether biome but the basalt deltas. Chests carry their own vanilla
// loot table (bastion_treasure / bridge / hoglin_stable / other); the
// "mobs" pieces carry the piglins, brutes and hoglins the server seeds.

const (
	bastionCell  = 448 // one candidate per 448-block cell (vanilla: ~one per 27 chunks)
	bastionOdds  = 0.6 // of qualifying cells
	bastionY     = 33  // vanilla start_height: absolute 33
	bastionDepth = 6   // vanilla size
)

// Bastion is a placed bastion site (or the zero value).
type Bastion struct {
	X, Y, Z int
	Exists  bool
}

// BastionIn returns the bastion owning (wx,wz)'s cell, if the cell rolled one
// on a qualifying biome.
func (g *Generator) BastionIn(wx, wz int) Bastion {
	ox, oz := cellOrigin(wx, bastionCell), cellOrigin(wz, bastionCell)
	if hash01(g.seed, ox, oz, 0xBA50) >= bastionOdds {
		return Bastion{}
	}
	x := ox + 96 + int(hash01(g.seed, ox, oz, 0xBA51)*float64(bastionCell-192))
	z := oz + 96 + int(hash01(g.seed, ox, oz, 0xBA52)*float64(bastionCell-192))
	if g.netherBiome(x, z) == "minecraft:basalt_deltas" {
		return Bastion{}
	}
	return Bastion{X: x, Y: bastionY, Z: z, Exists: true}
}

type bastionKey struct {
	seed int64
	x, z int
}

var (
	bastionCache = map[bastionKey][]PlacedPiece{}
	bastionMu    sync.Mutex
)

// AssembleBastion assembles (and caches) the bastion's pieces from the
// bastion/starts pool. Deterministic per site.
func (g *Generator) AssembleBastion(b Bastion) []PlacedPiece {
	k := bastionKey{g.seed, b.X, b.Z}
	bastionMu.Lock()
	p, ok := bastionCache[k]
	bastionMu.Unlock()
	if ok {
		return p
	}
	rng := newJigsawRNG(g.seed, b.X, b.Z)
	p = g.AssembleJigsaw("bastion/starts", b.X, b.Y, b.Z, rng, bastionDepth)
	bastionMu.Lock()
	bastionCache[k] = p
	bastionMu.Unlock()
	return p
}

// stampBastions stamps the bastion pieces overlapping this nether chunk.
func (g *Generator) stampBastions(ch *Chunk, cx, cz int32) {
	for _, b := range g.BastionsNear(int(cx)*16+8, int(cz)*16+8) {
		g.StampPieces(ch, cx, cz, g.AssembleBastion(b))
	}
}

// BastionsNear returns the bastions of the cell around (wx,wz) and its eight
// neighbours — a bastion's pieces can cross its cell's border.
func (g *Generator) BastionsNear(wx, wz int) []Bastion {
	var out []Bastion
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if b := g.BastionIn(wx+dx*bastionCell, wz+dz*bastionCell); b.Exists {
				out = append(out, b)
			}
		}
	}
	return out
}

// BastionChest is a bastion loot chest and the vanilla table it fills from.
type BastionChest struct {
	X, Y, Z int
	Table   string
}

// BastionChests returns the bastion's loot chests (the templates name their
// own tables).
func (g *Generator) BastionChests(b Bastion) []BastionChest {
	var out []BastionChest
	for _, pc := range g.AssembleBastion(b) {
		for i, c := range pc.Tmpl.Chests {
			rx, ry, rz := pc.Tmpl.rotatePos(c[0], c[1], c[2], pc.Rot)
			table := "chests/bastion_other"
			if i < len(pc.Tmpl.ChestLoot) && pc.Tmpl.ChestLoot[i] != "" {
				table = pc.Tmpl.ChestLoot[i]
			}
			out = append(out, BastionChest{pc.OX + rx, pc.OY + ry, pc.OZ + rz, table})
		}
	}
	return out
}

// BastionMob is a mob the bastion's templates seed, by entity name.
type BastionMob struct {
	X, Y, Z int
	Type    string
}

// BastionMobs returns the bastion's baked entity positions.
func (g *Generator) BastionMobs(b Bastion) []BastionMob {
	var out []BastionMob
	for _, pc := range g.AssembleBastion(b) {
		for _, m := range pc.Tmpl.Mobs {
			rx, ry, rz := pc.Tmpl.rotatePos(m.Pos[0], m.Pos[1], m.Pos[2], pc.Rot)
			out = append(out, BastionMob{pc.OX + rx, pc.OY + ry, pc.OZ + rz, m.Type})
		}
	}
	return out
}
