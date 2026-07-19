package worldgen

import "testing"

// findTrialChamber scans cells for a chamber site.
func findTrialChamber(g *Generator) (TrialChamber, bool) {
	for x := 0; x <= trialChamberCell*40; x += trialChamberCell {
		for z := 0; z <= trialChamberCell*40; z += trialChamberCell {
			if t := g.TrialChamberIn(x+8, z+8); t.Exists {
				return t, true
			}
		}
	}
	return TrialChamber{}, false
}

func TestTrialChamberAssembles(t *testing.T) {
	g := NewGenerator(7)
	tc, ok := findTrialChamber(g)
	if !ok {
		t.Fatal("no trial chamber found in 40x40 cells")
	}
	pieces := g.AssembleTrialChamber(tc)
	if len(pieces) < 5 {
		t.Fatalf("trial chamber assembled only %d pieces", len(pieces))
	}
	// Bounding box + start-pool check.
	minX, minY, minZ := 1<<30, 1<<30, 1<<30
	maxX, maxY, maxZ := -(1 << 30), -(1 << 30), -(1 << 30)
	haveStart := false
	for _, p := range pieces {
		if p.Tmpl.name == "trial_chambers/chamber/end" {
			haveStart = true
		}
		if p.OX < minX {
			minX = p.OX
		}
		if p.OZ < minZ {
			minZ = p.OZ
		}
		if p.OY < minY {
			minY = p.OY
		}
		if p.OX > maxX {
			maxX = p.OX
		}
		if p.OZ > maxZ {
			maxZ = p.OZ
		}
		if p.OY > maxY {
			maxY = p.OY
		}
	}
	chests := g.TrialChamberChests(tc)
	t.Logf("chamber at %d,%d,%d: %d pieces, span X=%d Z=%d Y=%d, %d chests, startPiece=%v",
		tc.X, tc.Y, tc.Z, len(pieces), maxX-minX, maxZ-minZ, maxY-minY, len(chests), haveStart)
	for _, c := range chests {
		if c.Table == "" {
			t.Fatalf("chest at %d,%d,%d has no table", c.X, c.Y, c.Z)
		}
	}
}
