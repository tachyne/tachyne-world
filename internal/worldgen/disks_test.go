package worldgen

import "testing"

// Clay has to exist somewhere a player will look for it. Before the disks the
// only clay in the overworld was the floor of a lush cave.
func TestDisksPutClayInRiverAndSeaBeds(t *testing.T) {
	g := NewGenerator(1)
	// Rings outward from the origin, stopping at the first clay: the claim is
	// only that disks place clay somewhere, and water is near. The sweep used
	// to generate all 48×48 chunks, nearly five minutes under -race — the
	// longest test in the suite by far. The reach is unchanged, so "no clay
	// anywhere in 48x48 chunks" still fails the same way.
	for r := int32(0); r < 24; r++ {
		for cx := -r; cx <= r; cx++ {
			for cz := -r; cz <= r; cz++ {
				if abs32(cx) != r && abs32(cz) != r {
					continue // the ring only; the inside was done
				}
				ch := g.GenerateChunk(cx, cz)
				for si := range ch.Sections {
					for _, s := range ch.Sections[si] {
						if s == Clay {
							t.Logf("first clay in chunk %d,%d (ring %d)", cx, cz, r)
							return
						}
					}
				}
			}
		}
	}
	t.Fatal("no clay anywhere in 48x48 chunks: the disks are not placing")
}

// A disk is a circle of the sampled radius, and it only replaces what its
// target list names — it never eats stone, and never breaks the surface.
func TestDiskReplacesOnlyItsTargets(t *testing.T) {
	spec := diskSpec{block: Clay, radiusLo: 3, radiusHi: 3, halfHeight: 1,
		targets: []uint32{Dirt, Clay}}
	g := NewGenerator(1)

	world := map[[3]int]uint32{}
	at := func(x, y, z int) uint32 {
		if s, ok := world[[3]int{x, y, z}]; ok {
			return s
		}
		return Stone
	}
	put := func(x, y, z int, s uint32) { world[[3]int{x, y, z}] = s }
	for dx := -5; dx <= 5; dx++ { // a dirt floor at y=60, stone under it
		for dz := -5; dz <= 5; dz++ {
			world[[3]int{dx, 60, dz}] = Dirt
		}
	}
	g.placeDisk(spec, 3, 0, 60, 0, func(x, z int) bool { return true }, at, put)

	if at(0, 60, 0) != Clay {
		t.Fatal("the centre of the disk should be clay")
	}
	if at(3, 60, 0) != Clay {
		t.Fatal("a cell exactly at the radius should be clay")
	}
	if at(4, 60, 0) != Dirt {
		t.Fatal("a cell beyond the radius should be untouched")
	}
	if at(0, 59, 0) != Stone {
		t.Fatal("stone is not in the target list and must be left alone")
	}
}

// The floor under shallow water is dirt, not gravel: gravel is vanilla's DEEP
// fallback. Measured against a real 1.21.11 world — of 92,062 open-water
// columns at sea level, 9,481 sat on dirt — and it is what the clay disks
// need to land on.
func TestShallowWaterFloorIsDirt(t *testing.T) {
	g := NewGenerator(1)
	shallowDirt, deepGravel := 0, 0
	for x := -400; x < 400; x += 2 {
		for z := -400; z < 400; z += 2 {
			h, ok := g.seafloorCol(x, z)
			if !ok {
				continue
			}
			top := g.columnAt(x, z).topBlock()
			if h >= SeaLevel-6 && top == Dirt {
				shallowDirt++
			}
			if h < SeaLevel-6 && top == Gravel {
				deepGravel++
			}
		}
	}
	if shallowDirt == 0 {
		t.Fatal("a floor within six blocks of the surface should be dirt")
	}
	if deepGravel == 0 {
		t.Fatal("a floor deeper than that should still be gravel")
	}
	t.Logf("shallow dirt=%d deep gravel=%d", shallowDirt, deepGravel)
}

// Vanilla's per-biome disk lists: both swamps take the clay disk and
// neither the sand nor the gravel one; disk_grass is the mangrove swamp's.
func TestDiskBiomeLists(t *testing.T) {
	for _, spec := range diskFeatures() {
		for _, b := range []string{"minecraft:swamp", "minecraft:mangrove_swamp"} {
			if got, want := spec.notIn[b], spec.block != Clay; got != want {
				t.Errorf("disk of %d in %s: left out %v, want %v", spec.block, b, got, want)
			}
		}
		if spec.notIn["minecraft:river"] || spec.notIn["minecraft:ocean"] {
			t.Errorf("disk of %d left out of the river or the sea", spec.block)
		}
	}
}

