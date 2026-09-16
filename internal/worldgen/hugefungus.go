package worldgen

// Huge fungi — HugeFungusFeature. One placer serves the crimson and warped
// forests' generation (where one in seventeen is HUGE: a three-wide stem and
// a wider hat) and bone meal on a planted fungus (never huge; a plant it
// pushes through breaks with its drops). The stem is four to thirteen tall,
// doubled one time in twelve; the hat of wart blocks starts five to a third
// of the way down, widens to two (three on tall hats) and is hung with
// shroomlights and, on a crimson one, weeping vines.

var (
	CrimsonNylium   = blockBase("crimson_nylium")
	WarpedNylium    = blockBase("warped_nylium")
	CrimsonFungus   = blockBase("crimson_fungus")
	WarpedFungus    = blockBase("warped_fungus")
	crimsonStem     = blockID("crimson_stem")
	warpedStem      = blockID("warped_stem")
	NetherWartBlock = blockBase("nether_wart_block")
	WarpedWartBlock = blockBase("warped_wart_block")
	shroomlight     = blockBase("shroomlight")
	weepingVinesLo  = func() uint32 { lo, _ := BlockRange("weeping_vines"); return lo }()
	weepingVinesHi  = func() uint32 { _, hi := BlockRange("weeping_vines"); return hi }()
	weepingPlant    = blockBase("weeping_vines_plant")
	twistingVinesLo = func() uint32 { lo, _ := BlockRange("twisting_vines"); return lo }()
	twistingVinesHi = func() uint32 { _, hi := BlockRange("twisting_vines"); return hi }()
	twistingPlant   = blockBase("twisting_vines_plant")
	stemPlantRanges = func() [][2]uint32 {
		var out [][2]uint32
		for _, n := range []string{"wheat", "carrots", "potatoes", "beetroots", "torchflower_crop", "pitcher_crop", "sweet_berry_bush",
			"crimson_fungus", "warped_fungus", "weeping_vines", "weeping_vines_plant", "twisting_vines", "twisting_vines_plant"} {
			lo, hi := BlockRange(n)
			out = append(out, [2]uint32{lo, hi})
		}
		return out
	}()
)

// FungusStemReplaceable is TreeFeatures' stemReplaceableBlocks: the plants a
// growing stem may push through.
func FungusStemReplaceable(s uint32) bool {
	for _, r := range stemPlantRanges {
		if s >= r[0] && s <= r[1] {
			return true
		}
	}
	return false
}

// FungusDriver is what the placer needs of a world: cells to read, cells to
// set, and (planted only) a block to break with its drops before a stem or
// hat block replaces it.
type FungusDriver struct {
	Read    func(x, y, z int) uint32
	Set     func(x, y, z int, s uint32)
	Destroy func(x, y, z int)
	InWorld func(y int) bool
}

