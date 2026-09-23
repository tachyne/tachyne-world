package worldgen

import (
	"strconv"
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

// AssembleVillage assembles (and caches) a village's jigsaw pieces from its
// biome variant's town-centre start pool. Deterministic per site.
func (g *Generator) AssembleVillage(v Village) []PlacedPiece {
	k := villKey{g.seed, v.X, v.Z}
	villMu.Lock()
	p, ok := villCache[k]
	villMu.Unlock()
	if ok {
		return p
	}
	variant := v.Variant
	if variant == "" {
		variant = "plains"
	}
	rng := newJigsawRNG(g.seed, v.X, v.Z)
	p = g.AssembleJigsawTerrain("village/"+variant+"/town_centers", v.X, v.Y-1, v.Z, rng, 6)
	// terrain_adaptation beard_thin: the ground rises to meet every rigid
	// piece (houses, decor) — over water too, as vanilla's dirt pillars.
	for i := range p {
		p[i].Beard = p[i].Tmpl != nil && !p[i].TerrainMatch
	}
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

// VillagerSpawn is a villager the village's jigsaw placed: the villagers
// pool hangs one off each house (unemployed ten in twelve, a nitwit one,
// a baby one), the entity baked in the piece's template.
type VillagerSpawn struct {
	X, Y, Z int
	Kind    string // "unemployed", "nitwit" or "baby"
}

// VillageVillagers returns the villagers the village's pieces spawn.
func (g *Generator) VillageVillagers(v Village) []VillagerSpawn {
	var out []VillagerSpawn
	pieces := g.AssembleVillage(v)
	for i := range pieces {
		p := &pieces[i]
		k := strings.LastIndex(p.Tmpl.name, "/villagers/")
		if k < 0 {
			continue
		}
		kind := p.Tmpl.name[k+len("/villagers/"):]
		for _, m := range p.Tmpl.Mobs {
			if m.Type != "villager" {
				continue
			}
			w := wp(p, m.Pos[0], m.Pos[1], m.Pos[2])
			out = append(out, VillagerSpawn{w[0], w[1], w[2], kind})
		}
	}
	return out
}

// VillageMob is one entity a village's pieces carry, as vanilla's
// StructureTemplate.placeEntities places it: the villagers (one per villagers
// piece: unemployed, nitwit or baby), the town centre's iron golem, the cats,
// the pen animals and the desert's camels, and a zombie village's zombie
// villagers — with what the template's NBT says (villager data, age,
// persistence, collar). finalizeSpawn rolls the rest when it is spawned.
type VillageMob struct {
	Type       string  // entity name without namespace
	X, Y, Z    float64 // where it stands
	BX, BY, BZ int     // its block (which chunk places it)
	Prof       string  // villager data (villagers, zombie villagers)
	VType      string
	Level      int
	Age        int  // negative: a baby
	Persist    bool // PersistenceRequired
	Collar     int  // a cat's collar colour (-1: none given)
	N          int  // how many of the same type share its block before it (a pen's two sheep)
}

// Key identifies a placed entity across restarts: its type and block, and
// its ordinal when several of a type share a block.
func (m VillageMob) Key() string {
	k := m.Type + "@" + strconv.Itoa(m.BX) + "," + strconv.Itoa(m.BY) + "," + strconv.Itoa(m.BZ)
	if m.N > 0 {
		k += "#" + strconv.Itoa(m.N)
	}
	return k
}

// VillageMobs returns every entity the village's pieces carry.
func (g *Generator) VillageMobs(v Village) []VillageMob {
	var out []VillageMob
	seen := map[string]int{}
	pieces := g.AssembleVillage(v)
	for i := range pieces {
		p := &pieces[i]
		if p.Tmpl == nil {
			continue
		}
		for _, m := range p.Tmpl.Mobs {
			w := wp(p, m.Pos[0], m.Pos[1], m.Pos[2])
			y := float64(w[1])
			if len(m.At) == 3 {
				y = float64(p.OY) + m.At[1]
			}
			collar := -1
			if m.Collar != nil {
				collar = *m.Collar
			}
			vm := VillageMob{
				Type: m.Type, X: float64(w[0]) + 0.5, Y: y, Z: float64(w[2]) + 0.5,
				BX: w[0], BY: w[1], BZ: w[2],
				Prof: m.Prof, VType: m.VType, Level: m.Level, Age: m.Age, Persist: m.Persist, Collar: collar,
			}
			base := vm.Key() // N is still 0: the type and block alone
			vm.N = seen[base]
			seen[base]++
			out = append(out, vm)
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
	variant := v.Variant
	if variant == "" {
		variant = "plains"
	}
	pieces := g.AssembleVillage(v)
	for i := range pieces {
		p := &pieces[i]
		tbl := villageTableForPiece(p.Tmpl.name, variant)
		for _, c := range p.Tmpl.Chests {
			w := wp(p, c[0], c[1], c[2])
			out = append(out, VillageChest{w[0], w[1], w[2], tbl})
		}
	}
	return out
}

// villageTableForPiece maps a house template name to its vanilla chest table.
// Profession chests share one table across biomes; a plain house chest uses the
// biome-specific "<variant>_house" table (vanilla village_desert_house, …).
func villageTableForPiece(name, variant string) string {
	for kw, prof := range map[string]string{
		"armorer": "armorer", "butcher": "butcher", "cartographer": "cartographer",
		"fisher": "fisher", "fletcher": "fletcher", "mason": "mason",
		"shepherd": "shepherd", "tannery": "tannery", "temple": "temple",
		"tool_smith": "toolsmith", "toolsmith": "toolsmith", "weaponsmith": "weaponsmith",
	} {
		if strings.Contains(name, kw) {
			return "chests/village/village_" + prof
		}
	}
	return "chests/village/village_" + variant + "_house"
}
