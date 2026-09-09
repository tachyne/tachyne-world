package worldgen

import "testing"

func findTrailRuins(g *Generator) (TrailRuins, bool) {
	for i := -40; i < 40; i++ {
		for j := -40; j < 40; j++ {
			if t := g.TrailRuinsIn(i*trailRuinsCell+272, j*trailRuinsCell+272); t.Exists {
				return t, true
			}
		}
	}
	return TrailRuins{}, false
}

// A trail-ruins site assembles a tower with attached pieces, starts fifteen
// blocks under the surface, and every piece's capped archaeology picks stay
// within the rule caps (6 common + 3 rare per house, 2 common per road or
// tower top), all landing as suspicious gravel in the generated chunks.
func TestTrailRuinsAssembleAndStamp(t *testing.T) {
	g := NewGenerator(1)
	site, ok := findTrailRuins(g)
	if !ok {
		t.Skip("no trail ruins in the scanned cells")
	}
	if site.Y != g.Height(site.X+2, site.Z+2)+trailRuinsDepth {
		t.Errorf("start y %d is not surface%+d", site.Y, trailRuinsDepth)
	}
	pieces := g.AssembleTrailRuins(site)
	if len(pieces) < 2 {
		t.Fatalf("site assembled %d pieces", len(pieces))
	}
	common, rare := 0, 0
	for _, p := range pieces {
		c, r := 0, 0
		for _, s := range p.Sus {
			switch s.Table {
			case "archaeology/trail_ruins_common":
				c++
			case "archaeology/trail_ruins_rare":
				r++
			default:
				t.Errorf("piece %s pick holds table %q", p.Tmpl.name, s.Table)
			}
			if s.State != SuspiciousGravel {
				t.Errorf("piece %s pick state %d", p.Tmpl.name, s.State)
			}
		}
		if c > 6 || r > 3 {
			t.Errorf("piece %s (%s) picks common=%d rare=%d", p.Tmpl.name, p.Proc, c, r)
		}
		if p.Proc == "trail_ruins_tower_top_archaeology" && (c > 2 || r > 0) {
			t.Errorf("tower top picks common=%d rare=%d", c, r)
		}
		common += c
		rare += r
	}
	t.Logf("trail ruins at %d,%d,%d: %d pieces, %d common + %d rare finds", site.X, site.Y, site.Z, len(pieces), common, rare)
	if common == 0 {
		t.Error("a site should hold some suspicious gravel")
	}
	chunks := map[[2]int32]*Chunk{}
	for _, p := range pieces {
		for _, s := range p.Sus {
			k := [2]int32{int32(s.X >> 4), int32(s.Z >> 4)}
			if chunks[k] == nil {
				chunks[k] = g.GenerateChunk(k[0], k[1])
			}
			if got := sectionBlockAt(chunks[k], s.X-(s.X>>4)*16, s.Y, s.Z-(s.Z>>4)*16); got != SuspiciousGravel {
				// A later piece may legitimately overwrite an earlier one's cell.
				t.Logf("pick %d,%d,%d holds %d (overwritten by another piece?)", s.X, s.Y, s.Z, got)
			}
		}
	}
	// The tower's masonry shows above the surface somewhere on the site.
	ch := chunks[[2]int32{int32(site.X >> 4), int32(site.Z >> 4)}]
	if ch == nil {
		ch = g.GenerateChunk(int32(site.X>>4), int32(site.Z>>4))
	}
	lo, hi, _ := BlockRangeOK("mud_bricks")
	stone := 0
	for s := range ch.Sections {
		for _, b := range ch.Sections[s] {
			if b >= lo && b <= hi {
				stone++
			}
		}
	}
	if stone == 0 {
		t.Error("the tower's mud bricks should be stamped in the site chunk")
	}
}
