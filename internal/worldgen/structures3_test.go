package worldgen

import "testing"

// TestOutpostQuery checks the placement query in isolation — deterministic, dry
// land, clear of villages, and a chest offset that lands inside the cabin. (Like
// the desert-temple test, we avoid asserting a stamped block against a separately
// recomputed terrain height, which musl vs glibc float noise don't guarantee.)
func TestOutpostQuery(t *testing.T) {
	g := NewGenerator(7)
	var p PillagerOutpost
	found := false
	for cx := 0; cx < 40000 && !found; cx += outpostCell {
		for cz := 0; cz < 40000; cz += outpostCell {
			if q := g.OutpostIn(cx+outpostCell/2, cz+outpostCell/2); q.Exists {
				p, found = q, true
				break
			}
		}
	}
	if !found {
		t.Skip("no outpost within scan range for this seed")
	}
	if q := g.OutpostIn(p.X, p.Z); q != p {
		t.Fatalf("OutpostIn not deterministic: %+v vs %+v", q, p)
	}
	if p.Y <= SeaLevel {
		t.Fatalf("outpost sited at/below sea level: y=%d", p.Y)
	}
	// Sited clear of villages (query rejects a village in the 3×3 neighbourhood).
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if g.VillageIn(p.X+dx*384, p.Z+dz*384).Exists {
				t.Fatalf("outpost overlaps a village at (%d,%d)", p.X, p.Z)
			}
		}
	}
	// Chest rides on the cabin floor (Y+13), inside the 5×5 cabin footprint.
	if p.ChestY != p.Y+13 || abs(p.ChestX-p.X) > 2 || abs(p.ChestZ-p.Z) > 2 {
		t.Fatalf("chest %d,%d,%d not on the cabin floor of tower %d,%d,%d",
			p.ChestX, p.ChestY, p.ChestZ, p.X, p.Y, p.Z)
	}
}

// blockAt reads one block from a freshly generated chunk (positive coords).
func blockAt(g *Generator, wx, wy, wz int) uint32 {
	cx, cz := int32(wx/16), int32(wz/16)
	ch := g.GenerateChunk(cx, cz)
	lx, lz := wx-int(cx)*16, wz-int(cz)*16
	s := (wy - MinY) / 16
	return ch.Sections[s][(wy-MinY)%16*256+lz*16+lx]
}

// TestOutpostStamps locates an outpost via the query, then reads the blocks the
// stamp must place at coordinates derived from the SAME query (a corner post and
// the chest). Both stamp and read use the query's p.Y, so the assertion holds on
// any machine even though the site itself moves with float-noise determinism.
func TestOutpostStamps(t *testing.T) {
	g := NewGenerator(7)
	var p PillagerOutpost
	found := false
	for cx := 0; cx < 40000 && !found; cx += outpostCell {
		for cz := 0; cz < 40000; cz += outpostCell {
			if q := g.OutpostIn(cx+outpostCell/2, cz+outpostCell/2); q.Exists {
				p, found = q, true
				break
			}
		}
	}
	if !found {
		t.Skip("no outpost within scan range for this seed")
	}
	// A dark-oak corner post one block above the footing (not a floor level).
	if b := blockAt(g, p.X+3, p.Y+1, p.Z+3); b != DarkOakLog {
		t.Fatalf("corner post at (%d,%d,%d) = %d, want dark_oak_log %d", p.X+3, p.Y+1, p.Z+3, b, DarkOakLog)
	}
	// The loot chest up in the cabin.
	if b := blockAt(g, p.ChestX, p.ChestY, p.ChestZ); b != ChestNorth {
		t.Fatalf("outpost chest at (%d,%d,%d) = %d, want chest %d", p.ChestX, p.ChestY, p.ChestZ, b, ChestNorth)
	}
}
