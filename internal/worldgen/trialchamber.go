package worldgen

import (
	"strings"
	"sync"
)

// Trial chambers — assembled from the REAL vanilla trial_chambers jigsaw
// templates (chamber/end → corridors, chambers, intersections, spawner/vault
// rooms and supply chests), a large underground jigsaw like the ancient city.
// The trial spawners and vaults stamp as their real blocks (mechanics are a
// later pass); the layout is vanilla-faithful. Assembly is cached per site — a
// chamber spans many chunks and would otherwise re-run for each.
//
// Placement mirrors the ancient city (deterministic cell + 128-block margin so
// the whole structure stays inside one cell), but trial chambers generate under
// ordinary overworld terrain; deep_dark cells are left to the ancient city.

const (
	trialChamberCell = 512  // one candidate per 512-block cell
	trialChamberOdds = 0.65 // of cells (skipped over deep_dark — ancient-city land)
)

// TrialChamber is a placed chamber site (or the zero value).
type TrialChamber struct {
	X, Y, Z int
	Exists  bool
}

// TrialChamberIn returns the chamber owning (wx,wz)'s cell.
func (g *Generator) TrialChamberIn(wx, wz int) TrialChamber {
	ox, oz := cellOrigin(wx, trialChamberCell), cellOrigin(wz, trialChamberCell)
	if hash01(g.seed, ox, oz, 0x7C00) >= trialChamberOdds {
		return TrialChamber{}
	}
	x := ox + 128 + int(hash01(g.seed, ox, oz, 0x7C01)*float64(trialChamberCell-256))
	z := oz + 128 + int(hash01(g.seed, ox, oz, 0x7C02)*float64(trialChamberCell-256))
	// Site depth: vanilla starts a trial chamber uniformly in [-40,-20]; it grows
	// upward/outward from there. Keep it clear of the deep_dark (ancient city).
	y := -40 + int(hash01(g.seed, ox, oz, 0x7C03)*20)
	if g.caveBiome(x, z, y) == "minecraft:deep_dark" {
		return TrialChamber{}
	}
	// Skip sites whose column is too shallow to bury the chamber (oceans/lowlands
	// where -20 would be near or above the surface would poke through).
	if g.Height(x, z) < y+40 {
		return TrialChamber{}
	}
	return TrialChamber{X: x, Y: y, Z: z, Exists: true}
}

type tcKey struct {
	seed int64
	x, z int
}

var (
	tcCache = map[tcKey][]PlacedPiece{}
	tcMu    sync.Mutex
)

// AssembleTrialChamber assembles (and caches) the chamber's jigsaw pieces from
// the chamber/end start pool. Deterministic per site.
func (g *Generator) AssembleTrialChamber(t TrialChamber) []PlacedPiece {
	k := tcKey{g.seed, t.X, t.Z}
	tcMu.Lock()
	p, ok := tcCache[k]
	tcMu.Unlock()
	if ok {
		return p
	}
	rng := newJigsawRNG(g.seed, t.X, t.Z)
	p = g.AssembleJigsaw("trial_chambers/chamber/end", t.X, t.Y, t.Z, rng, 8)
	tcMu.Lock()
	tcCache[k] = p
	tcMu.Unlock()
	return p
}

// stampTrialChambers stamps the chamber pieces overlapping this chunk.
func (g *Generator) stampTrialChambers(ch *Chunk, cx, cz int32) {
	t := g.TrialChamberIn(int(cx)*16+8, int(cz)*16+8)
	if !t.Exists {
		return
	}
	g.StampPieces(ch, cx, cz, g.AssembleTrialChamber(t))
}

// TrialChamberChest is a placed loot chest with the vanilla table its piece implies.
type TrialChamberChest struct {
	X, Y, Z int
	Table   string
}

// TrialChamberChests returns every loot chest/barrel with the table inferred
// from its piece (corridor/entrance/supply/intersection).
func (g *Generator) TrialChamberChests(t TrialChamber) []TrialChamberChest {
	var out []TrialChamberChest
	for _, pc := range g.AssembleTrialChamber(t) {
		tbl := trialChamberTableForPiece(pc.Tmpl.name)
		for _, c := range pc.Tmpl.Chests {
			rx, ry, rz := pc.Tmpl.rotatePos(c[0], c[1], c[2], pc.Rot)
			out = append(out, TrialChamberChest{pc.OX + rx, pc.OY + ry, pc.OZ + rz, tbl})
		}
	}
	return out
}

// trialChamberTableForPiece maps a piece name to its vanilla chest loot table.
func trialChamberTableForPiece(name string) string {
	switch {
	case strings.Contains(name, "entrance"):
		return "chests/trial_chambers/entrance"
	case strings.Contains(name, "intersection"):
		return "chests/trial_chambers/intersection"
	case strings.Contains(name, "supply"):
		return "chests/trial_chambers/supply"
	default:
		return "chests/trial_chambers/corridor"
	}
}
