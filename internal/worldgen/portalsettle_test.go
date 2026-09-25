package worldgen

import "testing"

// findSuitableY's descent: every overworld ruined portal stands with at
// least three of its four bottom corners in solid base terrain. The one a
// player found hanging off a snowy cliff on seed 1 (bug #38, near 475,-585)
// is among them.
func TestRuinedPortalsSettleOnThreeCorners(t *testing.T) {
	g := NewGenerator(1)
	checked, sawReported := 0, false
	for cx := -4; cx <= 4; cx++ {
		for cz := -4; cz <= 4; cz++ {
			p := g.RuinedPortalIn(cx*portalCell+8, cz*portalCell+8)
			if !p.Exists {
				continue
			}
			tp := TemplateByName(p.Tmpl)
			var corners [][2]int
			for _, c := range [][2]int{{0, 0}, {tp.Size[0] - 1, 0}, {0, tp.Size[2] - 1}, {tp.Size[0] - 1, tp.Size[2] - 1}} {
				rx, _, rz := tp.placePos(c[0], 0, c[1], p.Rot, p.Mir)
				corners = append(corners, [2]int{p.X + rx, p.Z + rz})
			}
			solid := 0
			for _, c := range corners {
				b := g.terrainCell(g.columnAt(c[0], c[1]), c[0], p.Y, c[1])
				if b != Air && (p.Props.placement != plOceanFloor || !IsFluid(b)) {
					solid++
				}
			}
			if solid < 3 && p.Y > MinY+15 {
				t.Errorf("portal %s at (%d,%d,%d): %d of 4 corners on solid ground", p.Tmpl, p.X, p.Y, p.Z, solid)
			}
			if p.X > 440 && p.X < 500 && p.Z > -620 && p.Z < -560 {
				sawReported = true
				t.Logf("the reported portal: %s at (%d,%d,%d), %d corners solid", p.Tmpl, p.X, p.Y, p.Z, solid)
			}
			checked++
		}
	}
	if checked < 10 || !sawReported {
		t.Fatalf("checked %d portals, reported one seen: %v", checked, sawReported)
	}
}
