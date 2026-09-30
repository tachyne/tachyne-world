package worldgen

import "testing"

func TestGeodesGenerate(t *testing.T) {
	g := NewGenerator(1)
	var basalt, calc, amethyst, budding, clusters int
	var geodeChunk [2]int32
	found := false
	for cx := int32(-15); cx <= 15 && !found; cx++ {
		for cz := int32(-15); cz <= 15; cz++ {
			ch := g.GenerateChunk(cx, cz)
			var b, c, a, bd, cl int
			for sec := range ch.Sections {
				for _, s := range ch.Sections[sec] {
					switch {
					case s == smoothBasalt:
						b++
					case s == calcite:
						c++
					case s == amethystBlock:
						a++
					case s == buddingAmethyst:
						bd++
					case s >= amethystClusterBase && s <= amethystClusterBase+11,
						s >= smallBudBase && s <= smallBudBase+11,
						s >= mediumBudBase && s <= mediumBudBase+11,
						s >= largeBudBase && s <= largeBudBase+11:
						cl++
					}
				}
			}
			basalt += b
			calc += c
			amethyst += a
			budding += bd
			clusters += cl
			if a > 0 && !found { // a chunk containing a geode's amethyst lining
				geodeChunk = [2]int32{cx, cz}
				found = true
			}
		}
	}
	if !found {
		t.Fatal("no geode found in 900 chunks (expected ~30)")
	}
	if basalt == 0 || calc == 0 || amethyst == 0 || budding == 0 || clusters == 0 {
		t.Errorf("geode layers incomplete: basalt=%d calcite=%d amethyst=%d budding=%d clusters=%d",
			basalt, calc, amethyst, budding, clusters)
	}
	t.Logf("basalt=%d calcite=%d amethyst=%d budding=%d clusters=%d (first geode chunk %v)",
		basalt, calc, amethyst, budding, clusters, geodeChunk)

	// Determinism: regenerating the same chunk yields identical geode blocks.
	a := g.GenerateChunk(geodeChunk[0], geodeChunk[1])
	b := NewGenerator(1).GenerateChunk(geodeChunk[0], geodeChunk[1])
	for sec := range a.Sections {
		for i := range a.Sections[sec] {
			if a.Sections[sec][i] != b.Sections[sec][i] {
				t.Fatalf("geode chunk not deterministic at sec %d idx %d", sec, i)
			}
		}
	}
}

// geodePlans is the first n geodes near the origin.
func geodePlans(g *Generator, n int) []*geodePlan {
	var out []*geodePlan
	for cx := int32(-30); cx <= 30 && len(out) < n; cx++ {
		for cz := int32(-30); cz <= 30 && len(out) < n; cz++ {
			if p := g.geodeIn(cx, cz); p != nil {
				out = append(out, p)
			}
		}
	}
	return out
}

// TestGeodeCrossChunkConsistent: a geode straddling a chunk border lands in
// every chunk it touches as its one plan says — each pass writes its part.
func TestGeodeCrossChunkConsistent(t *testing.T) {
	g := NewGenerator(1)
	for _, p := range geodePlans(g, 40) {
		if floorDiv16(p.x0) == floorDiv16(p.x1) {
			continue // not across an east-west border
		}
		chunks := map[[2]int32]*Chunk{}
		match, solidCells := 0, 0
		for c, s := range p.cells {
			if s == Air {
				continue // the hollow: later cave features may grow into it
			}
			k := [2]int32{int32(floorDiv16(c[0])), int32(floorDiv16(c[2]))}
			ch, ok := chunks[k]
			if !ok {
				ch = g.GenerateChunk(k[0], k[1])
				chunks[k] = ch
			}
			solidCells++
			if sectionBlockAt(ch, c[0]-int(k[0])*16, c[1], c[2]-int(k[1])*16) == s {
				match++
			}
		}
		if len(chunks) < 2 {
			continue
		}
		if match*10 < solidCells*9 {
			t.Fatalf("geode across %d chunks: %d of %d shell cells as planned", len(chunks), match, solidCells)
		}
		return
	}
	t.Skip("no geode across a chunk border near the origin")
}