// disk_grass's state rule: grass where the cell is open to the sky, dirt
// under water or under anything solid; mud and dirt are its targets.
func TestGrassDiskRule(t *testing.T) {
	g := NewGenerator(1)
	world := map[[3]int]uint32{}
	at := func(x, y, z int) uint32 {
		if s, ok := world[[3]int{x, y, z}]; ok {
			return s
		}
		return Air
	}
	put := func(x, y, z int, s uint32) { world[[3]int{x, y, z}] = s }
	for dx := -4; dx <= 4; dx++ {
		for dz := -4; dz <= 4; dz++ {
			for y := 55; y <= 60; y++ {
				world[[3]int{dx, y, dz}] = Mud
			}
		}
	}
	world[[3]int{1, 61, 0}] = Water
	world[[3]int{-1, 61, 0}] = Stone
	g.placeDisk(grassDisk, 2, 0, 60, 0, func(x, z int) bool { return true }, at, put)
	for _, c := range []struct {
		x, y, z int
		want    uint32
	}{
		{0, 60, 0, GrassBlock}, // open above
		{1, 60, 0, Dirt},       // under water
		{-1, 60, 0, Dirt},      // under stone
		{0, 59, 0, Dirt},       // under the grass just placed
		{0, 58, 0, Dirt},       // the disk's floor: half_height 2 below, less one
		{0, 57, 0, Mud},        // under the disk
		{3, 60, 0, Mud},        // beyond the radius
	} {
		if s := at(c.x, c.y, c.z); s != c.want {
			t.Errorf("%d,%d,%d: got %d, want %d", c.x, c.y, c.z, s, c.want)
		}
	}
}

// disk_grass turns the mangrove swamp's mud to grass here and there, and
// leaves out a disk whose circle holds a player's build.
func TestGrassDiskInMangroveSwamp(t *testing.T) {
	g := NewGenerator(1)
	// grassOnMud lays a chunk's disks over bare terrain (muddied: its
	// surface turned to mud first) and counts the mud they turn.
	grassOnMud := func(cx, cz int32, muddied bool) int {
		ch := terrainChunk(g, cx, cz)
		before := map[[3]int]bool{}
		for lx := 0; lx < 16; lx++ {
			for lz := 0; lz < 16; lz++ {
				h := g.columnAt(int(cx)*16+lx, int(cz)*16+lz).h
				if muddied && sectionBlockAt(ch, lx, h-1, lz) != Air {
					setSectionBlock(ch, lx, h-1, lz, Mud, true)
				}
				if sectionBlockAt(ch, lx, h-1, lz) == Mud {
					before[[3]int{lx, h - 1, lz}] = true
				}
			}
		}
		g.decorateDisks(ch, cx, cz)
		n := 0
		for p := range before {
			if s := sectionBlockAt(ch, p[0], p[1], p[2]); s == GrassBlock || s == Dirt {
				n++
			}
		}
		return n
	}
	chunks := findBiomeChunks(g, "minecraft:mangrove_swamp", 24)
	if len(chunks) == 0 {
		t.Skip("no mangrove swamp for this seed")
	}
	total := 0
	var hit [2]int32
	for _, p := range chunks {
		if n := grassOnMud(p[0], p[1], false); n > 0 {
			total += n
			hit = p
		}
	}
	t.Logf("mud turned by grass disks: %d cells over %d chunks", total, len(chunks))
	if total == 0 {
		t.Fatal("no grass disk on the mangrove swamp's mud")
	}
	// Outside the mangrove swamp no mud turns, even where there is mud:
	// a swamp and a plains chunk with their surface muddied.
	for _, p := range append(surroundedChunks(g, "minecraft:swamp", 4), surroundedChunks(g, "minecraft:plains", 4)...) {
		if n := grassOnMud(p[0], p[1], true); n != 0 {
			t.Errorf("chunk %v: %d mud cells turned outside the mangrove swamp", p, n)
		}
	}
	// A build block over every column of the chunk and its neighbours:
	// every disk reaching it has the build in its box and is left out.
	edits := map[[3]int]uint32{}
	planks := BlockBase("oak_planks")
	for x := int(hit[0])*16 - 16; x < int(hit[0])*16+32; x++ {
		for z := int(hit[1])*16 - 16; z < int(hit[1])*16+32; z++ {
			edits[[3]int{x, g.columnAt(x, z).h + 3, z}] = planks
		}
	}
	g.SetEditRegion(func(ecx, ecz int32, fn func(x, y, z int, s uint32)) {
		for p, s := range edits {
			if int32(floorDiv16(p[0])) == ecx && int32(floorDiv16(p[2])) == ecz {
				fn(p[0], p[1], p[2], s)
			}
		}
	})
	if n := grassOnMud(hit[0], hit[1], false); n != 0 {
		t.Errorf("%d mud cells turned under a build", n)
	}
}
