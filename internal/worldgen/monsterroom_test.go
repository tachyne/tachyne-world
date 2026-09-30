package worldgen

import "testing"

// roomView is a scratch view of solid stone round (x, y, z), with a
// two-high tunnel running east-west through it at floor level.
func roomView(g *Generator, x, y, z int, tunnel bool) *owRegion {
	v := &owRegion{g: g, baseX: x &^ 15, baseZ: z &^ 15, cols: map[[2]int]column{}, capture: map[[3]int]uint32{}}
	for dx := -8; dx <= 8; dx++ {
		for dy := -3; dy <= 7; dy++ {
			for dz := -8; dz <= 8; dz++ {
				s := Stone
				if tunnel && dz == 0 && (dy == 0 || dy == 1) {
					s = Air
				}
				v.capture[[3]int{x + dx, y + dy, z + dz}] = s
			}
		}
	}
	return v
}

// MonsterRoomFeature on a tunnel through rock: a cobblestone shell with a
// mostly mossy floor, air inside, the spawner in the middle, and each chest
// on the floor against one wall.
func TestMonsterRoomShape(t *testing.T) {
	g := NewGenerator(1)
	x, y, z := 1000, 20, 1000
	placed := 0
	for seed := int64(0); seed < 16; seed++ {
		v := roomView(g, x, y, z, true)
		d, ok := monsterRoom(v, newTreeRNG(seed, x, z), x, y, z)
		if !ok {
			t.Fatalf("seed %d: no room on a tunnel through rock", seed)
		}
		placed++
		at := func(x, y, z int) uint32 {
			if s, ok := d.cells[[3]int{x, y, z}]; ok {
				return s
			}
			return v.read(x, y, z)
		}
		if at(x, y, z) != Spawner {
			t.Fatalf("seed %d: no spawner at the origin", seed)
		}
		if d.W < 2 || d.W > 3 || d.D < 2 || d.D > 3 || d.Mob < 0 || d.Mob > 2 {
			t.Fatalf("seed %d: room %+v", seed, d)
		}
		mossy := 0
		for dx := -d.W - 1; dx <= d.W+1; dx++ {
			for dz := -d.D - 1; dz <= d.D+1; dz++ {
				switch f := at(x+dx, y-1, z+dz); f {
				case MossyCobblestone:
					mossy++
				case Cobblestone:
				default:
					t.Fatalf("seed %d: floor cell %d", seed, f)
				}
				if at(x+dx, y+4, z+dz) != Stone {
					t.Fatalf("seed %d: the ceiling is not the feature's to write", seed)
				}
				edge := dx == -d.W-1 || dx == d.W+1 || dz == -d.D-1 || dz == d.D+1
				for dy := 0; dy <= 3; dy++ {
					s := at(x+dx, y+dy, z+dz)
					switch {
					case edge && dz == 0 && dy <= 2:
						// The tunnel's opening, and the wall cell over it:
						// a wall cell over nothing turns to air.
						if s != Air {
							t.Fatalf("seed %d: the tunnel's opening at dy=%d is %d", seed, dy, s)
						}
					case edge:
						if s != Cobblestone {
							t.Fatalf("seed %d: wall cell %d", seed, s)
						}
					case dx == 0 && dz == 0 && dy == 0:
					case dy == 0 && isChestState(s):
					case s != Air:
						t.Fatalf("seed %d: inside cell %d", seed, s)
					}
				}
			}
		}
		if mossy == 0 {
			t.Errorf("seed %d: no mossy cobblestone in the floor", seed)
		}
		if len(d.Chests) > 2 {
			t.Fatalf("seed %d: %d chests", seed, len(d.Chests))
		}
		for _, c := range d.Chests {
			if !isChestState(at(c[0], c[1], c[2])) || c[1] != y {
				t.Fatalf("seed %d: chest %v is not a chest on the floor", seed, c)
			}
			walls := 0 // the other chest may have come to stand beside it
			for _, h := range horizontalNESW {
				if n := at(c[0]+h[0], y, c[2]+h[1]); solid(n) && !isChestState(n) {
					walls++
				}
			}
			if walls > 1 {
				t.Fatalf("seed %d: chest %v against %d walls", seed, c, walls)
			}
		}
	}
	if placed == 0 {
		t.Fatal("no room placed")
	}
}

// The validity check: sealed rock (no opening) and a room over a hole in
// its floor are both refused.
func TestMonsterRoomRefuses(t *testing.T) {
	g := NewGenerator(1)
	x, y, z := 1000, 20, 1000
	if _, ok := monsterRoom(roomView(g, x, y, z, false), newTreeRNG(1, x, z), x, y, z); ok {
		t.Error("a room in sealed rock: vanilla wants one to five openings")
	}
	v := roomView(g, x, y, z, true)
	v.capture[[3]int{x, y - 1, z}] = Air
	if _, ok := monsterRoom(v, newTreeRNG(1, x, z), x, y, z); ok {
		t.Error("a room over a hole in its floor")
	}
}

