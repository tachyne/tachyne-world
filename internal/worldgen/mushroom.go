package worldgen

// Huge mushrooms — AbstractHugeMushroomFeature and its red/brown cap builders
// in the 1.21.11 shape: the ground rule lives in code (#dirt or
// #mushroom_grow_block; the driver's DirtGround answers it, which covers
// every overworld grow block — the nether nyliums belong to the huge fungi,
// a different feature). The envelope check carries vanilla's own quirk:
// getTreeRadiusForHeight is called with treeHeight=-1, which collapses the
// RED mushroom's brackets to zero — it validates only its centre column —
// while the brown checks its full radius above layer three. Ported as-is:
// the oracle outranks tidiness.

// mushFaceState packs a mushroom block's six face booleans (down, east,
// north, south, up, west — true first, so a FALSE face adds its bit).
func mushFaceState(base uint32, down, east, north, south, up, west bool) uint32 {
	idx := uint32(0)
	for i, b := range [6]bool{down, east, north, south, up, west} {
		if !b {
			idx += 1 << (5 - i)
		}
	}
	return base + idx
}

// mushReplaceable is #replaceable_by_mushrooms (minus air, the caller's
// case): leaves, water, the small plants, and mushroom blocks themselves.
func mushReplaceable(state uint32) bool {
	if IsLeaves(state) || IsWater(state) {
		return true
	}
	for _, n := range mushReplaceableNames {
		lo, hi := BlockRange(n)
		if state >= lo && state <= hi {
			return true
		}
	}
	return false
}

var mushReplaceableNames = []string{
	// #small_flowers
	"dandelion", "open_eyeblossom", "poppy", "blue_orchid", "allium",
	"azure_bluet", "red_tulip", "orange_tulip", "white_tulip", "pink_tulip",
	"oxeye_daisy", "cornflower", "lily_of_the_valley", "wither_rose",
	"torchflower", "closed_eyeblossom",
	// the rest of the tag
	"pale_moss_carpet", "short_grass", "fern", "dead_bush", "vine",
	"glow_lichen", "sunflower", "lilac", "rose_bush", "peony", "tall_grass",
	"large_fern", "hanging_roots", "pitcher_plant", "seagrass",
	"tall_seagrass", "brown_mushroom", "red_mushroom",
	"brown_mushroom_block", "red_mushroom_block", "warped_roots",
	"nether_sprouts", "crimson_roots", "leaf_litter", "short_dry_grass",
	"tall_dry_grass", "bush", "firefly_bush",
}

// PlaceHugeMushroom grows one huge mushroom at (x,y,z) and reports whether
// it grew. Height is 4-6 with a one-in-twelve double; the cap goes in first
// and the stem after, each block only into air or #replaceable_by_mushrooms,
// exactly as placeMushroomBlock has it.
func PlaceHugeMushroom(brown bool, x, y, z int, rng TreeRNG, d TreeDriver) bool {
	height := rng.Intn(3) + 4
	if rng.Intn(12) == 0 {
		height *= 2
	}
	if y-1 <= MinY || !d.DirtGround(x, y-1, z) {
		return false
	}
	for dy := 0; dy <= height; dy++ {
		r := 0
		if brown && dy > 3 {
			r = 3
		}
		for dx := -r; dx <= r; dx++ {
			for dz := -r; dz <= r; dz++ {
				if !d.Free(x+dx, y+dy, z+dz) {
					return false
				}
			}
		}
	}
	put := func(px, py, pz int, st uint32) {
		cur := d.Read(px, py, pz)
		if cur == Air || mushReplaceable(cur) {
			d.Set(px, py, pz, st, false)
		}
	}
	if brown {
		// A flat radius-3 plate at the top, corners cut, with a two-deep
		// ring of side faces.
		capBase := blockBase("brown_mushroom_block")
		radius := 3
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				minX, maxX := dx == -radius, dx == radius
				minZ, maxZ := dz == -radius, dz == radius
				xE, zE := minX || maxX, minZ || maxZ
				if xE && zE {
					continue
				}
				west := minX || zE && dx == 1-radius
				east := maxX || zE && dx == radius-1
				north := minZ || xE && dz == 1-radius
				south := maxZ || xE && dz == radius-1
				put(x+dx, y+height, z+dz, mushFaceState(capBase, false, east, north, south, true, west))
			}
		}
	} else {
		// A hollow shell over the top four layers: side rows where exactly
		// one axis is at its rim, a full plate on top, faces trimmed by the
		// distance past the centre.
		capBase := blockBase("red_mushroom_block")
		radius := 2
		center := radius - 2
		for dy := height - 3; dy <= height; dy++ {
			r := radius
			if dy >= height {
				r = radius - 1
			}
			for dx := -r; dx <= r; dx++ {
				for dz := -r; dz <= r; dz++ {
					xE := dx == -r || dx == r
					zE := dz == -r || dz == r
					if dy < height && xE == zE {
						continue
					}
					put(x+dx, y+dy, z+dz, mushFaceState(capBase, false,
						dx > center, dz < -center, dz > center, dy >= height-1, dx < -center))
				}
			}
		}
	}
	stem := mushFaceState(blockBase("mushroom_stem"), false, true, true, true, false, true)
	for dy := 0; dy < height; dy++ {
		put(x, y+dy, z, stem)
	}
	return true
}
