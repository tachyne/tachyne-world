package worldgen

import (
	"strings"
	"testing"
)

// Beached wrecks lie on beaches, half buried, and stamp their hull; a build
// in the box keeps the wreck out.
func TestBeachedShipwreck(t *testing.T) {
	g := NewGenerator(3)
	var s Shipwreck
	for i := -8; i <= 8 && !s.Exists; i++ {
		for j := -8; j <= 8 && !s.Exists; j++ {
			s = g.BeachedShipwreckIn(i*beachedCell+10, j*beachedCell+10)
		}
	}
	if !s.Exists {
		t.Skip("no beached wreck in 289 cells")
	}
	if !isBeach(g.resolveBiome(s.X, s.Z).Name) {
		t.Errorf("wreck at %d,%d is in %s", s.X, s.Z, g.resolveBiome(s.X, s.Z).Name)
	}
	if strings.Contains(s.Tmpl, "upsidedown") {
		t.Errorf("a beached wreck is %s", s.Tmpl)
	}
	tm := TemplateByName(s.Tmpl)
	cx, cz := int32(floorDiv16(s.X)), int32(floorDiv16(s.Z))
	count := func(g *Generator) int {
		ch := g.GenerateChunk(cx, cz)
		n := 0
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				for y := s.Y; y < s.Y+tm.Size[1]; y++ {
					if name, ok := StateName(sectionBlockAt(ch, lx, y, lz)); ok && (strings.HasSuffix(name, "planks") || strings.HasSuffix(name, "_log")) {
						n++
					}
				}
			}
		}
		return n
	}
	with := count(g)
	t.Logf("wreck %s rot %d at %d,%d,%d: %d wood in its chunk", s.Tmpl, s.Rot, s.X, s.Y, s.Z, with)
	if with == 0 {
		t.Fatal("the beached wreck left no wood in its chunk")
	}
	g2 := NewGenerator(3)
	setEditOverlay(g2, map[[3]int]uint32{{s.X, s.Y + 1, s.Z}: BlockBase("stone_bricks")})
	if without := count(g2); without >= with {
		t.Errorf("a build in the wreck's box left %d wood (was %d)", without, with)
	}
	if x, z, ok := g.LocateStructure("shipwreck_beached", s.X, s.Z, 100); !ok || x != s.X || z != s.Z {
		t.Errorf("locate shipwreck_beached: %d,%d %v", x, z, ok)
	}
}
