package worldgen

import "testing"

// bareTerrain is a chunk of the terrain model alone: no features.
func bareTerrain(g *Generator, cx, cz int32) *Chunk {
	ch := NewChunk(g.sections)
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := int(cx)*16+lx, int(cz)*16+lz
			col := g.columnAt(wx, wz)
			for y := MinY; y < MinY+g.sections*16; y++ {
				setSectionBlock(ch, lx, y, lz, col.block(y), true)
			}
		}
	}
	return ch
}

// landChunk is a plains chunk whose centre column stands above the sea.
func landChunk(t *testing.T, g *Generator) (int32, int32, int) {
	t.Helper()
	for _, p := range findBiomeChunks(g, "minecraft:plains", 32) {
		if h := g.Height(int(p[0])*16+8, int(p[1])*16+8); h > SeaLevel+4 {
			return p[0], p[1], h
		}
	}
	t.Skip("no dry plains chunk for this seed")
	return 0, 0, 0
}

// The formulas: the beard fills below a piece's ground and cuts above it,
// bury is a cone of radius six, encapsulate weighs 0.8 inside the box.
func TestBeardifierFormulas(t *testing.T) {
	if v := beardContribution(0, -2, 0, -2); v <= 0 {
		t.Errorf("below the ground the beard is %v, want positive", v)
	}
	if v := beardContribution(0, 2, 0, 2); v >= 0 {
		t.Errorf("above the ground the beard is %v, want negative", v)
	}
	if v := beardContribution(13, 0, 0, 0); v != 0 {
		t.Errorf("past the kernel the beard is %v", v)
	}
	if buryContribution(0, 0, 0) != 1 || buryContribution(6, 0, 0) != 0 || buryContribution(3, 0, 0) != 0.5 {
		t.Error("bury is not 1 - d/6 inside a radius of six")
	}
	box := fbox{0, 0, 0, 4, 4, 4}
	if v := beardValue([]beardRigid{{box, adaptEncapsulate, 1}}, 2, 2, 2); v != 0.8 {
		t.Errorf("encapsulate inside its box is %v, want 0.8", v)
	}
	// beard_box: every height inside the box over its ground is cut alike.
	a := beardValue([]beardRigid{{box, adaptBeardBox, 1}}, 2, 2, 2)
	b := beardValue([]beardRigid{{box, adaptBeardBox, 1}}, 2, 4, 2)
	if a >= 0 || b >= 0 || a-b > 1e-9 || b-a > 1e-9 {
		t.Errorf("beard_box inside the box: %v and %v, want equal and negative", a, b)
	}
}

// beard_thin under a piece floating three blocks over the ground fills the
// gap; over a piece sunk into the ground it cuts the terrain back.
func TestBeardThinFillsAndShaves(t *testing.T) {
	g := NewGenerator(1)
	cx, cz, h := landChunk(t, g)
	bx, bz := int(cx)*16, int(cz)*16

	ch := bareTerrain(g, cx, cz)
	floating := fbox{bx + 5, h + 3, bz + 5, bx + 10, h + 8, bz + 10}
	g.adaptChunk(ch, cx, cz, []beardRigid{{floating, adaptBeardThin, h + 4}})
	for y := h; y <= h+3; y++ {
		if s := sectionBlockAt(ch, 8, y, 8); s == Air {
			t.Errorf("floating piece: y=%d under it still air (ground %d)", y, h)
		}
	}
	if s := sectionBlockAt(ch, 8, h+2, 8); s == Air || IsReplaceable(s) {
		t.Errorf("the fill under the piece is %d", s)
	}

	ch = bareTerrain(g, cx, cz)
	sunk := fbox{bx + 5, h - 5, bz + 5, bx + 10, h + 3, bz + 10}
	g.adaptChunk(ch, cx, cz, []beardRigid{{sunk, adaptBeardThin, h - 4}})
	for y := h - 4; y < h; y++ {
		if s := sectionBlockAt(ch, 8, y, 8); s != Air {
			t.Errorf("sunk piece: ground at y=%d (over its ground %d) not cut: %d", y, h-4, s)
		}
	}
	if s := sectionBlockAt(ch, 8, h-5, 8); s == Air {
		t.Error("the piece's own foundation layer was cut")
	}
}

