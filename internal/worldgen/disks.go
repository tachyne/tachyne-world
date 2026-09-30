package worldgen

// Sand, clay and gravel disks — the flat patches vanilla lays in the bed of
// every river, lake and shallow sea. They are the reason clay exists anywhere
// a player will look for it: without them the only clay in the world is the
// floor of a lush cave, and bricks, pots and a mason's whole trade are out of
// reach of anyone who has not gone spelunking.
//
// Vanilla runs three placed features over the OCEAN_FLOOR_WG heightmap, each
// filtered to columns whose floor is under water: disk_sand three times a
// chunk, disk_clay and disk_gravel once each. DiskFeature then fills a circle
// of the sampled radius, walking each column from halfHeight above the origin
// down to halfHeight below it and replacing whatever the target set names.
//
// Each ends in the biome filter, asked at its origin: the swamp and the
// mangrove swamp take the clay disk but neither sand nor gravel, and the
// mangrove swamp has disk_grass of its own — its mud floor turned to grass
// (dirt where anything stands on it or water covers it), which is where the
// swamp's patches of solid ground come from.

const diskSalt = 0x0D15C5

// diskSpec is one configured disk feature.
type diskSpec struct {
	block      uint32   // what it is made of
	radiusLo   int      // uniform radius, inclusive both ends
	radiusHi   int      //
	halfHeight int      //
	targets    []uint32 // the blocks it may replace
	count      int      // placements per chunk
	// overAir is the state rule sand carries: a cell with nothing under it
	// becomes sandstone instead, so a disk on the lip of a drop does not
	// crumble away the moment it is looked at.
	overAir uint32
	// notIn is the biomes whose feature lists leave the disk out.
	notIn map[string]bool
	// grassRule is disk_grass's state rule: grass where nothing solid and
	// no water is over the cell, dirt otherwise.
	grassRule bool
}

// noSandDisks is where vanilla's feature lists have no sand or gravel disk.
var noSandDisks = biomeSet("swamp", "mangrove_swamp")

// grassDisk is disk_grass: mud (or dirt) to grass, in the mangrove swamp.
var grassDisk = diskSpec{block: GrassBlock, radiusLo: 2, radiusHi: 6, halfHeight: 2,
	targets: []uint32{Dirt, Mud}, count: 1, grassRule: true}

// diskGrassSalt seeds disk_grass's own draws, so the other disks keep theirs.
const diskGrassSalt = 0x6A55D15C

// diskFeatures is the three overworld disks, in vanilla's own feature order.
func diskFeatures() []diskSpec {
	return []diskSpec{
		{block: Sand, radiusLo: 2, radiusHi: 6, halfHeight: 2,
			targets: []uint32{Dirt, GrassBlock}, count: 3, overAir: Sandstone, notIn: noSandDisks},
		{block: Clay, radiusLo: 2, radiusHi: 3, halfHeight: 1,
			targets: []uint32{Dirt, Clay}, count: 1},
		{block: Gravel, radiusLo: 2, radiusHi: 5, halfHeight: 2,
			targets: []uint32{Dirt, GrassBlock}, count: 1, notIn: noSandDisks},
	}
}

// decorateDisks lays the disks whose origins fall in this chunk or spill into
// it from a neighbour. Origins are drawn per ORIGIN chunk from a hashed
// stream, exactly as the sea-floor features are, so no chunk depends on
// another having been generated first.
func (g *Generator) decorateDisks(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	inChunk := func(x, z int) bool { return x >= baseX && x < baseX+16 && z >= baseZ && z < baseZ+16 }
	at := func(x, y, z int) uint32 { return sectionBlockAt(ch, x-baseX, y, z-baseZ) }
	_ = at
	put := func(x, y, z int, s uint32) { setSectionBlock(ch, x-baseX, y, z-baseZ, s, true) }

	for ncx := cx - 1; ncx <= cx+1; ncx++ {
		for ncz := cz - 1; ncz <= cz+1; ncz++ {
			ox, oz := int(ncx)*16, int(ncz)*16
			// disk_grass comes first in the mangrove swamp's list, from its
			// own stream.
			g.grassDiskAt(ox, oz, inChunk, at, put)
			r := newTreeRNG(g.seed^diskSalt, ox, oz)
			for _, spec := range diskFeatures() {
				for i := 0; i < spec.count; i++ {
					// in_square: a column anywhere in the origin chunk.
					x, z := ox+r.Intn(16), oz+r.Intn(16)
					radius := spec.radiusLo + r.Intn(spec.radiusHi-spec.radiusLo+1)
					// HeightmapPlacement(OCEAN_FLOOR_WG) picks the cell above
					// the floor; the filter then demands water in it, which is
					// what keeps disks in river and sea beds and out of fields.
					// The column is asked of the GENERATOR, not of the chunk:
					// eight origins in nine belong to a neighbour, whose cells
					// this chunk cannot read.
					col := g.columnAt(x, z)
					y, ok := col.seafloor()
					if !ok || spec.notIn[col.biome.Name] {
						continue
					}
					g.placeDisk(spec, radius, x, y, z, inChunk, at, put)
				}
			}
		}
	}
}

// grassDiskAt replays origin chunk (ox, oz)'s disk_grass: in_square, the
// OCEAN_FLOOR_WG heightmap one down — the floor block itself, which must be
// mud — in the mangrove swamp. It is new in explored land, so a disk with a
// player's build in its circle, up to a roof's reach above, is left out
// whole.
func (g *Generator) grassDiskAt(ox, oz int, inChunk func(x, z int) bool, at func(x, y, z int) uint32, put func(x, y, z int, s uint32)) {
	r := newTreeRNG(g.seed^diskGrassSalt, ox, oz)
	x, z := ox+r.Intn(16), oz+r.Intn(16)
	radius := grassDisk.radiusLo + r.Intn(grassDisk.radiusHi-grassDisk.radiusLo+1)
	col := g.columnAt(x, z)
	if col.biome.Name != "minecraft:mangrove_swamp" || col.topBlock() != Mud {
		return
	}
	y := col.h - 1
	if g.builtIn(x-radius, y-grassDisk.halfHeight-1, z-radius, x+radius, y+buildRoofReach, z+radius) {
		return
	}
	g.placeDisk(grassDisk, radius, x, y, z, inChunk, at, put)
}

// placeDisk is DiskFeature.place: a circle of the sampled radius, each column
// walked from halfHeight above the origin down to halfHeight below it.
func (g *Generator) placeDisk(spec diskSpec, radius, cx, cy, cz int,
	inChunk func(x, z int) bool, at func(x, y, z int) uint32, put func(x, y, z int, s uint32)) {
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			if dx*dx+dz*dz > radius*radius {
				continue
			}
			x, z := cx+dx, cz+dz
			if !inChunk(x, z) {
				continue // a neighbour's chunk will lay its own half
			}
			for y := cy + spec.halfHeight; y > cy-spec.halfHeight-1; y-- {
				cur := at(x, y, z)
				if !diskTargets(spec, cur) {
					continue
				}
				s := spec.block
				if spec.overAir != 0 && at(x, y-1, z) == Air {
					s = spec.overAir
				}
				if spec.grassRule {
					if above := at(x, y+1, z); solid(above) || IsWater(above) {
						s = Dirt
					}
				}
				put(x, y, z, s)
			}
		}
	}
}

func diskTargets(spec diskSpec, state uint32) bool {
	for _, t := range spec.targets {
		if state == t {
			return true
		}
	}
	return false
}
