package worldgen

import "testing"

// The spike shuffle runs on java.util.Random, as vanilla's does.
func TestJavaRandom(t *testing.T) {
	if got := newJavaRandom(0).nextLong(); got != -4962768465676381896 {
		t.Fatalf("new Random(0).nextLong() = %d", got)
	}
}

// EndSpikeFeature's layout: the ring positions, sizes 0–9 each once, radius
// 2 + size/3, height 76 + 3·size, sizes 1 and 2 caged.
func TestVanillaEndSpikes(t *testing.T) {
	spikes := vanillaEndSpikes(12345)
	if len(spikes) != 10 {
		t.Fatalf("%d spikes", len(spikes))
	}
	if spikes[0].X != 42 || spikes[0].Z != 0 || spikes[5].X != -42 || spikes[5].Z != -1 {
		t.Errorf("spike 0 at %d,%d and 5 at %d,%d, want 42,0 and -42,-1", spikes[0].X, spikes[0].Z, spikes[5].X, spikes[5].Z)
	}
	seen := map[int]bool{}
	guarded := 0
	for _, s := range spikes {
		size := (s.Height - 76) / 3
		if (s.Height-76)%3 != 0 || size < 0 || size > 9 || seen[size] {
			t.Fatalf("spike height %d is no fresh size", s.Height)
		}
		seen[size] = true
		if s.Radius != 2+size/3 {
			t.Errorf("size %d: radius %d", size, s.Radius)
		}
		if s.Guarded != (size == 1 || size == 2) {
			t.Errorf("size %d: guarded %v", size, s.Guarded)
		}
		if s.Guarded {
			guarded++
		}
	}
	if guarded != 2 {
		t.Errorf("%d guarded spikes, want 2", guarded)
	}
	again := vanillaEndSpikes(12345)
	for i := range spikes {
		if spikes[i] != again[i] {
			t.Fatal("the layout is not a function of the seed")
		}
	}
}

// endAt reads a generated End cell.
func endAt(g *Generator, cache map[[2]int32]*Chunk, x, y, z int) uint32 {
	k := [2]int32{int32(floorDiv16(x)), int32(floorDiv16(z))}
	ch, ok := cache[k]
	if !ok {
		ch = g.GenerateChunk(k[0], k[1])
		cache[k] = ch
	}
	return sectionBlockAt(ch, x-int(k[0])*16, y, z-int(k[1])*16)
}

// Through GenerateChunk: each spike is obsidian from the floor to its
// height, capped with bedrock and fire, and the guarded ones caged.
func TestEndSpikesGenerate(t *testing.T) {
	g := NewEndGenerator(7)
	cache := map[[2]int32]*Chunk{}
	for i, s := range g.EndSpikes() {
		if s.Legacy {
			t.Fatalf("spike %d is the old pillar with no edits", i)
		}
		for _, y := range []int{MinY + 1, 0, s.Height - 1} {
			if b := endAt(g, cache, s.X, y, s.Z); b != Obsidian {
				t.Errorf("spike %d: %d at y=%d, want obsidian", i, b, y)
			}
		}
		if b := endAt(g, cache, s.X+s.Radius, s.Height-1, s.Z); b != Obsidian {
			t.Errorf("spike %d: its rim at radius %d is %d", i, s.Radius, b)
		}
		if b := endAt(g, cache, s.X, s.Height, s.Z); b != Bedrock {
			t.Errorf("spike %d: cap %d, want bedrock", i, b)
		}
		if b := endAt(g, cache, s.X, s.Height+1, s.Z); b != endFire {
			t.Errorf("spike %d: %d over the cap, want fire", i, b)
		}
		lo, hi := BlockRange("iron_bars")
		b := endAt(g, cache, s.X+2, s.Height+3, s.Z)
		if caged := b >= lo && b <= hi; caged != s.Guarded {
			t.Errorf("spike %d: guarded %v, iron bars over it %v", i, s.Guarded, caged)
		}
	}
}

// A spike a player built on keeps the old pillar, crystal and all.
func TestEndSpikeKeptWhenTouched(t *testing.T) {
	g := NewEndGenerator(7)
	s := g.EndSpikes()[3]
	g2 := NewEndGenerator(7)
	setTestEdits(g2, map[[3]int]uint32{{s.X, s.Height + 2, s.Z}: BlockBase("stone_bricks")})
	got := g2.EndSpikes()[3]
	if !got.Legacy || got != legacyEndSpike(3) {
		t.Fatalf("touched spike: %+v, want the old pillar", got)
	}
	if got.CrystalY() != got.Height {
		t.Error("the old pillar's crystal stands on its obsidian")
	}
	cache := map[[2]int32]*Chunk{}
	if b := endAt(g2, cache, got.X, got.Height-1, got.Z); b != Obsidian {
		t.Errorf("the old pillar's top is %d", b)
	}
}

// EndPodiumFeature: the bowl, rim, pillar and torches; the portal only when
// active.
func TestEndPodiumShape(t *testing.T) {
	final := func(active bool) map[[3]int]uint32 {
		m := map[[3]int]uint32{}
		for _, c := range EndPodium(0, 60, 0, active) {
			m[[3]int{c.X, c.Y, c.Z}] = c.State
		}
		return m
	}
	on, off := final(true), final(false)
	portal, rim := 0, 0
	for p, s := range on {
		if p[1] != 60 {
			continue
		}
		switch s {
		case EndPortalBlock:
			portal++
		case Bedrock:
			if p != [3]int{0, 60, 0} {
				rim++
			}
		}
	}
	if portal != 20 || rim != 16 {
		t.Errorf("active podium: %d portal cells, %d rim cells, want 20 and 16", portal, rim)
	}
	if off[[3]int{1, 60, 0}] != Air || off[[3]int{3, 60, 0}] != Bedrock || off[[3]int{1, 59, 0}] != Bedrock || off[[3]int{3, 59, 0}] != EndStone {
		t.Error("inactive podium: air inside the rim, bedrock under it, end stone out to the rim")
	}
	for k := 0; k < 4; k++ {
		if on[[3]int{0, 60 + k, 0}] != Bedrock {
			t.Errorf("pillar block %d is %d", k, on[[3]int{0, 60 + k, 0}])
		}
	}
	if on[[3]int{0, 62, -1}] != withProps("wall_torch", "facing", "north") {
		t.Error("no north torch on the pillar")
	}
}

// Through GenerateChunk: a new End has the inactive podium at the island's
// top at 0,0 — and not where a player built there.
func TestEndPodiumPreplaced(t *testing.T) {
	g := NewEndGenerator(7)
	_, oy, _ := g.EndExitPortal()
	cache := map[[2]int32]*Chunk{}
	for k := 0; k < 4; k++ {
		if b := endAt(g, cache, 0, oy+k, 0); b != Bedrock {
			t.Errorf("pillar block %d is %d", k, b)
		}
	}
	if b := endAt(g, cache, 1, oy, 0); b != Air {
		t.Errorf("inside the rim: %d, want air (inactive)", b)
	}
	if b := endAt(g, cache, -3, oy, 0); b != Bedrock {
		t.Errorf("the rim: %d", b)
	}
	g2 := NewEndGenerator(7)
	setTestEdits(g2, map[[3]int]uint32{{2, oy + 1, 2}: BlockBase("stone_bricks")})
	cache2 := map[[2]int32]*Chunk{}
	if b := endAt(g2, cache2, 0, oy+3, 0); b == Bedrock {
		t.Error("the podium was pre-placed over a player's build")
	}
}
