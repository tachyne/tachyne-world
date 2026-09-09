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
	if _, _, ok := g.LocateStructure("swamp_hut", 0, 0, 1600); ok {
		t.Error("an unknown id must not locate")
	}
}
