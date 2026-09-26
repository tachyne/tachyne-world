package worldgen

import (
	"reflect"
	"testing"
)

// editOverlay feeds generation a fixed set of edits, as the world's guard
// snapshot does.
func editOverlay(g *Generator, edits map[[3]int]uint32) {
	g.SetEditLookup(func(x, y, z int) (uint32, bool) { s, ok := edits[[3]int{x, y, z}]; return s, ok })
	g.SetEditRegion(func(cx, cz int32, fn func(x, y, z int, s uint32)) {
		for p, s := range edits {
			if int32(floorDiv16(p[0])) == cx && int32(floorDiv16(p[2])) == cz {
				fn(p[0], p[1], p[2], s)
			}
		}
	})
}

// sulfurSpringCells replays chunk (ox, oz)'s ROOTED_SULFUR_SPRING placements
// — the first of sulfurFeatures' draws — over pure terrain, and returns the
// cells they changed and the next draw after them.
func sulfurSpringCells(g *Generator, ox, oz int) (map[[3]int]uint32, int) {
	view := func() *owRegion {
		return &owRegion{g: g, ch: NewChunk(g.sections), baseX: ox, baseZ: oz,
			cols: map[[2]int]column{}, capture: map[[3]int]uint32{}}
	}
	reg := view()
	if !reg.chunkHasSulfurClimate(ox, oz) {
		return nil, 0
	}
	r := newTreeRNG(g.seed^0x5A1FE, ox, oz)
	rangeY := func() int { return MinY + r.Intn(256-MinY+1) }
	for i, n := 0, 1+r.Intn(2); i < n; i++ {
		x, y, z := ox+r.Intn(16), rangeY(), oz+r.Intn(16)
		if cy, ok := reg.scanFor(x, y, z, +1, 12, solid); ok && reg.caveBiomeAt(x, cy-1, z) == sulfurCaves {
			g.rootedSulfurSpring(r, reg, x, cy-1, z)
		}
	}
	pure := view()
	cells := map[[3]int]uint32{}
	for p, s := range reg.capture {
		if pure.read(p[0], p[1], p[2]) != s {
			cells[p] = s
		}
	}
	return cells, r.Intn(1 << 30)
}

// findSulfurSpring finds a chunk near the sulfur caves whose spring places,
// with its changed cells and the chunks they fall in.
func findSulfurSpring(t *testing.T) (int64, int, int, map[[3]int]uint32, map[[2]int32]bool) {
	for _, seed := range []int64{5, 1, 2} {
		g := NewGenerator(seed)
		x, z, ok := findSulfurColumn(g, 300)
		if !ok {
			continue
		}
		for d := 0; d < 12; d++ {
			for dx := -d; dx <= d; dx++ {
				for dz := -d; dz <= d; dz++ {
					if max(abs(dx), abs(dz)) != d {
						continue
					}
					ox, oz := (x>>4+dx)*16, (z>>4+dz)*16
					cells, _ := sulfurSpringCells(g, ox, oz)
					tuff := 0
					for _, s := range cells {
						if s == stoneTuff {
							tuff++
						}
					}
					if tuff == 0 {
						continue
					}
					chunks := map[[2]int32]bool{}
					for p := range cells {
						chunks[[2]int32{int32(floorDiv16(p[0])), int32(floorDiv16(p[2]))}] = true
					}
					return seed, ox, oz, cells, chunks
				}
			}
		}
	}
	t.Fatal("no sulfur spring near the sulfur caves for seeds 5, 1, 2")
	return 0, 0, 0, nil, nil
}

// springLanded counts the spring's changed cells that hold its block in the
// generated chunks.
func springLanded(gen map[[2]int32]*Chunk, cells map[[3]int]uint32) int {
	n := 0
	for p, s := range cells {
		ch := gen[[2]int32{int32(floorDiv16(p[0])), int32(floorDiv16(p[2]))}]
		if sectionBlockAt(ch, p[0]-floorDiv16(p[0])*16, p[1], p[2]-floorDiv16(p[2])*16) == s {
			n++
		}
	}
	return n
}

