package worldgen

import (
	"strings"
	"testing"
)

// chunkCache generates each chunk a test reads once.
type chunkCache struct {
	g *Generator
	m map[[2]int]*Chunk
}

func newChunkCache(g *Generator) *chunkCache { return &chunkCache{g: g, m: map[[2]int]*Chunk{}} }

func (c *chunkCache) at(x, y, z int) uint32 {
	k := [2]int{x >> 4, z >> 4}
	ch := c.m[k]
	if ch == nil {
		ch = c.g.GenerateChunk(int32(k[0]), int32(k[1]))
		c.m[k] = ch
	}
	return sectionBlockAt(ch, x&15, y, z&15)
}

func nameOf(st uint32) string {
	n, _ := StateName(st)
	return n
}

func firstStronghold(g *Generator) (Stronghold, bool) {
	for i := -4; i <= 4; i++ {
		for j := -4; j <= 4; j++ {
			if st := g.StrongholdIn(i*strongholdCell+8, j*strongholdCell+8); st.Exists {
				return st, true
			}
		}
	}
	return Stronghold{}, false
}

// A stronghold is the full maze: over a few seeds every piece kind appears,
// and each one has exactly one start and one portal room behind a five-way
// crossing.
func TestStrongholdPieceSet(t *testing.T) {
	seen := map[string]int{}
	for seed := int64(1); seed <= 5; seed++ {
		st, ok := firstStronghold(NewGenerator(seed))
		if !ok {
			t.Fatalf("seed %d: no stronghold", seed)
		}
		c := st.PieceCounts()
		t.Logf("seed %d: %v", seed, c)
		if c["portal_room"] != 1 || c["start"] != 1 || c["five_crossing"] < 1 {
			t.Errorf("seed %d: want one start, one portal room, a five crossing; got %v", seed, c)
		}
		if c["library"] > 2 || c["prison_hall"] > 5 || c["room_crossing"] > 6 || c["chest_corridor"] > 4 {
			t.Errorf("seed %d: a capped piece went over its cap: %v", seed, c)
		}
		_, _, _, _, y1, _ := st.Bounds()
		if y1 > SeaLevel-10 {
			t.Errorf("seed %d: stronghold top at %d, above sea level less 10", seed, y1)
		}
		for k, n := range c {
			seen[k] += n
		}
	}
	for _, k := range []string{"straight", "prison_hall", "left_turn", "right_turn", "room_crossing",
		"straight_stairs_down", "stairs_down", "five_crossing", "chest_corridor", "library", "portal_room"} {
		if seen[k] == 0 {
			t.Errorf("no %s in five strongholds (%v)", k, seen)
		}
	}
}

// The portal room is stamped: twelve frames on its ring, the silverfish
// spawner on its stairs, the lava pool under the portal, and stone-brick
// walls weathered cracked, mossy and infested.
func TestStrongholdPortalRoomStamped(t *testing.T) {
	g := NewGenerator(7)
	st, ok := firstStronghold(g)
	if !ok {
		t.Fatal("no stronghold")
	}
	cc := newChunkCache(g)
	for i, f := range st.FramePositions(g.seed) {
		if got := cc.at(f.X, f.Y, f.Z); got != f.State || nameOf(got) != "end_portal_frame" {
			t.Errorf("frame %d at %d,%d,%d: got %s (%d), want state %d", i, f.X, f.Y, f.Z, nameOf(got), got, f.State)
		}
	}
	if sp, ok := st.Spawner(); !ok || cc.at(sp[0], sp[1], sp[2]) != Spawner {
		t.Errorf("portal room spawner at %v: got %s", sp, nameOf(cc.at(sp[0], sp[1], sp[2])))
	}
	if got := nameOf(cc.at(st.X, st.Y-2, st.Z)); got != "lava" {
		t.Errorf("under the portal: %s, want lava", got)
	}
	kinds := map[string]int{}
	for x := st.X - 8; x <= st.X+8; x++ {
		for z := st.Z - 8; z <= st.Z+8; z++ {
			for y := st.Y - 4; y <= st.Y+5; y++ {
				if n := nameOf(cc.at(x, y, z)); strings.Contains(n, "stone_bricks") {
					kinds[n]++
				}
			}
		}
	}
	for _, k := range []string{"stone_bricks", "mossy_stone_bricks", "cracked_stone_bricks"} {
		if kinds[k] == 0 {
			t.Errorf("no %s around the portal room: %v", k, kinds)
		}
	}
}