// GeodeFeature's layers: the hollow, the amethyst lining with some budding
// amethyst, calcite and the smooth-basalt crust; every bud or cluster
// stands in the hollow facing away from a budding block; and most geodes
// are cracked open to the rock around them.
func TestGeodePlanLayers(t *testing.T) {
	g := NewGenerator(1)
	plans := geodePlans(g, 12)
	if len(plans) == 0 {
		t.Fatal("no geode near the origin")
	}
	budRanges := rangesOf("small_amethyst_bud", "medium_amethyst_bud", "large_amethyst_bud", "amethyst_cluster")
	cracked, buds, budding, lining := 0, 0, 0, 0
	for _, p := range plans {
		counts := map[uint32]int{}
		open := false
		for c, s := range p.cells {
			counts[s]++
			if s == Air {
				for _, d := range geodeDirections {
					if _, in := p.cells[[3]int{c[0] + d.dx, c[1] + d.dy, c[2] + d.dz}]; !in {
						open = true // the hollow reaches past the geode's own cells
					}
				}
				continue
			}
			if !inAnyRange(s, budRanges) {
				continue
			}
			info, _ := InfoForState(s)
			buds++
			face := GetProperty(info, s, "facing")
			for _, d := range geodeDirections {
				if d.name == face {
					if back := p.cells[[3]int{c[0] - d.dx, c[1] - d.dy, c[2] - d.dz}]; back != buddingAmethyst {
						t.Errorf("a bud (%d) facing %s grows from %d, not budding amethyst", s, face, back)
					}
				}
			}
		}
		if counts[Air] == 0 || counts[amethystBlock] == 0 || counts[calcite] == 0 || counts[smoothBasalt] == 0 {
			t.Errorf("geode layers: air %d amethyst %d calcite %d basalt %d", counts[Air], counts[amethystBlock], counts[calcite], counts[smoothBasalt])
		}
		budding += counts[buddingAmethyst]
		lining += counts[buddingAmethyst] + counts[amethystBlock]
		if open {
			cracked++
		}
	}
	if budding == 0 || buds == 0 {
		t.Errorf("%d budding amethyst, %d buds in %d geodes", budding, buds, len(plans))
	}
	if frac := float64(budding) / float64(lining); frac < 0.04 || frac > 0.14 {
		t.Errorf("budding amethyst is %.3f of the lining, want about 0.083", frac)
	}
	if cracked == 0 {
		t.Errorf("none of %d geodes is cracked open (vanilla: 95%%)", len(plans))
	}
	t.Logf("%d geodes: %d cracked, %d budding of %d lining, %d buds", len(plans), cracked, budding, lining, buds)
}

// A geode with a player's build or dug cell in its box is left out whole.
func TestGeodeBuildGuard(t *testing.T) {
	plans := geodePlans(NewGenerator(1), 1)
	if len(plans) == 0 {
		t.Skip("no geode near the origin")
	}
	p := plans[0]
	var at [3]int
	for c := range p.cells {
		at = c
		break
	}
	for name, e := range map[string]uint32{"build": BlockBase("stone_bricks"), "dug": Air} {
		g := NewGenerator(1)
		setTestEdits(g, map[[3]int]uint32{at: e})
		if q := g.geodeIn(p.cx, p.cz); q != nil {
			t.Errorf("%s in the geode: it is still placed", name)
		}
		ch := g.GenerateChunk(int32(floorDiv16(at[0])), int32(floorDiv16(at[2])))
		if s := sectionBlockAt(ch, at[0]-floorDiv16(at[0])*16, at[1], at[2]-floorDiv16(at[2])*16); s == p.cells[at] && s != Air && s != Stone && s != Deepslate {
			t.Errorf("%s in the geode: its cell still generates as planned (%d)", name, s)
		}
	}
}