// A rooted sulfur spring is left out whole — tuff, template and roots, in
// its own chunk and its neighbours — where a player built or dug in its
// box, and places as before where nobody did; chunks away from the edit
// generate byte for byte as they did.
func TestSulfurSpringGuard(t *testing.T) {
	seed, ox, oz, cells, chunks := findSulfurSpring(t)
	generate := func(edits map[[3]int]uint32) map[[2]int32]*Chunk {
		g := NewGenerator(seed)
		if edits != nil {
			editOverlay(g, edits)
		}
		out := map[[2]int32]*Chunk{}
		for c := range chunks {
			out[c] = g.GenerateChunk(c[0], c[1])
		}
		return out
	}
	base := generate(nil)
	landed := springLanded(base, cells)
	t.Logf("seed %d: spring from chunk (%d,%d), %d changed cells over %d chunks, %d landed", seed, ox>>4, oz>>4, len(cells), len(chunks), landed)
	if landed*2 < len(cells) {
		t.Fatalf("with no edits only %d of %d spring cells landed", landed, len(cells))
	}

	// One stone brick where the spring put tuff; one dug cell where the
	// terrain was solid and the spring changed it (its body).
	var brick, dig [3]int
	haveBrick, haveDig := false, false
	pure := &owRegion{g: NewGenerator(seed), ch: NewChunk(SectionCount), cols: map[[2]int]column{}, capture: map[[3]int]uint32{}}
	for p, s := range cells {
		if s == stoneTuff && !haveBrick {
			brick, haveBrick = p, true
		}
		if solid(pure.read(p[0], p[1], p[2])) && (!haveDig || p[1] < dig[1]) {
			dig, haveDig = p, true
		}
	}
	if !haveDig {
		t.Fatal("the spring changed no solid cell")
	}
	for _, e := range []struct {
		name string
		at   [3]int
		s    uint32
	}{{"brick", brick, BlockBase("stone_bricks")}, {"dug", dig, Air}} {
		got := generate(map[[3]int]uint32{e.at: e.s})
		if n := springLanded(got, cells); n != 0 {
			t.Errorf("%s at %v: %d of %d spring cells still placed", e.name, e.at, n, len(cells))
		}
	}

	// The world's own change (water) does not count, and an edit far away
	// changes nothing.
	for name, edits := range map[string]map[[3]int]uint32{
		"water":   {brick: WaterBase},
		"far off": {{ox + 4000, 40, oz + 4000}: BlockBase("stone_bricks")},
	} {
		got := generate(edits)
		for c, ch := range got {
			if !reflect.DeepEqual(ch, base[c]) {
				t.Errorf("%s: chunk %v changed", name, c)
			}
		}
	}

	// A chunk four away from the brick: the same bytes with the brick as
	// without it.
	far := [2]int32{int32(floorDiv16(brick[0])) + 4, int32(floorDiv16(brick[2]))}
	plain := NewGenerator(seed).GenerateChunk(far[0], far[1])
	g := NewGenerator(seed)
	editOverlay(g, map[[3]int]uint32{brick: BlockBase("stone_bricks")})
	if !reflect.DeepEqual(g.GenerateChunk(far[0], far[1]), plain) {
		t.Errorf("chunk %v, four from the brick, changed", far)
	}
}

// A guarded spring makes every draw an unguarded one does: the chunk's
// later features see the same random stream.
func TestSulfurSpringGuardDraws(t *testing.T) {
	seed, ox, oz, cells, _ := findSulfurSpring(t)
	var at [3]int
	for p := range cells {
		at = p
		break
	}
	_, want := sulfurSpringCells(NewGenerator(seed), ox, oz)
	g := NewGenerator(seed)
	editOverlay(g, map[[3]int]uint32{at: Air})
	got, next := sulfurSpringCells(g, ox, oz)
	if len(got) != 0 {
		t.Errorf("guarded spring changed %d cells", len(got))
	}
	if next != want {
		t.Errorf("the draw after a guarded spring is %d, unguarded %d", next, want)
	}
}

// A sulfur pool with a build or a dug cell in its box places nothing and
// draws the same as one that goes in.
func TestSulfurPoolGuard(t *testing.T) {
	floor := func() *owRegion {
		ch := NewChunk(SectionCount)
		for y := MinY; y < MinY+len(ch.Sections)*16; y++ {
			for lx := 0; lx < 16; lx++ {
				for lz := 0; lz < 16; lz++ {
					s := uint32(Air)
					if y < 0 {
						s = SulfurBlock
					}
					setSectionBlock(ch, lx, y, lz, s, true)
				}
			}
		}
		return &owRegion{ch: ch, cols: map[[2]int]column{}}
	}
	count := func(reg *owRegion, want uint32) int {
		n := 0
		for s := range reg.ch.Sections {
			for _, b := range reg.ch.Sections[s] {
				if b == want {
					n++
				}
			}
		}
		return n
	}
	for name, edit := range map[string]uint32{"brick": BlockBase("stone_bricks"), "dug": Air} {
		for seed := int64(0); seed < 20; seed++ {
			g := NewGenerator(1)
			open := floor()
			open.g = g
			r := newTreeRNG(seed, 0, 0)
			g.sulfurPool(r, open, 8, -1, 8)
			want := r.Intn(1 << 30)

			g2 := NewGenerator(1)
			editOverlay(g2, map[[3]int]uint32{{1, -3, 14}: edit}) // a corner of the box
			shut := floor()
			shut.g = g2
			r2 := newTreeRNG(seed, 0, 0)
			g2.sulfurPool(r2, shut, 8, -1, 8)
			if got := r2.Intn(1 << 30); got != want {
				t.Errorf("%s, seed %d: next draw %d, unguarded %d", name, seed, got, want)
			}
			if w := count(shut, Water); w != 0 {
				t.Errorf("%s, seed %d: %d water in a guarded pool (unguarded %d)", name, seed, w, count(open, Water))
			}
			if n := count(shut, potentSulfurWet); n != 0 {
				t.Errorf("%s, seed %d: wet potent sulfur in a guarded pool", name, seed)
			}
		}
	}
}
