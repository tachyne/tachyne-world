package worldgen

import "testing"

// A template's cells for /place template: one per block, mirrored then
// rotated about the origin, and none when the integrity roll keeps nothing.
func TestPlaceCells(t *testing.T) {
	tm := TemplateByName("igloo/top")
	if tm == nil {
		t.Fatal("no igloo/top template")
	}
	plain := tm.PlaceCells(10, 20, 30, 0, MirrorNone, nil)
	if len(plain) != len(tm.Blocks) {
		t.Fatalf("%d cells for %d blocks", len(plain), len(tm.Blocks))
	}
	turned := tm.PlaceCells(10, 20, 30, 1, MirrorLeftRight, nil)
	for i, b := range tm.Blocks {
		x, y, z := transformPos(b[0], b[1], b[2], 1, mirLR)
		if c := turned[i]; c.X != 10+x || c.Y != 20+y || c.Z != 30+z {
			t.Fatalf("block %d at %d,%d,%d, want %d,%d,%d", i, c.X, c.Y, c.Z, 10+x, 20+y, 30+z)
		}
	}
	if got := tm.PlaceCells(0, 0, 0, 0, MirrorNone, func() bool { return false }); len(got) != 0 {
		t.Errorf("integrity 0 kept %d cells", len(got))
	}
}

// /place jigsaw anchors the start piece on its named jigsaw, a block below
// the given position; an unknown target fails.
func TestPlaceJigsawPieces(t *testing.T) {
	g := NewGenerator(1)
	pieces, ok := g.PlaceJigsawPieces("minecraft:trial_chambers/chamber/entrance_cap", "minecraft:entrance_cap", 3, 100, -4, 1, 7)
	if !ok || len(pieces) == 0 {
		t.Fatal("entrance cap did not place")
	}
	p := &pieces[0]
	found := false
	for _, j := range p.Tmpl.Jigsaws {
		if j.Name != "entrance_cap" {
			continue
		}
		x, y, z, _, _ := p.worldJigsaw(j)
		found = found || x == 3 && y == 99 && z == -4
	}
	if !found {
		t.Errorf("no entrance_cap jigsaw at 3,99,-4 (piece at %d,%d,%d rot %d)", p.OX, p.OY, p.OZ, p.Rot)
	}
	if _, ok := g.PlaceJigsawPieces("trial_chambers/chamber/entrance_cap", "no_such_jigsaw", 0, 100, 0, 1, 7); ok {
		t.Error("an unknown target must fail")
	}
	if HasPool("no/such/pool") || !HasPool("minecraft:village/plains/town_centers") {
		t.Error("HasPool")
	}
}

// /place structure starts a structure at the chunk holding the position.
func TestPlaceStructurePieces(t *testing.T) {
	g := NewGenerator(1)
	ig, ok := g.PlaceStructurePieces("minecraft:igloo", 21, -5)
	if !ok || ig[0].OX != 16 || ig[0].OZ != -16 {
		t.Fatalf("igloo: ok=%v %+v", ok, ig)
	}
	if v, ok := g.PlaceStructurePieces("village_plains", 0, 0); !ok || len(v) < 2 {
		t.Errorf("village: ok=%v, %d pieces", ok, len(v))
	}
	if _, ok := g.PlaceStructurePieces("mansion", 0, 0); ok {
		t.Error("the mansion cannot be started on demand")
	}
	if x0, z0, x1, z1, ok := PiecesBounds(ig); !ok || x0 > 16 || z0 > -16 || x1 < x0 || z1 < z0 {
		t.Errorf("bounds %d,%d..%d,%d", x0, z0, x1, z1)
	}
}
