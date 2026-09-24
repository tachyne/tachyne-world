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
	// The first spawner that is no mineshaft nest's (those are the
	// mineshafts' own, queried through MineshaftSpawners).
	nest := map[[3]int]bool{}
	for cx := -8; cx <= 8; cx += 4 {
		for cz := -8; cz <= 8; cz += 4 {
			for _, m := range g.MineshaftsNear(cx*16, cz*16) {
				for _, s := range g.MineshaftSpawners(m) {
					nest[s] = true
				}
			}
		}
	}
	var x, y, z int
	ok := false
	for cx := int32(-8); cx <= 8 && !ok; cx++ {
		for cz := int32(-8); cz <= 8 && !ok; cz++ {
			ch := g.GenerateChunk(cx, cz)
			for s := 0; s < SectionCount && !ok; s++ {
				for i, b := range ch.Sections[s] {
					px, py, pz := int(cx)*16+i%16, MinY+s*16+i/256, int(cz)*16+(i/16)%16
					if b == Spawner && !nest[[3]int{px, py, pz}] {
						x, y, z, ok = px, py, pz, true
						break
					}
				}
			}
		}
	}
	if !ok {
		t.Skip("no dungeon in range")
	}
	d := g.DungeonIn(x, z)
	if !d.Exists || d.X != x || d.Y != y || d.Z != z {
		t.Fatalf("DungeonIn(%d,%d) = %+v; want spawner at (%d,%d,%d)", x, z, d, x, y, z)
	}
}
