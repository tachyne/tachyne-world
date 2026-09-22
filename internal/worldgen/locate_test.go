package worldgen

import "testing"

// LocateStructure reports the nearest site of a named structure; asking from
// a known site's own position finds that site, and every id resolves.
func TestLocateStructure(t *testing.T) {
	g := NewGenerator(1)
	for _, name := range StructureNames() {
		l := structureLocators[name]
		gen := g
		switch l.dim {
		case 1:
			gen = NewNetherGenerator(1)
		case 2:
			gen = NewEndGenerator(1)
		}
		var sx, sz int
		found := false
		for i := -30; i < 30 && !found; i++ {
			for j := -30; j < 30 && !found; j++ {
				sx, sz, found = l.find(gen, i*l.cell+l.cell/2, j*l.cell+l.cell/2)
			}
		}
		if !found {
			t.Logf("%s: no site in the scanned cells", name)
			continue
		}
		x, z, ok := gen.LocateStructure(name, sx+3, sz-5, 1600)
		if !ok || x != sx || z != sz {
			t.Errorf("%s: located %d,%d ok=%v from beside the site at %d,%d", name, x, z, ok, sx, sz)
		}
	}
	if _, _, ok := g.LocateStructure("moon_base", 0, 0, 1600); ok {
		t.Error("an unknown id must not locate")
	}
}

// A nether fossil is a locatable structure: the cell's fossil sits in the
// soul sand valley, above the lava, on the block the stamp uses.
func TestNetherFossilIn(t *testing.T) {
	g := NewNetherGenerator(1)
	for i := -60; i < 60; i++ {
		for j := -60; j < 60; j++ {
			f := g.NetherFossilIn(i*netherFossilCell+3, j*netherFossilCell+3)
			if !f.Exists {
				continue
			}
			if g.netherBiome(f.X, f.Z) != "minecraft:soul_sand_valley" || f.Y <= NetherLavaSea || f.N < 1 || f.N > 14 {
				t.Fatalf("fossil %+v is misplaced", f)
			}
			if x, z, ok := g.LocateStructure("nether_fossil", f.X+2, f.Z-2, 200); !ok || x != f.X || z != f.Z {
				t.Fatalf("locate from beside the fossil at %d,%d gave %d,%d ok=%v", f.X, f.Z, x, z, ok)
			}
			return
		}
	}
	t.Skip("no fossil in the scanned cells")
}

// The five biome families vanilla gives their own ruined-portal structure id
// are locatable by that id, and the plain "ruined_portal" is the catch-all
// for everything else rather than a synonym for all of them.
func TestRuinedPortalVariantsAreLocatable(t *testing.T) {
	g := NewGenerator(12345)
	for _, name := range []string{
		"ruined_portal", "ruined_portal_desert", "ruined_portal_jungle",
		"ruined_portal_mountain", "ruined_portal_ocean", "ruined_portal_swamp",
	} {
		if _, ok := StructureDim(name); !ok {
			t.Fatalf("%s is not a locatable structure", name)
		}
	}
	// A portal the plain id finds must belong to no variant family.
	x, z, ok := g.LocateStructure("ruined_portal", 0, 0, 3000)
	if !ok {
		t.Skip("no plain ruined portal within reach of the origin")
	}
	p := g.RuinedPortalIn(x, z)
	if v := PortalVariantFor(g.BiomeName(p.X, p.Z)); v != "" {
		t.Fatalf("the plain id found a %s portal at (%d,%d)", v, x, z)
	}
}
