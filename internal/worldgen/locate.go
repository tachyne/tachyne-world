package worldgen

// Structure location — what /locate structure asks: the nearest site of a
// named vanilla structure within a radius, scanning the placement cells the
// structure is laid out on. Every generator query is keyed by its cell, so
// the search is a cell walk rather than a chunk walk (vanilla's
// findNearestMapStructure does the same over its structure sets).

// structureLocator is one vanilla structure id: the dimension it lives in,
// its placement cell and the query that reports the site in a cell.
type structureLocator struct {
	dim  int // 0 overworld, 1 nether, 2 end
	cell int
	find func(g *Generator, wx, wz int) (x, z int, ok bool)
}

var structureLocators = map[string]structureLocator{
	"village": {0, villageCell, func(g *Generator, wx, wz int) (int, int, bool) {
		v := g.VillageIn(wx, wz)
		return v.X, v.Z, v.Exists
	}},
	"desert_pyramid": {0, templeCell, func(g *Generator, wx, wz int) (int, int, bool) {
		d := g.DesertTempleIn(wx, wz)
		return d.X + templeWidth/2, d.Z + templeDepth/2, d.Exists
	}},
	"igloo": {0, iglooCell, func(g *Generator, wx, wz int) (int, int, bool) {
		i := g.IglooIn(wx, wz)
		return i.X, i.Z, i.Exists
	}},
	"pillager_outpost": {0, outpostCell, func(g *Generator, wx, wz int) (int, int, bool) {
		o := g.OutpostIn(wx, wz)
		return o.X, o.Z, o.Exists
	}},
	"mansion": {0, mansionCell, func(g *Generator, wx, wz int) (int, int, bool) {
		m := g.MansionIn(wx, wz)
		return m.X, m.Z, m.Exists
	}},
	"monument": {0, monumentCell, func(g *Generator, wx, wz int) (int, int, bool) {
		m := g.MonumentIn(wx, wz)
		return m.X, m.Z, m.Exists
	}},
	"ancient_city": {0, ancientCityCell, func(g *Generator, wx, wz int) (int, int, bool) {
		a := g.AncientCityIn(wx, wz)
		return a.X, a.Z, a.Exists
	}},
	"trial_chambers": {0, trialChamberCell, func(g *Generator, wx, wz int) (int, int, bool) {
		t := g.TrialChamberIn(wx, wz)
		return t.X, t.Z, t.Exists
	}},
	"stronghold": {0, strongholdCell, func(g *Generator, wx, wz int) (int, int, bool) {
		s := g.StrongholdIn(wx, wz)
		return s.X, s.Z, s.Exists
	}},
	"shipwreck": {0, shipwreckCell, func(g *Generator, wx, wz int) (int, int, bool) {
		s := g.ShipwreckIn(wx, wz)
		return s.X, s.Z, s.Exists
	}},
	"buried_treasure": {0, buriedCell, func(g *Generator, wx, wz int) (int, int, bool) {
		b := g.BuriedTreasureIn(wx, wz)
		return b.X, b.Z, b.Exists
	}},
	"ocean_ruin": {0, oceanRuinCell, func(g *Generator, wx, wz int) (int, int, bool) {
		r := g.OceanRuinsIn(wx, wz)
		return r.X, r.Z, r.Exists
	}},
	"ocean_ruin_warm": {0, oceanRuinCell, func(g *Generator, wx, wz int) (int, int, bool) {
		r := g.OceanRuinsIn(wx, wz)
		return r.X, r.Z, r.Exists && r.Warm
	}},
	"ocean_ruin_cold": {0, oceanRuinCell, func(g *Generator, wx, wz int) (int, int, bool) {
		r := g.OceanRuinsIn(wx, wz)
		return r.X, r.Z, r.Exists && !r.Warm
	}},
	"trail_ruins": {0, trailRuinsCell, func(g *Generator, wx, wz int) (int, int, bool) {
		t := g.TrailRuinsIn(wx, wz)
		return t.X, t.Z, t.Exists
	}},
	"ruined_portal": {0, portalCell, func(g *Generator, wx, wz int) (int, int, bool) {
		p := g.RuinedPortalIn(wx, wz)
		return p.X, p.Z, p.Exists
	}},
	"mineshaft": {0, shaftCell, func(g *Generator, wx, wz int) (int, int, bool) {
		arms := g.shaftArms(wx, wz)
		if len(arms) == 0 {
			return 0, 0, false
		}
		return arms[0].x, arms[0].z, true
	}},
	"ruined_portal_nether": {1, portalCell, func(g *Generator, wx, wz int) (int, int, bool) {
		p := g.RuinedPortalNetherIn(wx, wz)
		return p.X, p.Z, p.Exists
	}},
	"fortress": {1, fortressCell, func(g *Generator, wx, wz int) (int, int, bool) {
		f := g.FortressIn(wx, wz)
		return f.X, f.Z, f.Exists
	}},
	"bastion_remnant": {1, bastionCell, func(g *Generator, wx, wz int) (int, int, bool) {
		b := g.BastionIn(wx, wz)
		return b.X, b.Z, b.Exists
	}},
	"end_city": {2, endCityCell, func(g *Generator, wx, wz int) (int, int, bool) {
		c := g.EndCityIn(wx, wz)
		return c.X, c.Z, c.Exists
	}},
}

// StructureNames lists the structure ids /locate accepts, sorted.
func StructureNames() []string {
	names := make([]string, 0, len(structureLocators))
	for n := range structureLocators {
		names = append(names, n)
	}
	sortStrings(names)
	return names
}

// StructureDim reports which dimension a structure id generates in
// (0 overworld, 1 nether, 2 end); ok=false for an unknown id.
func StructureDim(name string) (dim int, ok bool) {
	l, ok := structureLocators[name]
	return l.dim, ok
}

// LocateStructure finds the nearest site of the named structure to (wx, wz)
// within radius blocks, on this generator (whose dimension must match the
// structure's). ok=false when none lies within reach or the id is unknown.
func (g *Generator) LocateStructure(name string, wx, wz, radius int) (x, z int, ok bool) {
	l, known := structureLocators[name]
	if !known {
		return 0, 0, false
	}
	bestD := radius*radius + 1
	cells := radius/l.cell + 1
	ox, oz := cellOrigin(wx, l.cell), cellOrigin(wz, l.cell)
	for cx := -cells; cx <= cells; cx++ {
		for cz := -cells; cz <= cells; cz++ {
			sx, sz, found := l.find(g, ox+cx*l.cell, oz+cz*l.cell)
			if !found {
				continue
			}
			dx, dz := sx-wx, sz-wz
			if d := dx*dx + dz*dz; d < bestD {
				x, z, ok, bestD = sx, sz, true, d
			}
		}
	}
	return x, z, ok
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
