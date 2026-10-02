package worldgen

import "testing"

// netherrackChunk is a chunk of netherrack from the floor to the roof.
func netherrackChunk(g *Generator) *Chunk {
	ch := NewChunk(g.sections)
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			for y := MinY; y < MinY+g.sections*16; y++ {
				setSectionBlock(ch, lx, y, lz, Netherrack, true)
			}
		}
	}
	return ch
}

// countIn counts a block in a chunk.
func countIn(ch *Chunk, b uint32) int {
	n := 0
	for s := range ch.Sections {
		for _, v := range ch.Sections[s] {
			if v == b {
				n++
			}
		}
	}
	return n
}

// netherBiomeArea finds a chunk whose whole 3×3 neighbourhood is one
// Nether biome (every ore origin's filter then answers it).
func netherBiomeArea(g *Generator, biome string) (int32, int32, bool) {
	for r := 0; r < 48; r++ {
		for cx := -r; cx <= r; cx++ {
			for _, cz := range []int{-r, r} {
				ok := true
				for x := cx*16 - 16; x < cx*16+32 && ok; x += 4 {
					for z := cz*16 - 16; z < cz*16+32 && ok; z += 4 {
						ok = g.netherBiome(x, z) == biome
					}
				}
				if ok {
					return int32(cx), int32(cz), true
				}
			}
		}
	}
	return 0, 0, false
}

// The Nether's ores replace netherrack only (BlockMatchTest), as every
// nether OreFeature does.
func TestNetherOresReplaceNetherrackOnly(t *testing.T) {
	cfg := netherrackOre(NetherGoldOre, 10)
	if to, ok := cfg.replace(Netherrack, 40); !ok || to != NetherGoldOre {
		t.Fatal("netherrack should take the ore")
	}
	for _, s := range []uint32{Basalt, Blackstone, SoulSand, Air, Lava} {
		if _, ok := cfg.replace(s, 40); ok {
			t.Errorf("block %d should not take a nether ore", s)
		}
	}
}

// An OreFeature blob in netherrack: an ellipsoid of ore about its origin,
// and none at all with a player's build in its box.
func TestNetherOreBlobAndBuildGuard(t *testing.T) {
	g := NewNetherGenerator(11)
	top := MinY + g.sections*16
	blob := func(guard *buildIndex) int {
		ch := netherrackChunk(g)
		reg := &owRegion{g: g, ch: ch, baseX: 0, baseZ: 0, cols: map[[2]int]column{}}
		oreBlob(reg, guard, netherrackOre(NetherQuartzOre, 14), newTreeRNG(5, 8, 8), 8, 40, 8, top, 5, 1)
		return countIn(ch, NetherQuartzOre)
	}
	if n := blob(&buildIndex{}); n < 3 {
		t.Fatalf("a size-14 quartz blob in netherrack placed %d ore", n)
	}
	guard := &buildIndex{buckets: map[[3]int][][3]int{{8 >> 2, 40 >> 2, 8 >> 2}: {{8, 40, 8}}}}
	if n := blob(guard); n != 0 {
		t.Fatalf("a blob with a build in its box placed %d ore", n)
	}
}

// The placements' biome filters: soul sand only in the soul sand valley;
// the basalt deltas take their own gold and quartz and no gravel or
// blackstone blobs.
func TestNetherOrePlacementBiomes(t *testing.T) {
	skipHeavy(t)
	g := NewNetherGenerator(11)
	run := func(cx, cz int32) *Chunk {
		ch := netherrackChunk(g)
		g.placeNetherOres(ch, cx, cz)
		return ch
	}
	if cx, cz, ok := netherBiomeArea(g, "minecraft:soul_sand_valley"); ok {
		if n := countIn(run(cx, cz), SoulSand); n == 0 {
			t.Error("no soul sand ore in a soul sand valley chunk")
		}
	} else {
		t.Log("no soul sand valley area for this seed")
	}
	if cx, cz, ok := netherBiomeArea(g, "minecraft:nether_wastes"); ok {
		ch := run(cx, cz)
		if n := countIn(ch, SoulSand); n != 0 {
			t.Errorf("%d soul sand ore in the nether wastes", n)
		}
		if countIn(ch, NetherQuartzOre) == 0 || countIn(ch, NetherGoldOre) == 0 {
			t.Error("the wastes should hold gold and quartz")
		}
	} else {
		t.Log("no nether wastes area for this seed")
	}
	if cx, cz, ok := netherBiomeArea(g, "minecraft:basalt_deltas"); ok {
		ch := run(cx, cz)
		if n := countIn(ch, Gravel) + countIn(ch, Blackstone); n != 0 {
			t.Errorf("%d gravel or blackstone ore in the basalt deltas", n)
		}
		if countIn(ch, NetherQuartzOre) == 0 {
			t.Error("the deltas should hold their own quartz")
		}
	} else {
		t.Log("no basalt deltas area for this seed")
	}
}

