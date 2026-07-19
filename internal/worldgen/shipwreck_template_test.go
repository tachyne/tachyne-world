package worldgen

import (
	"strings"
	"testing"
)

// TestShipwreckUsesTemplate: a placed wreck names a real baked template and its
// chests carry the vanilla shipwreck loot tables from the template markers.
func TestShipwreckUsesTemplate(t *testing.T) {
	g := NewGenerator(1)
	var found int
	for i := 0; i < 60 && found < 6; i++ {
		for j := 0; j < 60 && found < 6; j++ {
			s := g.ShipwreckIn(i*shipwreckCell+160, j*shipwreckCell+160)
			if !s.Exists {
				continue
			}
			found++
			if TemplateByName(s.Tmpl) == nil {
				t.Fatalf("wreck names unknown template %q", s.Tmpl)
			}
			if !strings.HasPrefix(s.Tmpl, "shipwreck/") {
				t.Fatalf("unexpected template %q", s.Tmpl)
			}
			for _, c := range s.Chests {
				switch c.Table {
				case "chests/shipwreck_supply", "chests/shipwreck_map", "chests/shipwreck_treasure":
				default:
					t.Fatalf("chest at %d,%d,%d has bad table %q", c.X, c.Y, c.Z, c.Table)
				}
			}
		}
	}
	if found == 0 {
		t.Skip("no wreck rolled")
	}
}
