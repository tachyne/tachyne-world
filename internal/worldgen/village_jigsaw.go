package worldgen

import (
	"strings"
	"sync"
)

// Jigsaw village — assembled from the real vanilla village/plains templates
// (town centre → streets → houses/decorations), replacing the hand-built
// cottages. The server builds its villager economy on the beds, job-site blocks
// and meeting-point bell these pieces carry (see VillageBeds/JobSites/Bells).
// Currently plains-only; the other biome variants follow.
//
// Streets use terrain_matching projection in vanilla (they conform to the
// ground); the assembler treats every piece rigid, so villages rely on
// VillageIn's flatness gate to stay level.

type villKey struct {
	seed int64
	x, z int
}

var (
	villCache = map[villKey][]PlacedPiece{}
	villMu    sync.Mutex
)

// AssembleVillage assembles (and caches) a village's jigsaw pieces from the
// plains town-centre start pool. Deterministic per site.
func (g *Generator) AssembleVillage(v Village) []PlacedPiece {
	k := villKey{g.seed, v.X, v.Z}
	villMu.Lock()
	p, ok := villCache[k]
	villMu.Unlock()
	if ok {
		return p
	}
	rng := newJigsawRNG(g.seed, v.X, v.Z)
	p = g.AssembleJigsawTerrain("village/plains/town_centers", v.X, v.Y-1, v.Z, rng, 6)
	villMu.Lock()
	villCache[k] = p
	villMu.Unlock()
	return p
}

// wp maps a template-local cell to world space for a placed piece.
func wp(p *PlacedPiece, x, y, z int) [3]int {
	rx, ry, rz := p.Tmpl.rotatePos(x, y, z, p.Rot)
	return [3]int{p.OX + rx, p.OY + ry, p.OZ + rz}
}

// VillageBeds returns the world positions of every villager home bed.
func (g *Generator) VillageBeds(v Village) [][3]int {
	var out [][3]int
	pieces := g.AssembleVillage(v)
	for i := range pieces {
		p := &pieces[i]
		for _, b := range p.Tmpl.Beds {
			out = append(out, wp(p, b[0], b[1], b[2]))
		}
	}
	return out
}

// VillageJobSites returns [x,y,z,profession] for every job-site block.
func (g *Generator) VillageJobSites(v Village) [][4]int {
	var out [][4]int
	pieces := g.AssembleVillage(v)
	for i := range pieces {
		p := &pieces[i]
		for _, j := range p.Tmpl.JobSites {
			w := wp(p, j[0], j[1], j[2])
			out = append(out, [4]int{w[0], w[1], w[2], j[3]})
		}
	}
	return out
}

// VillageBells returns the world positions of the meeting-point bells.
func (g *Generator) VillageBells(v Village) [][3]int {
	var out [][3]int
	pieces := g.AssembleVillage(v)
	for i := range pieces {
		p := &pieces[i]
		for _, b := range p.Tmpl.Bells {
			out = append(out, wp(p, b[0], b[1], b[2]))
		}
	}
	return out
}

// VillageChest is a placed loot chest with the vanilla table its house implies.
type VillageChest struct {
	X, Y, Z int
	Table   string
}

// VillageChests returns every chest with the loot table inferred from its house
// piece (a weaponsmith house → chests/village/village_weaponsmith, …), falling
// back to the plains house table.
func (g *Generator) VillageChests(v Village) []VillageChest {
	var out []VillageChest
	pieces := g.AssembleVillage(v)
	for i := range pieces {
		p := &pieces[i]
		tbl := villageTableForPiece(p.Tmpl.name)
		for _, c := range p.Tmpl.Chests {
			w := wp(p, c[0], c[1], c[2])
			out = append(out, VillageChest{w[0], w[1], w[2], tbl})
		}
	}
	return out
}

// villageTableForPiece maps a house template name to its vanilla chest table.
func villageTableForPiece(name string) string {
	for kw, prof := range map[string]string{
		"armorer": "armorer", "butcher": "butcher", "cartographer": "cartographer",
		"fisher": "fisher", "fletcher": "fletcher", "mason": "mason",
		"shepherd": "shepherd", "tannery": "tannery", "temple": "temple",
		"tool_smith": "toolsmith", "toolsmith": "toolsmith", "weaponsmith": "weaponsmith",
		"library": "plains_house", "farm": "plains_house",
	} {
		if strings.Contains(name, kw) {
			return "chests/village/village_" + prof
		}
	}
	return "chests/village/village_plains_house"
}
