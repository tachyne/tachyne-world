package worldgen

import "testing"

// Clay has to exist somewhere a player will look for it. Before the disks the
// only clay in the overworld was the floor of a lush cave.
func TestDisksPutClayInRiverAndSeaBeds(t *testing.T) {
	g := NewGenerator(1)
	clay, sand, gravel := 0, 0, 0
	// A wide sweep so the sample crosses real water somewhere.
	for cx := int32(-24); cx < 24; cx++ {
		for cz := int32(-24); cz < 24; cz++ {
			ch := g.GenerateChunk(cx, cz)
			for si := range ch.Sections {
				for _, s := range ch.Sections[si] {
					switch s {
					case Clay:
						clay++
					case Sand:
						sand++
					case Gravel:
						gravel++
					}
				}
			}
		}
	}
	if clay == 0 {
		t.Fatal("no clay anywhere in 48x48 chunks: the disks are not placing")
	}
	t.Logf("clay=%d sand=%d gravel=%d", clay, sand, gravel)
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
