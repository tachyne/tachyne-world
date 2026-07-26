package worldgen

import "testing"

func TestEndIslandShape(t *testing.T) {
	g := NewEndGenerator(7)
	ch := g.GenerateChunk(0, 0)
	counts := map[uint32]int{}
	for s := range ch.Sections {
		for _, b := range ch.Sections[s] {
			counts[b]++
		}
	}
	if counts[EndStone] < 5000 {
		t.Fatalf("origin chunk should be island interior, end stone=%d", counts[EndStone])
	}
	// Far chunk: pure void.
	far := g.GenerateChunk(40, 40)
	for s := range far.Sections {
		for _, b := range far.Sections[s] {
			if b != Air {
				t.Fatalf("void chunk contains %d", b)
			}
		}
	}
	// Pillars: obsidian on the ring.
	px := int(EndPillarRing * cos01(0))
	pz := int(EndPillarRing * sin01(0))
	if g.endBlock(px, 70, pz) != Obsidian {
		t.Fatalf("pillar 0 at (%d,%d) should be obsidian at y70, got %d", px, pz, g.endBlock(px, 70, pz))
	}
	if g.BiomeName(0, 0) != "minecraft:the_end" {
		t.Fatal("End biome wrong")
	}
}

// The outer islands: void inside the ring, scattered land beyond it. The End
// used to be one island of radius 95 and nothing else, which is why an end
// gateway would have dropped a player into the void.
func TestOuterEndIslands(t *testing.T) {
	g := NewEndGenerator(12345)

	// Nothing near the middle — the main island's falloff keeps it clear.
	for x := 150; x < 800; x += 7 {
		if _, _, ok := g.endOuterColumn(x, 0); ok {
			t.Fatalf("outer island found at x=%d, inside the void ring", x)
		}
	}

	// Land out beyond it, but far from continuous.
	solid, cols := 0, 0
	for x := 1500; x < 3500; x += 5 {
		for z := -300; z < 300; z += 5 {
			cols++
			if _, _, ok := g.endOuterColumn(x, z); ok {
				solid++
			}
		}
	}
	pct := 100 * float64(solid) / float64(cols)
	if pct < 3 {
		t.Errorf("%.1f%% of the outer End is land — too empty to glide between", pct)
	}
	if pct > 40 {
		t.Errorf("%.1f%% of the outer End is land — it should be scattered discs, not a continent", pct)
	}
}

// Islands are lenses: thin at the rim, thick in the middle, all within a band
// a player can land on.
func TestOuterIslandShape(t *testing.T) {
	g := NewEndGenerator(12345)
	thickest, minTop, maxTop := 0, 1<<30, 0
	for x := 1200; x < 3000; x += 3 {
		for z := -300; z < 300; z += 3 {
			top, bot, ok := g.endOuterColumn(x, z)
			if !ok {
				continue
			}
			if bot > top {
				t.Fatalf("island at %d,%d has bottom %d above top %d", x, z, bot, top)
			}
			if th := top - bot + 1; th > thickest {
				thickest = th
			}
			if top < minTop {
				minTop = top
			}
			if top > maxTop {
				maxTop = top
			}
		}
	}
	if thickest < 6 {
		t.Errorf("thickest island is %d blocks — they should have some body", thickest)
	}
	if minTop < 40 || maxTop > 90 {
		t.Errorf("island tops span %d..%d, want a band around the End's surface", minTop, maxTop)
	}
}

// floorDiv must round toward negative infinity, or the islands mirror across
// the axes instead of continuing.
func TestFloorDivRoundsDown(t *testing.T) {
	for _, c := range []struct{ a, b, want int }{
		{16, 8, 2}, {15, 8, 1}, {-1, 8, -1}, {-8, 8, -1}, {-9, 8, -2}, {0, 8, 0},
	} {
		if got := floorDiv(c.a, c.b); got != c.want {
			t.Errorf("floorDiv(%d,%d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