// findOutpost is the first pillager outpost of the generator's seed near
// the origin.
func findOutpost(g *Generator) PillagerOutpost {
	var op PillagerOutpost
	for i := -20; i <= 20 && !op.Exists; i++ {
		for j := -20; j <= 20 && !op.Exists; j++ {
			op = g.OutpostIn(i*outpostCell+outpostCell/2, j*outpostCell+outpostCell/2)
		}
	}
	return op
}

// JigsawPlacement's ground level: the start's groundLevelDelta is one, a
// rigid chain keeps one ground (each child's box moves by the connection's
// deltaY and its delta by minus that), and every connection leaves a
// junction on each of its two pieces.
func TestJigsawGroundLevelAndJunctions(t *testing.T) {
	g := NewGenerator(1)
	op := findOutpost(g)
	if !op.Exists {
		t.Skip("no outpost for this seed")
	}
	pieces := g.AssembleOutpost(op)
	if len(pieces) < 2 {
		t.Fatalf("the outpost assembled %d pieces", len(pieces))
	}
	if pieces[0].gld != 1 {
		t.Fatalf("start piece gld %d, want 1", pieces[0].gld)
	}
	ground := pieces[0].OY + 1
	templ, junctions := 0, 0
	for i := range pieces {
		p := &pieces[i]
		if p.Tmpl == nil {
			continue
		}
		templ++
		junctions += len(p.junctions)
		if p.OY+p.gld != ground {
			t.Errorf("piece %s: ground %d, the rigid chain's is %d", p.Tmpl.name, p.OY+p.gld, ground)
		}
		for _, j := range p.junctions {
			// A vertical connection's far side sits a step off (the
			// junction's ground less the source jigsaw's facing step).
			if j[1] < ground-1 || j[1] > ground+1 {
				t.Errorf("piece %s: a rigid junction's ground %d, want %d±1", p.Tmpl.name, j[1], ground)
			}
		}
	}
	if junctions != 2*(templ-1) {
		t.Errorf("%d junctions over %d template pieces, want two per connection", junctions, templ)
	}
}

// touchAt gives the generator one player block as its edit overlay.
func touchAt(g *Generator, x, y, z int) {
	edits := map[[3]int]uint32{{x, y, z}: BlockBase("oak_planks")}
	g.SetEditLookup(func(x, y, z int) (uint32, bool) { s, ok := edits[[3]int{x, y, z}]; return s, ok })
	g.SetEditRegion(func(ecx, ecz int32, fn func(x, y, z int, s uint32)) {
		for p, s := range edits {
			if int32(floorDiv16(p[0])) == ecx && int32(floorDiv16(p[2])) == ecz {
				fn(p[0], p[1], p[2], s)
			}
		}
	})
}

// rigidFor is the adaptation's rigid for a piece's box in a chunk, if any.
func rigidFor(g *Generator, p *PlacedPiece) (beardRigid, bool) {
	b := fbox{p.OX, p.OY, p.OZ, p.x1 - 1, p.y1 - 1, p.z1 - 1}
	for _, r := range g.adaptRigids(int32(floorDiv16(p.OX)), int32(floorDiv16(p.OZ))) {
		if r.box == b && r.kind != adaptJunction {
			return r, true
		}
	}
	return beardRigid{}, false
}