// PlaceHugeFungus grows a fungus at (x,y,z), the cell the small fungus
// occupies; below it must be the fungus's own nylium. Returns whether it
// grew.
func PlaceHugeFungus(r TreeRNG, x, y, z int, warped, planted bool, d FungusDriver) bool {
	nylium, stem, hat := CrimsonNylium, crimsonStem, NetherWartBlock
	if warped {
		nylium, stem, hat = WarpedNylium, warpedStem, WarpedWartBlock
	}
	if d.Read(x, y-1, z) != nylium {
		return false
	}
	total := 4 + r.Intn(10)
	if r.Intn(12) == 0 {
		total *= 2
	}
	if !planted && d.InWorld != nil && !d.InWorld(y+total+1) {
		return false
	}
	huge := !planted && r.Float64() < 0.06
	replaceable := func(px, py, pz int, plants bool) bool {
		s := d.Read(px, py, pz)
		return s == Air || IsReplaceable(s) || (plants && FungusStemReplaceable(s))
	}
	put := func(px, py, pz int, s uint32) {
		if planted && d.Destroy != nil && d.Read(px, py, pz) != Air && d.Read(px, py-1, pz) != Air {
			d.Destroy(px, py, pz)
		}
		d.Set(px, py, pz, s)
	}
	d.Set(x, y, z, Air)
	// placeStem
	radius := 0
	if huge {
		radius = 1
	}
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			corner := huge && absInt(dx) == radius && absInt(dz) == radius
			for dy := 0; dy < total; dy++ {
				px, py, pz := x+dx, y+dy, z+dz
				if !replaceable(px, py, pz, true) {
					continue
				}
				switch {
				case planted:
					put(px, py, pz, stem)
				case corner:
					if r.Float64() < 0.1 {
						d.Set(px, py, pz, stem)
					}
				default:
					d.Set(px, py, pz, stem)
				}
			}
		}
	}
	// placeHat
	vines := hat == NetherWartBlock
	hatHeight := r.Intn(1+total/3) + 5
	if hatHeight > total {
		hatHeight = total
	}
	hatStart := total - hatHeight
	hatBlock := func(px, py, pz int, decorP, hatP, vinesP float64) {
		if r.Float64() < decorP {
			put(px, py, pz, shroomlight)
		} else if r.Float64() < hatP {
			put(px, py, pz, hat)
			if r.Float64() < vinesP {
				tryWeepingVines(r, px, py, pz, d)
			}
		}
	}
	for dy := hatStart; dy <= total; dy++ {
		rad := 1
		if dy < total-r.Intn(3) {
			rad = 2
		}
		if hatHeight > 8 && dy < hatStart+4 {
			rad = 3
		}
		if huge {
			rad++
		}
		for dx := -rad; dx <= rad; dx++ {
			for dz := -rad; dz <= rad; dz++ {
				edgeX, edgeZ := dx == -rad || dx == rad, dz == -rad || dz == rad
				inside := !edgeX && !edgeZ && dy != total
				corner := edgeX && edgeZ
				bottom := dy < hatStart+3
				px, py, pz := x+dx, y+dy, z+dz
				if !replaceable(px, py, pz, false) {
					continue
				}
				if planted && d.Destroy != nil && d.Read(px, py, pz) != Air && d.Read(px, py-1, pz) != Air {
					d.Destroy(px, py, pz)
				}
				switch {
				case bottom:
					if !inside { // placeHatDropBlock
						if d.Read(px, py-1, pz) == hat {
							d.Set(px, py, pz, hat)
						} else if r.Float64() < 0.15 {
							d.Set(px, py, pz, hat)
							if vines && r.Intn(11) == 0 {
								tryWeepingVines(r, px, py, pz, d)
							}
						}
					}
				case inside:
					hatBlock(px, py, pz, 0.1, 0.2, vinesOr(vines, 0.1))
				case corner:
					hatBlock(px, py, pz, 0.01, 0.7, vinesOr(vines, 0.083))
				default:
					hatBlock(px, py, pz, 5.0e-4, 0.98, vinesOr(vines, 0.07))
				}
			}
		}
	}
	return true
}

func vinesOr(on bool, p float64) float64 {
	if on {
		return p
	}
	return 0
}

// tryWeepingVines is HugeFungusFeature.tryPlaceWeepingVines: below a hat
// block, a column one to five long (doubled one time in seven), aged 23–25.
func tryWeepingVines(r TreeRNG, x, y, z int, d FungusDriver) {
	if d.Read(x, y-1, z) != Air {
		return
	}
	n := 1 + r.Intn(5)
	if r.Intn(7) == 0 {
		n *= 2
	}
	WeepingVinesColumn(r, x, y-1, z, n, 23, 25, d)
}

// WeepingVinesColumn is WeepingVinesFeature.placeWeepingVinesColumn: down
// from (x,y,z), body until the last cell or a block below, which is a head
// aged minAge–maxAge.
func WeepingVinesColumn(r TreeRNG, x, y, z, total, minAge, maxAge int, d FungusDriver) {
	for k := 0; k <= total; k++ {
		if d.Read(x, y, z) == Air {
			if k == total || d.Read(x, y-1, z) != Air {
				d.Set(x, y, z, weepingVinesLo+uint32(minAge+r.Intn(maxAge-minAge+1)))
				break
			}
			d.Set(x, y, z, weepingPlant)
		}
		y--
	}
}

// TwistingVinesColumn is TwistingVinesFeature.placeWeepingVinesColumn (its
// name notwithstanding): up from (x,y,z), body until the last cell or a
// block above, which is a head aged minAge–maxAge.
func TwistingVinesColumn(r TreeRNG, x, y, z, total, minAge, maxAge int, d FungusDriver) {
	for k := 1; k <= total; k++ {
		if d.Read(x, y, z) == Air {
			if k == total || d.Read(x, y+1, z) != Air {
				d.Set(x, y, z, twistingVinesLo+uint32(minAge+r.Intn(maxAge-minAge+1)))
				break
			}
			d.Set(x, y, z, twistingPlant)
		}
		y++
	}
}
