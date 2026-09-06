package worldgen

import "testing"

// A fortress grows from its start crossing into bridges and, behind an
// entrance, castle corridors; its chests and blaze thrones are exposed, and
// its lowest floor lands in the vanilla height band.
func TestFortressAssembles(t *testing.T) {
	g := NewGenerator(9)
	bridges, corridors, chests, thrones, big := 0, 0, 0, 0, 0
	for i := 0; i < 8; i++ {
		f := Fortress{X: 1000 + i*400, Z: -700, Exists: true}
		ps := g.assembleFortress(f)
		if len(ps) >= 15 {
			big++
		}
		if ps[0].kind != fkBridgeCrossing {
			t.Error("a fortress starts with a bridge crossing")
		}
		if g.assembleFortress(f)[0] != ps[0] {
			t.Error("assembly must be cached per site")
		}
		lo, spawners := 1<<30, 0
		for _, p := range ps {
			if p.box.y0 < lo {
				lo = p.box.y0
			}
			switch p.kind {
			case fkBridgeStraight, fkBridgeCrossing:
				bridges++
			case fkCorridor, fkCorridorCrossing, fkCorridorLeftTurn, fkCorridorRightTurn, fkCorridorStairs:
				corridors++
			case fkMonsterThrone:
				thrones++
				spawners++
			}
		}
		if lo < fortressMinY || lo > fortressMaxY {
			t.Errorf("fortress %d lowest floor y=%d, want %d..%d", i, lo, fortressMinY, fortressMaxY)
		}
		if len(g.FortressSpawners(f)) != spawners {
			t.Errorf("fortress %d: %d spawners for %d thrones", i, len(g.FortressSpawners(f)), spawners)
		}
		chests += len(g.FortressChests(f))
	}
	if big == 0 {
		t.Errorf("no fortress of eight reached 15 pieces")
	}
	if bridges == 0 || corridors == 0 || chests == 0 || thrones == 0 {
		t.Errorf("eight fortresses: bridges=%d corridors=%d chests=%d thrones=%d — every kind should show up", bridges, corridors, chests, thrones)
	}
}

// Pieces never overlap one another.
func TestFortressPiecesDisjoint(t *testing.T) {
	g := NewGenerator(21)
	ps := g.assembleFortress(Fortress{X: 300, Z: 300, Exists: true})
	for i := range ps {
		for j := i + 1; j < len(ps); j++ {
			if ps[i].box.intersects(ps[j].box) {
				t.Errorf("pieces %d and %d overlap: %+v %+v", i, j, ps[i].box, ps[j].box)
			}
		}
	}
}

// An oriented box and the local→world mapping agree for every facing: local
// cells stay inside the box, the foot cell maps back to the foot, and a
// fence's connections turn with the piece.
func TestFortressOrientation(t *testing.T) {
	for _, dir := range []fdir{fNorth, fSouth, fWest, fEast} {
		p := &fpiece{kind: fkBridgeStraight, box: orientBox(100, 60, 200, -1, -3, 0, 5, 10, 19, dir), dir: dir}
		for x := 0; x < 5; x++ {
			for z := 0; z < 19; z++ {
				w := p.worldPos(x, 3, z)
				if w[0] < p.box.x0 || w[0] > p.box.x1 || w[2] < p.box.z0 || w[2] > p.box.z1 || w[1] != 60 {
					t.Fatalf("dir %d local (%d,%d) → %v outside %+v", dir, x, z, w, p.box)
				}
			}
		}
		if w := p.worldPos(1, 3, 0); w[0] != 100 || w[2] != 200 {
			t.Errorf("dir %d: the foot cell maps to %v, want (100,_,200)", dir, w)
		}
	}
	east := &fpiece{dir: fEast}
	east.rot, east.mir = orientation(fEast)
	ns := east.resolve(fence("north", "south"))
	we := (&fpiece{}).resolve(fence("west", "east"))
	if ns != we {
		t.Errorf("a north-south fence in an east-facing piece should be the west-east fence (state %d vs %d)", ns, we)
	}
}

// A nether chunk over a fortress carries nether bricks; the site query is
// stable.
func TestFortressStamps(t *testing.T) {
	g := NewGenerator(9)
	var f Fortress
	for cx := -6; cx < 6 && !f.Exists; cx++ {
		for cz := -6; cz < 6 && !f.Exists; cz++ {
			f = g.FortressIn(cx*fortressCell+8, cz*fortressCell+8)
		}
	}
	if !f.Exists {
		t.Fatal("no fortress in 144 cells")
	}
	if f2 := g.FortressIn(f.X, f.Z); f2 != f {
		t.Errorf("site query unstable: %+v vs %+v", f, f2)
	}
	start := g.assembleFortress(f)[0].box
	ch := g.generateNetherChunk(int32(floorDiv(start.x0+9, 16)), int32(floorDiv(start.z0+9, 16)))
	lo, hi := BlockRange("nether_bricks")
	n := 0
	for _, sec := range ch.Sections {
		for _, st := range sec {
			if st >= lo && st <= hi {
				n++
			}
		}
	}
	if n < 50 {
		t.Errorf("the start chunk holds only %d nether bricks", n)
	}
}