// A village takes beard_thin: its rigid pieces are the chunk's rigids at
// their own ground, with junction beards, and its pieces stamp without the
// old dirt beard; a village a player touched is left out of the adaptation
// and keeps its dirt beards.
func TestVillageTakesBeardThin(t *testing.T) {
	g := NewGenerator(1)
	var v Village
	for i := -12; i <= 12 && !v.Exists; i++ {
		for j := -12; j <= 12 && !v.Exists; j++ {
			v = g.VillageIn(i*villageCell+villageCell/2, j*villageCell+villageCell/2)
		}
	}
	if !v.Exists {
		t.Skip("no village for this seed")
	}
	pieces := g.AssembleVillage(v)
	var house *PlacedPiece
	for i := range pieces {
		if p := &pieces[i]; p.Tmpl != nil && !p.TerrainMatch && i > 0 {
			house = p
			break
		}
	}
	if house == nil {
		t.Skip("the village has no rigid piece past its centre")
	}
	if !g.jigsawAdapted(pieces) {
		t.Fatal("an untouched village should take the adaptation")
	}
	r, ok := rigidFor(g, house)
	if !ok || r.kind != adaptBeardThin || r.groundY != house.OY+house.gld {
		t.Fatalf("the house should be a beard_thin rigid at its own ground %d: %+v %v", house.OY+house.gld, r, ok)
	}
	hasJunction := false
	for _, rr := range g.adaptRigids(int32(floorDiv16(house.OX)), int32(floorDiv16(house.OZ))) {
		if rr.kind == adaptJunction {
			hasJunction = true
		}
	}
	if !hasJunction {
		t.Error("the village's junctions should beard the chunk too")
	}

	touchAt(g, house.OX, house.OY+1, house.OZ)
	if g.jigsawAdapted(pieces) {
		t.Fatal("a touched village should keep its old layout")
	}
	if _, ok := rigidFor(g, house); ok {
		t.Fatal("a touched village's house is still adapted")
	}
}

// A camp takes beard_thin the same way.
func TestAbandonedCampTakesBeardThin(t *testing.T) {
	g := NewGenerator(1)
	var c AbandonedCamp
	for i := -16; i <= 16 && !c.Exists; i++ {
		for j := -16; j <= 16 && !c.Exists; j++ {
			c = g.AbandonedCampIn(i*campCell+campCell/2, j*campCell+campCell/2)
		}
	}
	if !c.Exists {
		t.Skip("no abandoned camp for this seed")
	}
	pieces := g.AssembleAbandonedCamp(c)
	if len(pieces) == 0 || pieces[0].Tmpl == nil {
		t.Skip("the camp assembled no tent")
	}
	r, ok := rigidFor(g, &pieces[0])
	if !ok || r.kind != adaptBeardThin {
		t.Fatalf("the camp's tent should be a beard_thin rigid: %+v %v", r, ok)
	}
}

// A Nether fossil takes beard_thin: rock over its floor within its box is
// shaved away and a cave under it filled; a fossil a player built near is
// left alone.
func TestNetherFossilBeardThin(t *testing.T) {
	g := NewNetherGenerator(11)
	if TemplateByName("nether_fossils/fossil_1") == nil {
		t.Skip("no fossil templates")
	}
	var f NetherFossil
	for i := -40; i <= 40 && !f.Exists; i++ {
		for j := -40; j <= 40 && !f.Exists; j++ {
			f = g.NetherFossilIn(i*netherFossilCell, j*netherFossilCell)
		}
	}
	if !f.Exists {
		t.Skip("no nether fossil for this seed")
	}
	cx, cz := int32(floorDiv16(f.X)), int32(floorDiv16(f.Z))
	lx, lz := f.X-int(cx)*16, f.Z-int(cz)*16
	adapt := func() *Chunk {
		ch := netherrackChunk(g)
		setSectionBlock(ch, lx, f.Y-2, lz, Air, true) // a cave cell under the fossil's floor
		g.adaptNetherTerrain(ch, cx, cz)
		return ch
	}
	ch := adapt()
	if s := sectionBlockAt(ch, lx, f.Y+1, lz); s != Air {
		t.Errorf("the rock over the fossil's floor was not shaved: %d", s)
	}
	if s := sectionBlockAt(ch, lx, f.Y-2, lz); s != Netherrack {
		t.Errorf("the cave under the fossil was not filled: %d", s)
	}

	touchAt(g, f.X, f.Y+3, f.Z)
	ch = adapt()
	if s := sectionBlockAt(ch, lx, f.Y+1, lz); s != Netherrack {
		t.Error("a touched fossil's ground was adapted")
	}
}
