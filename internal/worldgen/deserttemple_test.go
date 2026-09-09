package worldgen

import "testing"

// The ported pyramid stamps its shell, the four chests round the TNT trap,
// the plate over the trap, and the cellar's sand with its suspicious cells,
// in whatever facing the site rolled.
func TestDesertTempleStampsVanillaPiece(t *testing.T) {
	g := NewGenerator(7)
	var d DesertTemple
	for cx := 0; cx < 24000 && !d.Exists; cx += templeCell {
		for cz := 0; cz < 24000 && !d.Exists; cz += templeCell {
			d = g.DesertTempleIn(cx+templeCell/2, cz+templeCell/2)
		}
	}
	if !d.Exists {
		t.Skip("no desert temple within scan range for this seed")
	}
	chunks := map[[2]int32]*Chunk{}
	at := func(wx, wy, wz int) uint32 {
		k := [2]int32{int32(wx >> 4), int32(wz >> 4)}
		if chunks[k] == nil {
			chunks[k] = g.GenerateChunk(k[0], k[1])
		}
		return sectionBlockAt(chunks[k], wx-(wx>>4)*16, wy, wz-(wz>>4)*16)
	}
	chestLo, chestHi, _ := BlockRangeOK("chest")
	mx, _, mz := d.world(10, -11, 10)
	for _, c := range d.Chests() {
		got := at(c[0], c[1], c[2])
		if got < chestLo || got > chestHi {
			t.Errorf("chest cell %v holds %d", c, got)
			continue
		}
		info, _ := InfoForState(got)
		want := "north"
		switch {
		case mz > c[2]:
			want = "south"
		case mx < c[0]:
			want = "west"
		case mx > c[0]:
			want = "east"
		}
		if f := GetProperty(info, got, "facing"); f != want {
			t.Errorf("chest at %v faces %s, want %s (toward the plate)", c, f, want)
		}
	}
	px, py, pz := d.world(10, -11, 10)
	if got := at(px, py, pz); got != StonePressurePlate {
		t.Errorf("pressure plate cell holds %d", got)
	}
	tx, ty, tz := d.world(10, -13, 10)
	if got := at(tx, ty, tz); got != TNTBlock {
		t.Errorf("TNT cell holds %d", got)
	}
	bx, by, bz := d.world(10, 0, 10)
	if got := at(bx, by, bz); got != BlueTerracotta {
		t.Errorf("floor motif centre holds %d", got)
	}
	ax, ay, az := d.world(9, 9, 9)
	if got := at(ax, ay, az); got != Sandstone {
		t.Errorf("apex ring holds %d", got)
	}
	for _, c := range d.Sus {
		if got := at(c[0], c[1], c[2]); got != SuspiciousSand {
			t.Errorf("suspicious cell %v holds %d", c, got)
		}
	}
	sandCells := 0
	for _, c := range templePotentialSand(d) {
		if got := at(c[0], c[1], c[2]); got == Sand || got == SuspiciousSand {
			sandCells++
		}
	}
	if sandCells < 70 {
		t.Errorf("cellar holds %d sand cells of %d", sandCells, len(templePotentialSand(d)))
	}
	// The cellar stair's facing follows the piece's orientation.
	sx, sy, sz := d.world(13, -1, 17)
	got := at(sx, sy, sz)
	info, _ := InfoForState(got)
	want := map[int]string{0: "west", 1: "west", 2: "north", 3: "north"}[d.Dir]
	if f := GetProperty(info, got, "facing"); f != want {
		t.Errorf("cellar stair facing %q for dir %d, want %q", f, d.Dir, want)
	}
	t.Logf("temple at %d,%d,%d facing %d, %d suspicious cells", d.X, d.Y, d.Z, d.Dir, len(d.Sus))
}
