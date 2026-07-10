package worldgen

import (
	"strings"
	"testing"
)

// TestBiomeVariety: the overworld must produce a rich spread of biomes, not the
// old handful. Sample a wide area and require a healthy distinct count plus a
// few signature families actually appearing.
func TestBiomeVariety(t *testing.T) {
	g := NewGenerator(1)
	seen := map[string]int{}
	for x := -2000; x <= 2000; x += 25 {
		for z := -2000; z <= 2000; z += 25 {
			seen[g.BiomeName(x, z)]++
		}
	}
	if len(seen) < 15 {
		t.Fatalf("expected a varied overworld, saw only %d biomes: %v", len(seen), keys(seen))
	}
	// Families we must be able to find somewhere in a 4000-block span.
	families := []string{"forest", "taiga", "ocean", "desert", "plains"}
	for _, fam := range families {
		found := false
		for name := range seen {
			if strings.Contains(name, fam) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no %q biome anywhere in the sample: %v", fam, keys(seen))
		}
	}
}

// TestRiversAppear: river carving must open water channels through the lowlands.
func TestRiversAppear(t *testing.T) {
	g := NewGenerator(7)
	rivers := 0
	for x := -3000; x <= 3000; x += 15 {
		for z := -3000; z <= 3000; z += 15 {
			if n := g.BiomeName(x, z); n == "minecraft:river" || n == "minecraft:frozen_river" {
				rivers++
			}
		}
	}
	if rivers == 0 {
		t.Fatal("no river columns generated anywhere — river carving is dead")
	}
}

// TestSurfaceBlocksMatchBiome: a desert column must be sand-topped, a snowy one
// snow-topped — the biome drives the surface, so they can't diverge.
func TestSurfaceBlocksMatchBiome(t *testing.T) {
	g := NewGenerator(1)
	for x := -3000; x <= 3000; x += 11 {
		for z := -3000; z <= 3000; z += 11 {
			name := g.BiomeName(x, z)
			col := g.columnAt(x, z)
			switch name {
			case "minecraft:desert":
				if col.topBlock() != Sand {
					t.Fatalf("desert at (%d,%d) not sand-topped: %d", x, z, col.topBlock())
				}
				return
			}
		}
	}
	t.Skip("no desert found to check")
}

// TestRiversStayShallow: rivers must carve gentle lowland channels, never the
// 50-block abyss the first cut gouged through hills.
func TestRiversStayShallow(t *testing.T) {
	g := NewGenerator(1)
	maxDrop := 0
	for wx := -2500; wx < 2500; wx += 3 {
		for wz := -2500; wz < 2500; wz += 3 {
			if d := g.landHeight(wx, wz) - g.Height(wx, wz); d > maxDrop {
				maxDrop = d
			}
		}
	}
	if maxDrop > 12 {
		t.Fatalf("river carve gouged %d blocks — must stay a shallow channel (<=12)", maxDrop)
	}
}

// TestNoSkyFloaters: no surface terrain may be disconnected from the ground.
// This is the real check — a cross-chunk flood fill from the deep ground; any
// solid block above sea level the flood can't reach is a floating fragment
// (cave-severed spire, undercut bank) that removeFloatingFragments must delete.
func TestNoSkyFloaters(t *testing.T) {
	g := NewGenerator(1)
	const N = 5
	lo, hi := -N*16, N*16
	W, H := hi-lo, SectionCount*16
	grid := map[[2]int32]*Chunk{}
	for cx := int32(-N); cx < N; cx++ {
		for cz := int32(-N); cz < N; cz++ {
			grid[[2]int32{cx, cz}] = g.GenerateChunk(cx, cz)
		}
	}
	at := func(wx, y, wz int) uint32 {
		if y < MinY || y >= MinY+H || wx < lo || wx >= hi || wz < lo || wz >= hi {
			return Air
		}
		ch := grid[[2]int32{int32(wx >> 4), int32(wz >> 4)}]
		if ch == nil {
			return Air
		}
		return ch.Sections[(y-MinY)/16][((y-MinY)%16*16+(wz&15))*16+(wx&15)]
	}
	solid := func(b uint32) bool { return b != Air && b != Water && b != Lava }
	vis := make([]bool, W*H*W)
	fi := func(wx, y, wz int) int { return ((y-MinY)*W+(wz-lo))*W + (wx - lo) }
	var st [][3]int
	push := func(wx, y, wz int) {
		if wx < lo || wx >= hi || wz < lo || wz >= hi || y < MinY || y >= MinY+H {
			return
		}
		if i := fi(wx, y, wz); !vis[i] && solid(at(wx, y, wz)) {
			vis[i] = true
			st = append(st, [3]int{wx, y, wz})
		}
	}
	for wx := lo; wx < hi; wx++ { // seed the always-solid deep ground everywhere
		for wz := lo; wz < hi; wz++ {
			for y := MinY; y < MinY+40; y++ {
				push(wx, y, wz)
			}
		}
	}
	for len(st) > 0 {
		c := st[len(st)-1]
		st = st[:len(st)-1]
		push(c[0]+1, c[1], c[2])
		push(c[0]-1, c[1], c[2])
		push(c[0], c[1], c[2]+1)
		push(c[0], c[1], c[2]-1)
		push(c[0], c[1]+1, c[2])
		push(c[0], c[1]-1, c[2])
	}
	floaters := 0
	for wx := lo + 2; wx < hi-2; wx++ { // interior only (borders can attach outward)
		for wz := lo + 2; wz < hi-2; wz++ {
			for y := SeaLevel; y < 200; y++ {
				if solid(at(wx, y, wz)) && !vis[fi(wx, y, wz)] {
					floaters++
				}
			}
		}
	}
	if floaters > 0 {
		t.Fatalf("%d disconnected floating blocks above sea level — generation leaves debris", floaters)
	}
}

func keys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