// Every chest the stronghold lists is a chest in the world, and the three
// stronghold tables all occur.
func TestStrongholdChests(t *testing.T) {
	tables := map[string]int{}
	for seed := int64(1); seed <= 5; seed++ {
		g := NewGenerator(seed)
		st, _ := firstStronghold(g)
		cc := newChunkCache(g)
		for _, c := range st.Chests() {
			tables[c.Table]++
			if n := nameOf(cc.at(c.X, c.Y, c.Z)); n != "chest" {
				t.Errorf("seed %d: %s chest at %d,%d,%d is %s", seed, c.Table, c.X, c.Y, c.Z, n)
			}
		}
	}
	t.Logf("chests: %v", tables)
	for _, k := range []string{"chests/stronghold_corridor", "chests/stronghold_library", "chests/stronghold_crossing"} {
		if tables[k] == 0 {
			t.Errorf("no %s chest in five strongholds", k)
		}
	}
}

// Stamping is chunk-order independent: a piece crossing a chunk border draws
// the same cells whichever chunk is generated.
func TestStrongholdStampDeterministic(t *testing.T) {
	g := NewGenerator(3)
	st, _ := firstStronghold(g)
	a, b := g.GenerateChunk(int32(st.X>>4), int32(st.Z>>4)), NewGenerator(3).GenerateChunk(int32(st.X>>4), int32(st.Z>>4))
	for s := range a.Sections {
		if a.Sections[s] != b.Sections[s] {
			t.Fatalf("section %d differs between generators", s)
		}
	}
}

// mineshafts returns up to n mineshafts of a seed, nearest the origin first.
func mineshafts(g *Generator, n, reach int, want func(Mineshaft) bool) []Mineshaft {
	var out []Mineshaft
	for r := 0; r <= reach && len(out) < n; r++ {
		for cx := -r; cx <= r; cx++ {
			for cz := -r; cz <= r; cz++ {
				if max(abs(cx), abs(cz)) != r {
					continue
				}
				if m := g.MineshaftAt(cx, cz); m.Exists && want(m) {
					out = append(out, m)
				}
			}
		}
	}
	return out
}

func TestMineshaftPieceSet(t *testing.T) {
	g := NewGenerator(7)
	shafts := mineshafts(g, 12, 80, func(m Mineshaft) bool { return !m.Mesa })
	if len(shafts) < 12 {
		t.Fatalf("only %d mineshafts within 80 chunks — odds broken", len(shafts))
	}
	total, spiders := map[string]int{}, 0
	for _, m := range shafts {
		c := m.PieceCounts()
		if c["room"] != 1 {
			t.Errorf("mineshaft at %d,%d: %d rooms", m.X, m.Z, c["room"])
		}
		if m.Y > SeaLevel-10 {
			t.Errorf("mineshaft at %d,%d floor %d: not sunk below sea level", m.X, m.Z, m.Y)
		}
		for k, n := range c {
			total[k] += n
		}
		spiders += m.SpiderCorridors()
	}
	t.Logf("12 mineshafts: %v, %d spider corridors", total, spiders)
	for _, k := range []string{"corridor", "crossing", "stairs"} {
		if total[k] == 0 {
			t.Errorf("no %s in twelve mineshafts", k)
		}
	}
	if spiders == 0 {
		t.Error("no cave spider corridor in twelve mineshafts")
	}
}

