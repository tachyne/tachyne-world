package worldgen

import "testing"

// A lava lake with a build in its box is not placed.
func TestLavaLakeBuildGuard(t *testing.T) {
	g := NewGenerator(1)
	m := &lakeModel{g: g, cols: map[[2]int]column{}}
	var l *lavaLake
	var sc [2]int32
	for scx := int32(0); scx < 40 && l == nil; scx++ {
		for scz := int32(0); scz < 40 && l == nil; scz++ {
			l, sc = g.lavaLakeAt(m, scx, scz, false), [2]int32{scx, scz}
		}
	}
	if l == nil {
		t.Fatal("no underground lava lake in 1600 chunks")
	}
	// A lava cell of the lake, and its chunk.
	var p [3]int
	for i := range l.grid {
		if l.grid[i] && i%8 < 4 {
			xx, zz, yy := i/128, (i/8)%16, i%8
			p = [3]int{l.x + xx, l.y + yy, l.z + zz}
			break
		}
	}
	cx, cz := int32(floorDiv16(p[0])), int32(floorDiv16(p[2]))
	lx, lz := p[0]-int(cx)*16, p[2]-int(cz)*16
	ch := terrainChunk(g, cx, cz)
	g.placeLavaLakes(ch, cx, cz)
	if s := sectionBlockAt(ch, lx, p[1], lz); s != Lava {
		t.Fatalf("lake cell %v is %d, want lava", p, s)
	}
	g2 := NewGenerator(1)
	setTestEdits(g2, map[[3]int]uint32{{l.x - 1, l.y + 2, l.z - 1}: BlockBase("cobblestone")})
	if g2.lavaLakeAt(&lakeModel{g: g2, cols: map[[2]int]column{}}, sc[0], sc[1], false) != nil {
		t.Error("a lake with a build in its box was drawn")
	}
	ch2 := terrainChunk(g2, cx, cz)
	g2.placeLavaLakes(ch2, cx, cz)
	if s := sectionBlockAt(ch2, lx, p[1], lz); s == Lava {
		t.Errorf("lake cell %v is lava beside a build", p)
	}
}