// bury and encapsulate close the caves about a buried structure; beard_box
// opens an ancient city's box over its ground.
func TestBuryEncapsulateBeardBox(t *testing.T) {
	g := NewGenerator(1)
	cx, cz, h := landChunk(t, g)
	bx, bz := int(cx)*16, int(cz)*16
	y := h - 40
	pocket := func(ch *Chunk, lx, ly, lz int) {
		for dx := 0; dx < 3; dx++ {
			for dy := 0; dy < 3; dy++ {
				for dz := 0; dz < 3; dz++ {
					setSectionBlock(ch, lx+dx, ly+dy, lz+dz, Air, true)
				}
			}
		}
	}
	airIn := func(ch *Chunk, lx, ly, lz int) int {
		n := 0
		for dx := 0; dx < 3; dx++ {
			for dy := 0; dy < 3; dy++ {
				for dz := 0; dz < 3; dz++ {
					if sectionBlockAt(ch, lx+dx, ly+dy, lz+dz) == Air {
						n++
					}
				}
			}
		}
		return n
	}

	ch := bareTerrain(g, cx, cz)
	pocket(ch, 6, y+1, 6)
	g.adaptChunk(ch, cx, cz, []beardRigid{{fbox{bx + 4, y, bz + 4, bx + 11, y + 6, bz + 11}, adaptBury, y}})
	if n := airIn(ch, 6, y+1, 6); n != 0 {
		t.Errorf("bury left %d cave cells open at the stronghold's floor", n)
	}

	ch = bareTerrain(g, cx, cz)
	pocket(ch, 11, y+1, 6) // three blocks east of the box
	g.adaptChunk(ch, cx, cz, []beardRigid{{fbox{bx + 2, y, bz + 2, bx + 7, y + 6, bz + 12}, adaptEncapsulate, y + 1}})
	if n := airIn(ch, 11, y+1, 6); n != 0 {
		t.Errorf("encapsulate left %d cave cells open beside the chamber", n)
	}

	ch = bareTerrain(g, cx, cz)
	g.adaptChunk(ch, cx, cz, []beardRigid{{fbox{bx + 2, y, bz + 2, bx + 13, y + 12, bz + 13}, adaptBeardBox, y + 1}})
	for yy := y + 1; yy <= y+12; yy++ {
		if s := sectionBlockAt(ch, 8, yy, 8); s != Air {
			t.Errorf("beard_box left rock at y=%d inside the city's box: %d", yy, s)
		}
	}
	if s := sectionBlockAt(ch, 8, y-2, 8); s == Air {
		t.Error("beard_box cut under the city's ground")
	}
}

// A structure a player touched is not adapted: its pieces are left out of
// the chunk's rigids whole.
func TestAdaptSkipsTouchedStructures(t *testing.T) {
	g := NewGenerator(1)
	var op PillagerOutpost
	for i := -20; i <= 20 && !op.Exists; i++ {
		for j := -20; j <= 20 && !op.Exists; j++ {
			op = g.OutpostIn(i*outpostCell+outpostCell/2, j*outpostCell+outpostCell/2)
		}
	}
	if !op.Exists {
		t.Skip("no outpost for this seed")
	}
	cx, cz := int32(floorDiv16(op.X)), int32(floorDiv16(op.Z))
	if len(g.adaptRigids(cx, cz)) == 0 {
		t.Fatal("an untouched outpost gives the chunk no rigids")
	}
	edits := map[[3]int]uint32{{op.X + 3, op.Y + 2, op.Z + 3}: BlockBase("oak_planks")}
	g.SetEditLookup(func(x, y, z int) (uint32, bool) { s, ok := edits[[3]int{x, y, z}]; return s, ok })
	g.SetEditRegion(func(ecx, ecz int32, fn func(x, y, z int, s uint32)) {
		for p, s := range edits {
			if int32(floorDiv16(p[0])) == ecx && int32(floorDiv16(p[2])) == ecz {
				fn(p[0], p[1], p[2], s)
			}
		}
	})
	for _, r := range g.adaptRigids(cx, cz) {
		if r.kind == adaptBeardThin {
			t.Errorf("the touched outpost still adapts: %v", r.box)
		}
	}
}

// Through GenerateChunk: an ancient city's chunk comes out different with
// the adaptation — its box opened over the city's ground.
func TestAdaptTerrainAncientCity(t *testing.T) {
	skipHeavy(t)
	g := NewGenerator(1)
	var a AncientCity
	for i := -30; i <= 30 && !a.Exists; i++ {
		for j := -30; j <= 30 && !a.Exists; j++ {
			a = g.AncientCityIn(i*ancientCityCell+ancientCityCell/2, j*ancientCityCell+ancientCityCell/2)
		}
	}
	if !a.Exists {
		t.Skip("no ancient city for this seed")
	}
	cx, cz := int32(floorDiv16(a.X)), int32(floorDiv16(a.Z))
	air := func(ch *Chunk) int {
		n := 0
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				for y := a.Y + 1; y < a.Y+20; y++ {
					if sectionBlockAt(ch, lx, y, lz) == Air {
						n++
					}
				}
			}
		}
		return n
	}
	on := air(g.GenerateChunk(cx, cz))
	terrainAdaptOff = true
	off := air(g.GenerateChunk(cx, cz))
	terrainAdaptOff = false
	t.Logf("city chunk %d,%d: %d air cells adapted, %d without", cx, cz, on, off)
	if on == off {
		t.Error("the city's chunk came out the same with the adaptation as without")
	}
}
