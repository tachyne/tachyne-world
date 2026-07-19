package worldgen

import "testing"

func findMansion(g *Generator) (Mansion, bool) {
	for i := -20; i <= 20; i++ {
		for j := -20; j <= 20; j++ {
			if m := g.MansionIn(i*mansionCell+512, j*mansionCell+512); m.Exists {
				return m, true
			}
		}
	}
	return Mansion{}, false
}

func TestMansionAssembles(t *testing.T) {
	for _, seed := range []int64{1, 7, 42, 99} {
		g := NewGenerator(seed)
		m, ok := findMansion(g)
		if !ok {
			continue
		}
		pieces := g.AssembleMansion(m)
		if len(pieces) < 30 {
			t.Errorf("seed %d: mansion assembled only %d pieces", seed, len(pieces))
		}
		nEntrance := 0
		for _, pc := range pieces {
			if TemplateByName("woodland_mansion/"+pc.tmpl) == nil {
				t.Errorf("seed %d: piece names unknown template %q", seed, pc.tmpl)
			}
			if pc.tmpl == "entrance" {
				nEntrance++
			}
		}
		if nEntrance != 1 {
			t.Errorf("seed %d: expected 1 entrance, got %d", seed, nEntrance)
		}
		chests := g.MansionChests(m)
		t.Logf("seed %d: mansion at %d,%d,%d — %d pieces, %d chests", seed, m.X, m.Y, m.Z, len(pieces), len(chests))
		return
	}
	t.Skip("no mansion found in scan for any test seed")
}