// A mineshaft is stamped: the room is hollow, corridors carry oak planks
// and fence supports and cobwebs, a nest corridor holds its spawner and a
// rolled chest minecart has its rail.
func TestMineshaftStamps(t *testing.T) {
	g := NewGenerator(7)
	cc := newChunkCache(g)
	shafts := mineshafts(g, 40, 120, func(m Mineshaft) bool { return !m.Mesa })
	m := shafts[0]
	if n := nameOf(cc.at(m.X+1, m.Y+1, m.Z+1)); n != "air" {
		t.Errorf("room at %d,%d,%d not hollow: %s", m.X, m.Y, m.Z, n)
	}
	blocks := map[string]int{}
	for _, p := range m.pieces {
		if p.kind != msCorridor {
			continue
		}
		b := p.box
		for x := b.x0 - 1; x <= b.x1+1; x++ {
			for z := b.z0 - 1; z <= b.z1+1; z++ {
				for y := b.y0 - 1; y <= b.y1; y++ {
					blocks[nameOf(cc.at(x, y, z))]++
				}
			}
		}
		if len(cc.m) > 80 {
			break
		}
	}
	t.Logf("corridor blocks: planks %d fence %d web %d rail %d torch %d log %d chain %d",
		blocks["oak_planks"], blocks["oak_fence"], blocks["cobweb"], blocks["rail"], blocks["wall_torch"], blocks["oak_log"], blocks["iron_chain"])
	for _, k := range []string{"oak_planks", "oak_fence", "cobweb"} {
		if blocks[k] == 0 {
			t.Errorf("no %s in the corridors", k)
		}
	}
	spawners, carts := 0, 0
	for _, m := range shafts {
		for _, s := range g.MineshaftSpawners(m) {
			if cc.at(s[0], s[1], s[2]) == Spawner {
				spawners++
			}
		}
		for _, c := range g.MineshaftCarts(m) {
			if strings.HasSuffix(nameOf(cc.at(c[0], c[1], c[2])), "rail") {
				carts++
			}
		}
		if spawners > 0 && carts > 0 {
			break
		}
	}
	if spawners == 0 {
		t.Error("no cave spider spawner stamped in forty mineshafts")
	}
	if carts == 0 {
		t.Error("no chest-minecart rail stamped in forty mineshafts")
	}
}

// In the badlands the mineshaft is the mesa one: dark oak, and lifted to
// between sea level and the surface.
func TestMineshaftMesa(t *testing.T) {
	var m Mineshaft
	var g *Generator
	for seed := int64(1); seed <= 6 && !m.Exists; seed++ {
		g = NewGenerator(seed)
		if found := mineshafts(g, 1, 160, func(m Mineshaft) bool { return m.Mesa }); len(found) > 0 {
			m = found[0]
		}
	}
	if !m.Exists {
		t.Skip("no badlands mineshaft within 160 chunks of six seeds")
	}
	if m.Y < SeaLevel-16 {
		t.Errorf("mesa mineshaft floor at %d: not lifted", m.Y)
	}
	cc := newChunkCache(g)
	blocks := map[string]int{}
	for _, p := range m.pieces {
		b := p.box
		for x := b.x0 - 1; x <= b.x1+1; x++ {
			for z := b.z0 - 1; z <= b.z1+1; z++ {
				for y := b.y0 - 1; y <= b.y1; y++ {
					blocks[nameOf(cc.at(x, y, z))]++
				}
			}
		}
		if len(cc.m) > 60 {
			break
		}
	}
	t.Logf("mesa blocks: dark oak planks %d fence %d log %d; oak planks %d fence %d",
		blocks["dark_oak_planks"], blocks["dark_oak_fence"], blocks["dark_oak_log"], blocks["oak_planks"], blocks["oak_fence"])
	if blocks["dark_oak_planks"] == 0 && blocks["dark_oak_fence"] == 0 {
		t.Error("mesa mineshaft has no dark oak")
	}
	if blocks["oak_fence"] > 0 {
		t.Error("mesa mineshaft carries oak fences")
	}
}
