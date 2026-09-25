package worldgen

import (
	"math"
	"reflect"
	"testing"
)

// setTestEdits installs a fake edit overlay on g.
func setTestEdits(g *Generator, edits map[[3]int]uint32) {
	g.SetEditLookup(func(x, y, z int) (uint32, bool) { s, ok := edits[[3]int{x, y, z}]; return s, ok })
	g.SetEditRegion(func(cx, cz int32, fn func(x, y, z int, s uint32)) {
		for p, s := range edits {
			if int32(floorDiv16(p[0])) == cx && int32(floorDiv16(p[2])) == cz {
				fn(p[0], p[1], p[2], s)
			}
		}
	})
}

// terrainChunk is GenerateChunk's terrain fill (noise, caves, the surface
// support pass) — what the carvers and lakes run on.
func terrainChunk(g *Generator, cx, cz int32) *Chunk {
	ch := NewChunk(g.sections)
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := int(cx)*16+lx, int(cz)*16+lz
			col := g.columnAt(wx, wz)
			for s := 0; s < g.sections; s++ {
				for ly := 0; ly < 16; ly++ {
					ch.Sections[s][(ly*16+lz)*16+lx] = g.terrainCell(col, wx, MinY+s*16+ly, wz)
				}
			}
		}
	}
	g.supportSurface(ch, cx, cz)
	return ch
}

// firstCanyon finds a ravine whose envelope spans at least two chunks
// east-west and whose middle runs under dry land when dry is set.
func firstCanyon(t *testing.T, g *Generator, dry bool) (*canyon, int32, int32) {
	t.Helper()
	for scx := int32(0); scx < 80; scx++ {
		for scz := int32(0); scz < 80; scz++ {
			c := g.canyonAt(scx, scz)
			if c == nil || len(c.steps) <= 20 || floorDiv16(c.x1)-floorDiv16(c.x0) < 2 {
				continue
			}
			if mid := c.steps[len(c.steps)/2]; !dry || g.Height(int(mid.x), int(mid.z)) > SeaLevel+8 {
				return c, scx, scz
			}
		}
	}
	t.Fatal("no ravine in 6400 chunks")
	return nil, 0, 0
}

// A ravine is drawn the same from any pass, and each chunk pass carves
// exactly the ravine's own cells within it — so adjacent chunks meet.
func TestCanyonSameInEveryPass(t *testing.T) {
	g := NewGenerator(1)
	c, scx, scz := firstCanyon(t, g, false)
	if d := NewGenerator(1).canyonAt(scx, scz); !reflect.DeepEqual(c, d) {
		t.Fatal("the same ravine drew differently in a second generator")
	}
	// Two adjacent chunks on its spine.
	mid := c.steps[len(c.steps)/2]
	cx, cz := int32(floorDiv16(int(mid.x))), int32(floorDiv16(int(mid.z)))
	loY, hiY := MinY+1, g.Ceiling()-1-canyonTopProtected
	carved := 0
	for _, pass := range [][2]int32{{cx, cz}, {cx + 1, cz}, {cx - 1, cz}, {cx, cz + 1}} {
		mask := make([]bool, g.sections*16*256)
		c.markChunk(mask, pass[0], pass[1], loY, hiY)
		bx, bz := int(pass[0])*16, int(pass[1])*16
		for y := loY + 1; y <= hiY; y++ {
			for lz := 0; lz < 16; lz++ {
				for lx := 0; lx < 16; lx++ {
					want := false
					for _, s := range c.steps {
						if y > int(math.Floor(s.y-s.vr))-1 && y <= int(math.Floor(s.y+s.vr))+1 && c.carves(s, bx+lx, y, bz+lz) {
							want = true
							break
						}
					}
					if got := mask[(y-MinY)*256+lz*16+lx]; got != want {
						t.Fatalf("chunk %v cell %d,%d,%d: pass carves %v, the ravine %v", pass, bx+lx, y, bz+lz, got, want)
					}
					if want {
						carved++
					}
				}
			}
		}
	}
	if carved == 0 {
		t.Fatal("the ravine carved nothing on its own spine")
	}
}

// A ravine with a player's build anywhere in its envelope is not carved —
// not even in the chunks far from the build.
func TestCanyonBuildGuard(t *testing.T) {
	g := NewGenerator(1)
	c, _, _ := firstCanyon(t, g, true)
	mid := c.steps[len(c.steps)/2]
	cx, cz := int32(floorDiv16(int(mid.x))), int32(floorDiv16(int(mid.z)))
	open := terrainChunk(g, cx, cz)
	g.carveCanyons(open, cx, cz)
	base := terrainChunk(g, cx, cz)
	var cell [3]int
	found := false
	for i := 0; i < len(base.Sections)*4096 && !found; i++ {
		s, j := i/4096, i%4096
		if base.Sections[s][j] == Stone && open.Sections[s][j] == Air {
			cell, found = [3]int{j & 15, MinY + s*16 + j>>8, (j >> 4) & 15}, true
		}
	}
	if !found {
		t.Fatal("the ravine cut no stone in its middle chunk")
	}
	// A plank at the far corner of the envelope, chunks away.
	g2 := NewGenerator(1)
	setTestEdits(g2, map[[3]int]uint32{{c.x0, c.y1, c.z0}: BlockBase("oak_planks")})
	shut := terrainChunk(g2, cx, cz)
	g2.carveCanyons(shut, cx, cz)
	if s := sectionBlockAt(shut, cell[0], cell[1], cell[2]); s != Stone {
		t.Errorf("cell %v under a guarded ravine is %d, want stone", cell, s)
	}
	// The world's own changes (a dug hole, water) do not guard.
	g3 := NewGenerator(1)
	setTestEdits(g3, map[[3]int]uint32{{c.x0, c.y1, c.z0}: Air, {c.x1, c.y0, c.z1}: WaterBase})
	dug := terrainChunk(g3, cx, cz)
	g3.carveCanyons(dug, cx, cz)
	if s := sectionBlockAt(dug, cell[0], cell[1], cell[2]); s != Air {
		t.Errorf("a dug hole blocked the ravine: cell %v is %d", cell, s)
	}
}