// findRoom is a monster room with a chest near the origin for seed 1.
func findRoom(t *testing.T, g *Generator) Dungeon {
	t.Helper()
	for cx := int32(-12); cx <= 12; cx++ {
		for cz := int32(-12); cz <= 12; cz++ {
			for _, d := range g.monsterRooms(cx, cz) {
				if len(d.Chests) > 0 {
					return d
				}
			}
		}
	}
	t.Skip("no monster room with a chest near the origin")
	return Dungeon{}
}

// Through GenerateChunk: the rooms the lookups report are the ones the
// chunks carry — spawner and chests — and there are many more of them than
// the old one-per-48-block-cell grid made.
func TestMonsterRoomsGenerate(t *testing.T) {
	g := NewGenerator(1)
	d := findRoom(t, g)
	ch := g.GenerateChunk(int32(floorDiv16(d.X)), int32(floorDiv16(d.Z)))
	if s := sectionBlockAt(ch, d.X-floorDiv16(d.X)*16, d.Y, d.Z-floorDiv16(d.Z)*16); s != Spawner {
		t.Fatalf("no spawner at %d,%d,%d (got %d)", d.X, d.Y, d.Z, s)
	}
	for _, c := range d.Chests {
		cc := g.GenerateChunk(int32(floorDiv16(c[0])), int32(floorDiv16(c[2])))
		if s := sectionBlockAt(cc, c[0]-floorDiv16(c[0])*16, c[1], c[2]-floorDiv16(c[2])*16); !isChestState(s) {
			t.Fatalf("no chest at %v (got %d)", c, s)
		}
		if !g.DungeonChestAt(c[0], c[1], c[2]) {
			t.Fatalf("DungeonChestAt misses the chest at %v", c)
		}
	}
	if got, ok := g.DungeonAt(d.X, d.Y, d.Z); !ok || got.Mob != d.Mob {
		t.Fatalf("DungeonAt: %+v %v", got, ok)
	}
	rooms := 0
	for cx := int32(0); cx < 8; cx++ {
		for cz := int32(0); cz < 8; cz++ {
			rooms += len(g.monsterRooms(cx, cz))
		}
	}
	t.Logf("%d monster rooms in 64 chunks", rooms)
	if rooms < 2 {
		t.Errorf("%d monster rooms in 64 chunks: the old grid alone made about that", rooms)
	}
}

// A room with a player's build or a dug cell in its box is left out, in the
// lookups and in the chunk.
func TestMonsterRoomBuildGuard(t *testing.T) {
	d := findRoom(t, NewGenerator(1))
	for name, e := range map[string]uint32{"build": BlockBase("stone_bricks"), "dug": Air} {
		g := NewGenerator(1)
		setTestEdits(g, map[[3]int]uint32{{d.X + d.W + 1, d.Y + 2, d.Z}: e})
		if _, ok := g.DungeonAt(d.X, d.Y, d.Z); ok {
			t.Errorf("%s in the room's wall: the room is still reported", name)
		}
		ch := g.GenerateChunk(int32(floorDiv16(d.X)), int32(floorDiv16(d.Z)))
		if s := sectionBlockAt(ch, d.X-floorDiv16(d.X)*16, d.Y, d.Z-floorDiv16(d.Z)*16); s == Spawner {
			t.Errorf("%s in the room's wall: the spawner still generates", name)
		}
	}
}

// An old grid dungeon is gone unless a player touched it; one with a dug
// cell in it stays where it was, in the lookups and the chunk.
func TestLegacyDungeonKeptWhenTouched(t *testing.T) {
	g := NewGenerator(7)
	var old Dungeon
	for x := -480; x <= 480 && !old.Exists; x += dungeonCell {
		for z := -480; z <= 480 && !old.Exists; z += dungeonCell {
			old = g.legacyDungeonIn(x, z)
		}
	}
	if !old.Exists {
		t.Skip("no old dungeon cell near the origin")
	}
	if d := g.keptLegacyDungeon(old.X, old.Z); d.Exists {
		t.Fatal("an untouched old dungeon is kept")
	}
	g2 := NewGenerator(7)
	setTestEdits(g2, map[[3]int]uint32{{old.X + 1, old.Y + 1, old.Z}: Air})
	d, ok := g2.DungeonAt(old.X, old.Y, old.Z)
	if !ok || !d.Legacy || len(d.Chests) != 1 {
		t.Fatalf("the touched old dungeon: %+v %v", d, ok)
	}
	if !g2.DungeonChestAt(d.Chests[0][0], d.Chests[0][1], d.Chests[0][2]) {
		t.Error("the kept dungeon's chest is not a dungeon chest")
	}
	ch := g2.GenerateChunk(int32(floorDiv16(old.X)), int32(floorDiv16(old.Z)))
	if s := sectionBlockAt(ch, old.X-floorDiv16(old.X)*16, old.Y, old.Z-floorDiv16(old.Z)*16); s != Spawner {
		t.Errorf("the kept dungeon's spawner is %d", s)
	}
}
