package worldgen

import "testing"

// findWith scans generated chunks around origin for a predicate block.
func findWith(g *Generator, radius int32, want func(uint32) bool) (int, int, int, bool) {
	for cx := -radius; cx <= radius; cx++ {
		for cz := -radius; cz <= radius; cz++ {
			ch := g.GenerateChunk(cx, cz)
			for s := 0; s < SectionCount; s++ {
				for i, b := range ch.Sections[s] {
					if want(b) {
						return int(cx)*16 + i%16, MinY + s*16 + i/256, int(cz)*16 + (i/16)%16, true
					}
				}
			}
		}
	}
	return 0, 0, 0, false
}

func TestStructuresAppear(t *testing.T) {
	g := NewGenerator(7)
	if _, _, _, ok := findWith(g, 8, func(b uint32) bool { return b == Spawner }); !ok {
		t.Error("no dungeon spawner within 17x17 chunks")
	}
}

// TestMineshaftStamps hunts the grid for a real shaft cell, then generates the
// chunk holding its centre and expects corridor blocks.
func TestMineshaftStamps(t *testing.T) {
	g := NewGenerator(7)
	for ox := -shaftCell * 20; ox <= shaftCell*20; ox += shaftCell {
		for oz := -shaftCell * 20; oz <= shaftCell*20; oz += shaftCell {
			arms := g.shaftArms(ox+8, oz+8)
			if len(arms) == 0 {
				continue
			}
			a := arms[0]
			mid := a.length / 2
			wx, wz := a.x+a.dx*mid, a.z+a.dz*mid
			ch := g.GenerateChunk(int32(wx>>4), int32(wz>>4))
			lx, lz := wx&15, wz&15
			sec := (a.y - 1 - MinY) / 16
			ly := (a.y - 1 - MinY) % 16
			if got := ch.Sections[sec][(ly*16+lz)*16+lx]; got != OakPlanks {
				t.Fatalf("corridor floor at (%d,%d,%d) should be planks, got %d", wx, a.y-1, wz, got)
			}
			return
		}
	}
	t.Fatal("no mineshaft cell rolled in 41x41 cells — odds broken")
}

// TestRuinStamps hunts for a ruin cell on habitable land and expects bricks.
func TestRuinStamps(t *testing.T) {
	g := NewGenerator(7)
	for ox := -ruinCell * 60; ox <= ruinCell*60; ox += ruinCell {
		for oz := -ruinCell * 60; oz <= ruinCell*60; oz += ruinCell {
			if hash01(g.seed, ox, oz, 0x2E11) >= ruinOdds {
				continue
			}
			rx := ox + 12 + int(hash01(g.seed, ox, oz, 0x2E12)*float64(ruinCell-24))
			rz := oz + 12 + int(hash01(g.seed, ox, oz, 0x2E13)*float64(ruinCell-24))
			surf := g.Height(rx, rz)
			if surf <= SeaLevel+1 || surf >= 96 {
				continue
			}
			half := 2 + int(hash01(g.seed, ox, oz, 0x2E14)*2)
			wx, wz := rx+half, rz // a wall column
			ch := g.GenerateChunk(int32(wx>>4), int32(wz>>4))
			found := false
			for s := 0; s < SectionCount && !found; s++ {
				for i, b := range ch.Sections[s] {
					if i%16 == wx&15 && (i/16)%16 == wz&15 &&
						(b == StoneBricks || b == MossyStoneBricks || b == CrackedStoneBricks) {
						found = true
						break
					}
				}
			}
			if !found {
				// Wall height can roll 0 on this column; try the corner too.
				wx2, wz2 := rx-half, rz-half
				ch2 := g.GenerateChunk(int32(wx2>>4), int32(wz2>>4))
				for s := 0; s < SectionCount && !found; s++ {
					for i, b := range ch2.Sections[s] {
						if i%16 == wx2&15 && (i/16)%16 == wz2&15 &&
							(b == StoneBricks || b == MossyStoneBricks || b == CrackedStoneBricks) {
							found = true
							break
						}
					}
				}
			}
			if found {
				return
			}
		}
	}
	t.Fatal("no ruin stamped anywhere it should be")
}

func TestStructuresDeterministic(t *testing.T) {
	a, b := NewGenerator(11), NewGenerator(11)
	for _, c := range [][2]int32{{0, 0}, {-3, 5}, {17, -9}} {
		ca, cb := a.GenerateChunk(c[0], c[1]), b.GenerateChunk(c[0], c[1])
		for s := range ca.Sections {
			if ca.Sections[s] != cb.Sections[s] {
				t.Fatalf("chunk %v section %d differs between identical seeds", c, s)
			}
		}
	}
}

func TestDungeonQueryMatchesStamp(t *testing.T) {
	g := NewGenerator(7)
	x, y, z, ok := findWith(g, 8, func(b uint32) bool { return b == Spawner })
	if !ok {
		t.Skip("no dungeon in range")
	}
	d := g.DungeonIn(x, z)
	if !d.Exists || d.X != x || d.Y != y || d.Z != z {
		t.Fatalf("DungeonIn(%d,%d) = %+v; want spawner at (%d,%d,%d)", x, z, d, x, y, z)
	}
}
