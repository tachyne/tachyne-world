package worldgen

import "testing"

// findMonument is the first monument the seed rolls.
func findMonument(g *Generator) Monument {
	for i := 0; i < 90; i++ {
		for j := 0; j < 90; j++ {
			if m := g.MonumentIn(i*monumentCell+224, j*monumentCell+224); m.Exists {
				return m
			}
		}
	}
	return Monument{}
}

// The room graph keeps every room reachable from the entry through its
// open connections, claims the core's eight cells, and the fitters hand
// every other grid room to exactly one piece.
func TestMonumentRoomGraph(t *testing.T) {
	for seed := int64(1); seed <= 40; seed++ {
		defs, source, core := monRoomGraph(newTreeRNG(seed, 0, 0))
		reach := map[*monDef]bool{source: true}
		queue := []*monDef{source}
		for len(queue) > 0 {
			d := queue[0]
			queue = queue[1:]
			for f := 0; f < 6; f++ {
				if c := d.connections[f]; c != nil && d.hasOpening[f] && !reach[c] {
					reach[c] = true
					queue = append(queue, c)
				}
			}
		}
		grid := 0
		for _, d := range defs {
			if d.isSpecial() {
				continue
			}
			grid++
			if !reach[d] {
				t.Errorf("seed %d: room %d cut off from the entry", seed, d.index)
			}
		}
		if grid != 46 {
			t.Errorf("seed %d: %d grid rooms, want 46 (20+20+6)", seed, grid)
		}
		if x, y, z := core.index%5, core.index/25, core.index/5%5; y != 0 || z != 2 || x > 3 {
			t.Errorf("seed %d: core room at %d,%d,%d", seed, x, y, z)
		}
	}
	g := NewGenerator(1)
	for i := 0; i < 20; i++ {
		pl := g.monumentPlan(Monument{X: i * 1000, Y: 30, Z: -i * 700, Exists: true})
		owned := map[int]int{}
		for _, p := range pl.pieces {
			if p.def != nil {
				owned[p.def.index]++
			}
		}
		for idx, n := range owned {
			if n != 1 {
				t.Errorf("plan %d: room %d starts %d pieces", i, idx, n)
			}
		}
		if len(pl.elders) != 3 {
			t.Errorf("plan %d: %d elder cells, want 3", i, len(pl.elders))
		}
	}
}

// The building faces all four ways, and every piece of a plan lies inside
// the building's box.
func TestMonumentOrientations(t *testing.T) {
	g := NewGenerator(1)
	dirs := map[int]bool{}
	for i := 0; i < 40; i++ {
		m := Monument{X: i * 997, Y: 30, Z: i * 331, Exists: true}
		pl := g.monumentPlan(m)
		dirs[pl.bld.dir] = true
		b := pl.bld.bb
		for _, p := range pl.pieces {
			if p.bb.x0 < b.x0 || p.bb.x1 > b.x1 || p.bb.z0 < b.z0 || p.bb.z1 > b.z1 || p.bb.y0 < b.y0 || p.bb.y1 > b.y1 {
				t.Errorf("plan %d (dir %d): piece %d box %v outside the building %v", i, pl.bld.dir, p.kind, p.bb, b)
			}
		}
		for _, e := range pl.elders {
			if e[0] < b.x0 || e[0] > b.x1 || e[2] < b.z0 || e[2] > b.z1 {
				t.Errorf("plan %d: elder cell %v outside the building", i, e)
			}
		}
	}
	if len(dirs) != 4 {
		t.Errorf("orientations seen %v, want all four", dirs)
	}
}

// Through GenerateChunk: a monument has its rooms — the core's eight gold
// blocks inside dark prismarine, the elder cells open water — and some
// monument among the first few has a sponge room.
func TestMonumentInteriorGenerates(t *testing.T) {
	g := NewGenerator(1)
	mon := findMonument(g)
	if !mon.Exists {
		t.Skip("no monument rolled for this seed")
	}
	chunks := map[[2]int32]*Chunk{}
	at := func(x, y, z int) uint32 {
		k := [2]int32{int32(floorDiv16(x)), int32(floorDiv16(z))}
		ch := chunks[k]
		if ch == nil {
			ch = g.GenerateChunk(k[0], k[1])
			chunks[k] = ch
		}
		return sectionBlockAt(ch, x-int(k[0])*16, y, z-int(k[1])*16)
	}
	gold, dark := 0, 0
	for x := mon.X - 30; x <= mon.X+30; x++ {
		for z := mon.Z - 30; z <= mon.Z+30; z++ {
			for y := mon.Y; y <= mon.Y+23; y++ {
				switch at(x, y, z) {
				case mGold:
					gold++
				case mBlack:
					dark++
				}
			}
		}
	}
	if gold != 8 {
		t.Errorf("gold = %d, want the core room's 8", gold)
	}
	if dark < 100 {
		t.Errorf("only %d dark prismarine: the rooms are missing", dark)
	}
	elders := g.MonumentElders(mon)
	if len(elders) != 3 {
		t.Fatalf("%d elder cells, want 3", len(elders))
	}
	for _, e := range elders {
		if s := at(e[0], e[1], e[2]); s != Water && s != Air {
			t.Errorf("elder cell %v is %d, not open water", e, s)
		}
	}
}

// A monument with a player's block in its footprint keeps the old layout:
// the gold in the central pillar, no elder cells from the rooms.
func TestMonumentTouchedKeepsOldLayout(t *testing.T) {
	g := NewGenerator(1)
	mon := findMonument(g)
	if !mon.Exists {
		t.Skip("no monument rolled for this seed")
	}
	edits := map[[3]int]uint32{{mon.X + 20, mon.Y + 3, mon.Z - 20}: BlockBase("oak_planks")}
	g.SetEditLookup(func(x, y, z int) (uint32, bool) { s, ok := edits[[3]int{x, y, z}]; return s, ok })
	g.SetEditRegion(func(cx, cz int32, fn func(x, y, z int, s uint32)) {
		for p, s := range edits {
			if int32(floorDiv16(p[0])) == cx && int32(floorDiv16(p[2])) == cz {
				fn(p[0], p[1], p[2], s)
			}
		}
	})
	if e := g.MonumentElders(mon); e != nil {
		t.Errorf("a touched monument still reports room elder cells %v", e)
	}
	// The old pillar's gold sits at local (28..29, 4..5, 28..29) off the
	// min corner.
	x, y, z := mon.X-monumentHalf+28, mon.Y+4, mon.Z-monumentHalf+28
	ch := g.GenerateChunk(int32(floorDiv16(x)), int32(floorDiv16(z)))
	if s := sectionBlockAt(ch, x-floorDiv16(x)*16, y, z-floorDiv16(z)*16); s != mGold {
		t.Errorf("old-layout gold cell holds %d", s)
	}
}
