package worldgen

import "testing"

// findRuin scans placement cells for the first site of the wanted kind.
func findRuin(g *Generator, warm bool, largeOnly bool) (OceanRuins, bool) {
	for i := -60; i < 60; i++ {
		for j := -60; j < 60; j++ {
			s := g.OceanRuinsIn(i*oceanRuinCell+160, j*oceanRuinCell+160)
			if s.Exists && s.Warm == warm && (!largeOnly || s.Pieces[0].Large) {
				return s, true
			}
		}
	}
	return OceanRuins{}, false
}

// pieceChunks generates the chunks a piece of the given footprint touches.
func pieceChunks(g *Generator, p OceanRuinPiece, span int) map[[2]int32]*Chunk {
	out := map[[2]int32]*Chunk{}
	for _, wx := range []int{p.X, p.X + span - 1} {
		for _, wz := range []int{p.Z, p.Z + span - 1} {
			k := [2]int32{int32(wx >> 4), int32(wz >> 4)}
			if _, ok := out[k]; !ok {
				out[k] = g.GenerateChunk(k[0], k[1])
			}
		}
	}
	return out
}

func blockIn(chunks map[[2]int32]*Chunk, wx, wy, wz int) uint32 {
	ch := chunks[[2]int32{int32(wx >> 4), int32(wz >> 4)}]
	if ch == nil {
		return Air
	}
	return sectionBlockAt(ch, wx-(wx>>4)*16, wy, wz-(wz>>4)*16)
}

// A cold ruin layers a brick piece with its cracked and mossy overlays; each
// piece rots by its own integrity, keeps its chest and turns at most five
// gravel blocks suspicious. The stamped chunk carries the masonry, the chest
// (waterlogged under the sea) and the suspicious gravel.
func TestOceanRuinColdStamps(t *testing.T) {
	g := NewGenerator(1)
	site, ok := findRuin(g, false, false)
	if !ok {
		t.Skip("no cold ocean ruin in the scanned cells")
	}
	if len(site.Pieces)%3 != 0 {
		t.Fatalf("cold ruin pieces come in brick/cracked/mossy triples, got %d", len(site.Pieces))
	}
	p := site.Pieces[0]
	if p.Integrity != 0.8 && p.Integrity != 0.9 {
		t.Errorf("first piece integrity %v", p.Integrity)
	}
	if site.Pieces[1].Integrity != 0.7 || site.Pieces[2].Integrity != 0.5 {
		t.Errorf("overlay integrities %v %v", site.Pieces[1].Integrity, site.Pieces[2].Integrity)
	}
	for _, pc := range site.Pieces {
		if len(pc.Sus) > oceanRuinSusCap {
			t.Errorf("%s seeds %d suspicious blocks (cap %d)", pc.Tmpl, len(pc.Sus), oceanRuinSusCap)
		}
	}
	chunks := pieceChunks(g, p, 7)
	bricks, chests, sus := 0, 0, 0
	brickLo, brickHi, _ := BlockRangeOK("stone_bricks")
	crackedLo, crackedHi, _ := BlockRangeOK("cracked_stone_bricks")
	mossyLo, mossyHi, _ := BlockRangeOK("mossy_stone_bricks")
	for _, ch := range chunks {
		for s := range ch.Sections {
			for _, b := range ch.Sections[s] {
				switch {
				case (b >= brickLo && b <= brickHi) || (b >= crackedLo && b <= crackedHi) || (b >= mossyLo && b <= mossyHi):
					bricks++
				case b == chestWaterlogged || b == ChestNorth:
					chests++
				case b == SuspiciousGravel:
					sus++
				}
			}
		}
	}
	t.Logf("cold ruin %s at %d,%d,%d pieces=%d bricks=%d chests=%d sus=%d", p.Tmpl, p.X, p.Y, p.Z, len(site.Pieces), bricks, chests, sus)
	if bricks == 0 {
		t.Error("ruin masonry should survive generation")
	}
	if chests == 0 {
		t.Error("the ruin's chest should be stamped")
	}
	for _, c := range p.Chests {
		if got := blockIn(chunks, c.X, c.Y, c.Z); got != chestWaterlogged && got != ChestNorth {
			t.Errorf("chest cell %v holds %d", c, got)
		}
		if c.Table != "chests/underwater_ruin_small" && c.Table != "chests/underwater_ruin_big" {
			t.Errorf("chest table %q", c.Table)
		}
	}
	// The overlays' suspicious gravel may be overwritten by a later layer's
	// block; the LAST piece's picks are what the world ends up holding.
	last := site.Pieces[len(site.Pieces)-1]
	for _, s := range last.Sus {
		if got := blockIn(chunks, s[0], s[1], s[2]); got != SuspiciousGravel {
			t.Errorf("suspicious cell %v holds %d", s, got)
		}
	}
}

// A warm ruin is one sandstone piece per site; a large one drags a cluster of
// small ones behind it, none overlapping the big piece's box.
func TestOceanRuinWarmCluster(t *testing.T) {
	g := NewGenerator(1)
	site, ok := findRuin(g, true, true)
	if !ok {
		t.Skip("no large warm ocean ruin in the scanned cells")
	}
	p := site.Pieces[0]
	if !p.Large || p.Integrity != 0.9 {
		t.Fatalf("large piece %+v", p)
	}
	for _, pc := range site.Pieces[1:] {
		if pc.Large {
			t.Errorf("cluster piece %s is large", pc.Tmpl)
		}
		if pc.X+5 >= p.X && pc.X <= p.X+15 && pc.Z+6 >= p.Z && pc.Z <= p.Z+15 {
			t.Errorf("cluster piece at %d,%d overlaps the big box at %d,%d", pc.X, pc.Z, p.X, p.Z)
		}
		if len(pc.Sus) > oceanRuinSusCap {
			t.Errorf("%s seeds %d suspicious blocks", pc.Tmpl, len(pc.Sus))
		}
	}
	chunks := pieceChunks(g, p, 16)
	sandstone := 0
	lo, hi, _ := BlockRangeOK("cut_sandstone")
	for _, ch := range chunks {
		for s := range ch.Sections {
			for _, b := range ch.Sections[s] {
				if b >= lo && b <= hi {
					sandstone++
				}
			}
		}
	}
	for _, s := range p.Sus {
		if got := blockIn(chunks, s[0], s[1], s[2]); got != SuspiciousSand {
			t.Errorf("suspicious cell %v holds %d", s, got)
		}
	}
	t.Logf("warm ruin %s at %d,%d,%d cluster=%d sandstone=%d drowned=%d", p.Tmpl, p.X, p.Y, p.Z, len(site.Pieces)-1, sandstone, len(p.Drowned))
	if sandstone == 0 {
		t.Error("warm ruin sandstone should survive generation")
	}
}

// The dolphin's treasure is whichever of wreck or ruin is nearer.
func TestNearestDolphinTreasure(t *testing.T) {
	g := NewGenerator(1)
	site, ok := findRuin(g, false, false)
	if !ok {
		t.Skip("no ocean ruin in the scanned cells")
	}
	x, z, ok := g.NearestDolphinTreasure(site.X+3, site.Z-2, 64)
	if !ok || x != site.X || z != site.Z {
		t.Errorf("treasure next to a ruin = %d,%d ok=%v, want %d,%d", x, z, ok, site.X, site.Z)
	}
}
