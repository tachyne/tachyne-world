package worldgen

import (
	"encoding/json"
)

// More of the configured features the vanilla generator places
// (vanillafeatures.go has the selectors, ores, single blocks and trees):
// the top layer's snow and ice, disks, springs, block columns (sugar cane,
// cactus, kelp, cave vines), vines, multiface growths (glow lichen, sculk
// veins), underwater magma and lakes — each drawing from its stream as
// the 26.3 feature does.

// vpExtra is a parsed config for the features below.
type vpExtra struct {
	provider, fluid, barrier vpStateProvider
	target, allowed          vpPredicate
	canPlace, canReplace     vpPredicate
	canBarrier               vpPredicate
	radius                   vpIntProvider
	halfHeight               int
	state                    uint32
	ranges                   [][2]uint32
	requiresBelow            bool
	rocks, holes             int
	layers                   []vpColumnLayer
	dir                      [3]int
	tip                      bool
	block                    string
	searchRange              int
	floor, ceiling, wall     bool
	spread                   float32
	magma                    [3]float32 // floor search, radius, probability
}

type vpColumnLayer struct {
	height vpIntProvider
	pr     vpStateProvider
}

func (f *vpFeature) extra() *vpExtra {
	f.once.Do(f.parse)
	return f.ext
}

// parseExtra reads the configs of this file's features.
func (f *vpFeature) parseExtra() {
	x := &vpExtra{}
	pred := func(k string) vpPredicate {
		if r, ok := f.raw[k]; ok {
			if p, err := parseVPPredicate(r); err == nil {
				return p
			}
		}
		return vpTrue{}
	}
	num := func(k string, def int) int {
		v := def
		if r, ok := f.raw[k]; ok {
			_ = json.Unmarshal(r, &v)
		}
		return v
	}
	flag := func(k string, def bool) bool {
		v := def
		if r, ok := f.raw[k]; ok {
			_ = json.Unmarshal(r, &v)
		}
		return v
	}
	blocks := func(k string) [][2]uint32 {
		var one string
		if json.Unmarshal(f.raw[k], &one) == nil {
			if len(one) > 0 && one[0] == '#' {
				return vpTagRangesOf(vpShort(one[1:]))
			}
			return vpBlockRanges([]string{one})
		}
		var many []string
		_ = json.Unmarshal(f.raw[k], &many)
		return vpBlockRanges(many)
	}
	switch f.typ {
	case "disk":
		x.provider = parseVPStateProvider(f.raw["state_provider"])
		x.target = pred("target")
		x.radius, _ = parseVPInt(f.raw["radius"])
		x.halfHeight = num("half_height", 0)
	case "spring_feature":
		x.state = vpParseState(f.raw["state"])
		x.requiresBelow = flag("requires_block_below", true)
		x.rocks = num("rock_count", 4)
		x.holes = num("hole_count", 1)
		x.ranges = blocks("valid_blocks")
	case "block_column":
		var ls []struct {
			Height   json.RawMessage `json:"height"`
			Provider json.RawMessage `json:"provider"`
		}
		_ = json.Unmarshal(f.raw["layers"], &ls)
		for _, l := range ls {
			h, err := parseVPInt(l.Height)
			if err != nil {
				h = vpConstInt(0)
			}
			x.layers = append(x.layers, vpColumnLayer{h, parseVPStateProvider(l.Provider)})
		}
		var d string
		_ = json.Unmarshal(f.raw["direction"], &d)
		x.dir = vpDirections[d]
		x.allowed = pred("allowed_placement")
		x.tip = flag("prioritize_tip", false)
	case "multiface_growth":
		_ = json.Unmarshal(f.raw["block"], &x.block)
		x.block = vpShort(x.block)
		x.searchRange = num("search_range", 10)
		x.floor = flag("can_place_on_floor", false)
		x.ceiling = flag("can_place_on_ceiling", false)
		x.wall = flag("can_place_on_wall", false)
		x.spread = 0.5
		if r, ok := f.raw["chance_of_spreading"]; ok {
			_ = json.Unmarshal(r, &x.spread)
		}
		x.ranges = blocks("can_be_placed_on")
	case "underwater_magma":
		var a, b int
		var p float32
		_ = json.Unmarshal(f.raw["floor_search_range"], &a)
		_ = json.Unmarshal(f.raw["placement_radius_around_floor"], &b)
		_ = json.Unmarshal(f.raw["placement_probability_per_valid_position"], &p)
		x.magma = [3]float32{float32(a), float32(b), p}
	case "lake":
		x.fluid = parseVPStateProvider(f.raw["fluid"])
		x.barrier = parseVPStateProvider(f.raw["barrier"])
		x.canPlace = pred("can_place_feature")
		x.canReplace = pred("can_replace_with_air_or_fluid")
		x.canBarrier = pred("can_replace_with_barrier")
	}
	f.ext = x
}

// placeExtra places this file's features; handled false for any other
// type.
func (e *vpExec) placeExtra(f *vpFeature, r *vwRandom, p vpPos) (placed, handled bool) {
	switch f.typ {
	case "freeze_top_layer":
		return e.placeFreeze(p), true
	case "disk":
		return e.placeDisk(f.extra(), r, p), true
	case "spring_feature":
		return e.placeSpring(f.extra(), p), true
	case "block_column":
		return e.placeColumn(f.extra(), r, p), true
	case "vines":
		return e.placeVines(p), true
	case "multiface_growth":
		return e.placeMultiface(f.extra(), r, p), true
	case "underwater_magma":
		return e.placeMagma(f.extra(), r, p), true
	case "lake":
		return e.placeLake(f.extra(), r, p), true
	}
	return false, false
}

// placeFreeze is SnowAndFreezeFeature over the source chunk's 16x16
// columns. It runs last and only on its own chunk, so it reads the chunk
// as decorated (snow settles on the canopy, as vanilla's MOTION_BLOCKING
// has it).
func (e *vpExec) placeFreeze(p vpPos) bool {
	if p.x>>4 != e.baseX>>4 || p.z>>4 != e.baseZ>>4 || e.cold == nil {
		return false
	}
	top := MinY + len(e.ch.Sections)*16 - 1
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			x, z := e.baseX+lx, e.baseZ+lz
			y := top
			for y > MinY && !vpMotionBlocking(sectionBlockAt(e.ch, lx, y, lz)) {
				y--
			}
			y++ // the first free cell above
			if y > top {
				continue
			}
			biome := e.ctx.lv.Biome(x, y, z)
			below := sectionBlockAt(e.ch, lx, y-1, lz)
			if e.cold(biome, x, y-1, z) && below == Water && y-1 >= MinY {
				e.set(x, y-1, z, Ice)
				below = Ice
			}
			if e.cold(biome, x, y, z) {
				cur := sectionBlockAt(e.ch, lx, y, lz)
				if (cur == Air || cur == Snow) && snowSurvivesOn(below) {
					e.set(x, y, z, Snow)
					if info, ok := InfoForState(below); ok && info.HasProperty("snowy") {
						e.set(x, y-1, z, SetProperty(info, below, "snowy", "true"))
					}
				}
			}
		}
	}
	return true
}

// vpMotionBlocking is the MOTION_BLOCKING heightmap's test: a block that
// blocks motion, or a fluid.
func vpMotionBlocking(s uint32) bool {
	return s != Air && (IsSolid(s) || IsFluid(s) || HoldsWater(s) || IsLeaves(s))
}

// placeDisk is DiskFeature.
func (e *vpExec) placeDisk(x *vpExtra, r *vwRandom, p vpPos) bool {
	if x.radius == nil {
		return false
	}
	top, bottom := p.y+x.halfHeight, p.y-x.halfHeight-1
	rad := x.radius.sample(r)
	any := false
	// BlockPos.betweenClosed: x fastest, then z (y is fixed).
	for dz := -rad; dz <= rad; dz++ {
		for dx := -rad; dx <= rad; dx++ {
			if dx*dx+dz*dz > rad*rad {
				continue
			}
			for y := top; y > bottom; y-- {
				q := vpPos{p.x + dx, y, p.z + dz}
				if !x.target.test(e.ctx, q) {
					continue
				}
				if s, ok := x.provider(e.ctx, r, q); ok {
					e.set(q.x, q.y, q.z, s)
					any = true
				}
			}
		}
	}
	return any
}

// placeSpring is SpringFeature.
func (e *vpExec) placeSpring(x *vpExtra, p vpPos) bool {
	valid := func(q vpPos) bool { return vpInRanges(x.ranges, e.ctx.blockAt(q)) }
	empty := func(q vpPos) bool { return e.ctx.blockAt(q) == Air }
	if !valid(vpPos{p.x, p.y + 1, p.z}) {
		return false
	}
	if x.requiresBelow && !valid(vpPos{p.x, p.y - 1, p.z}) {
		return false
	}
	if cur := e.ctx.blockAt(p); cur != Air && !vpInRanges(x.ranges, cur) {
		return false
	}
	rocks, holes := 0, 0
	for _, d := range [5][3]int{{-1, 0, 0}, {1, 0, 0}, {0, 0, -1}, {0, 0, 1}, {0, -1, 0}} {
		q := vpPos{p.x + d[0], p.y + d[1], p.z + d[2]}
		if valid(q) {
			rocks++
		}
		if empty(q) {
			holes++
		}
	}
	if rocks != x.rocks || holes != x.holes {
		return false
	}
	s := x.state
	if IsWater(s) {
		s = Water
	} else if IsLava(s) {
		s = LavaBase
	}
	e.set(p.x, p.y, p.z, s)
	return true
}

// placeColumn is BlockColumnFeature.
func (e *vpExec) placeColumn(x *vpExtra, r *vwRandom, p vpPos) bool {
	n := len(x.layers)
	heights := make([]int, n)
	total := 0
	for i, l := range x.layers {
		heights[i] = l.height.sample(r)
		total += heights[i]
	}
	if total == 0 {
		return false
	}
	next := vpPos{p.x + x.dir[0], p.y + x.dir[1], p.z + x.dir[2]}
	for y := 0; y < total; y++ {
		if !x.allowed.test(e.ctx, next) {
			remove := total - y
			if x.tip {
				for i := 0; i < n && remove > 0; i++ {
					t := min(heights[i], remove)
					remove -= t
					heights[i] -= t
				}
			} else {
				for i := n - 1; i >= 0 && remove > 0; i-- {
					t := min(heights[i], remove)
					remove -= t
					heights[i] -= t
				}
			}
			break
		}
		next = vpPos{next.x + x.dir[0], next.y + x.dir[1], next.z + x.dir[2]}
	}
	at := p
	for i, l := range x.layers {
		for k := 0; k < heights[i]; k++ {
			e.set(at.x, at.y, at.z, vpGetState(l.pr, e.ctx, r, at))
			at = vpPos{at.x + x.dir[0], at.y + x.dir[1], at.z + x.dir[2]}
		}
	}
	return true
}

// vpFaces is Direction.values(): down, up, north, south, west, east.
var vpFaces = [6]struct {
	name string
	d    [3]int
	face int
}{
	{"down", [3]int{0, -1, 0}, FaceDown}, {"up", [3]int{0, 1, 0}, FaceUp},
	{"north", [3]int{0, 0, -1}, FaceNorth}, {"south", [3]int{0, 0, 1}, FaceSouth},
	{"west", [3]int{-1, 0, 0}, FaceWest}, {"east", [3]int{1, 0, 0}, FaceEast},
}

var vpOpposite = [6]int{1, 0, 3, 2, 5, 4}

// vpCanAttach is MultifaceBlock.canAttachTo: the neighbour's face toward
// us full.
func (e *vpExec) vpCanAttach(dir int, q vpPos) bool {
	return IsFaceSturdy(e.ctx.blockAt(q), vpFaces[vpOpposite[dir]].face)
}

// placeVines is VinesFeature: a vine on the first face (not down) that
// has something to hang on.
func (e *vpExec) placeVines(p vpPos) bool {
	if e.ctx.blockAt(p) != Air {
		return false
	}
	for i := 1; i < 6; i++ {
		f := vpFaces[i]
		if e.vpCanAttach(i, vpPos{p.x + f.d[0], p.y + f.d[1], p.z + f.d[2]}) {
			e.set(p.x, p.y, p.z, vpStateOf("vine", map[string]string{f.name: "true"}))
			return true
		}
	}
	return false
}

// vpShuffle is Util.shuffle.
func vpShuffle(list []int, r *vwRandom) {
	for i := len(list); i > 1; i-- {
		j := int(r.nextIntN(int32(i)))
		list[i-1], list[j] = list[j], list[i-1]
	}
}

// placeMultiface is MultifaceGrowthFeature (the spreading after a
// placement is not drawn).
func (e *vpExec) placeMultiface(x *vpExtra, r *vwRandom, p vpPos) bool {
	airOrWater := func(s uint32) bool { return s == Air || IsWater(s) && !IsBubbleColumn(s) }
	if !airOrWater(e.ctx.blockAt(p)) {
		return false
	}
	var valid []int
	if x.ceiling {
		valid = append(valid, 1)
	}
	if x.floor {
		valid = append(valid, 0)
	}
	if x.wall {
		valid = append(valid, 2, 5, 3, 4) // Direction.Plane.HORIZONTAL: north, east, south, west
	}
	dirs := append([]int(nil), valid...)
	vpShuffle(dirs, r)
	try := func(q vpPos, old uint32, order []int) bool {
		for _, d := range order {
			f := vpFaces[d]
			n := vpPos{q.x + f.d[0], q.y + f.d[1], q.z + f.d[2]}
			if !vpInRanges(x.ranges, e.ctx.blockAt(n)) {
				continue
			}
			if !e.vpCanAttach(d, n) {
				return false
			}
			props := map[string]string{f.name: "true"}
			if IsWater(old) {
				props["waterlogged"] = "true"
			}
			e.set(q.x, q.y, q.z, vpStateOf(x.block, props))
			r.nextFloat() // the spread's chance (the spread itself is not drawn)
			return true
		}
		return false
	}
	if try(p, e.ctx.blockAt(p), dirs) {
		return true
	}
	for _, sd := range dirs {
		var except []int
		for _, d := range valid {
			if d != vpOpposite[sd] {
				except = append(except, d)
			}
		}
		vpShuffle(except, r)
		f := vpFaces[sd]
		q := vpPos{p.x + f.d[0], p.y + f.d[1], p.z + f.d[2]}
		for i := 0; i < x.searchRange; i++ {
			s := e.ctx.blockAt(q)
			if !airOrWater(s) && !vpIsBlock(s, x.block) {
				break
			}
			if try(q, s, except) {
				return true
			}
		}
	}
	return false
}

// placeMagma is UnderwaterMagmaFeature.
func (e *vpExec) placeMagma(x *vpExtra, r *vwRandom, p vpPos) bool {
	// Column.scan: down through water to the floor below it.
	floor, ok := 0, false
	rng := int(x.magma[0])
	if IsWater(e.ctx.blockAt(p)) {
		for d := 1; d <= rng; d++ {
			if s := e.ctx.blockAt(vpPos{p.x, p.y - d, p.z}); !IsWater(s) {
				floor, ok = p.y-d, true
				break
			}
		}
	}
	if !ok {
		return false
	}
	rad := int(x.magma[1])
	any := false
	waterOrAir := func(s uint32) bool { return s == Air || IsWater(s) }
	open := func(q vpPos) bool { return !IsFullCube(e.ctx.blockAt(q)) }
	for zz := p.z - rad; zz <= p.z+rad; zz++ {
		for yy := floor - rad; yy <= floor+rad; yy++ {
			for xx := p.x - rad; xx <= p.x+rad; xx++ {
				if r.nextFloat() >= x.magma[2] {
					continue
				}
				q := vpPos{xx, yy, zz}
				if waterOrAir(e.ctx.blockAt(q)) || open(vpPos{xx, yy - 1, zz}) {
					continue
				}
				if open(vpPos{xx, yy, zz - 1}) || open(vpPos{xx + 1, yy, zz}) || open(vpPos{xx, yy, zz + 1}) || open(vpPos{xx - 1, yy, zz}) {
					continue
				}
				e.set(xx, yy, zz, vpStateOf("magma_block", nil))
				any = true
			}
		}
	}
	return any
}

// placeLake is LakeFeature.
func (e *vpExec) placeLake(x *vpExtra, r *vwRandom, p vpPos) bool {
	if p.y <= e.ctx.minY+4 {
		return false
	}
	o := vpPos{p.x - 8, p.y - 4, p.z - 8}
	var grid [2048]bool
	spots := int(r.nextIntN(4)) + 4
	for i := 0; i < spots; i++ {
		xr := r.nextDouble()*6 + 3
		yr := r.nextDouble()*4 + 2
		zr := r.nextDouble()*6 + 3
		xp := r.nextDouble()*(16-xr-2) + 1 + xr/2
		yp := r.nextDouble()*(8-yr-4) + 2 + yr/2
		zp := r.nextDouble()*(16-zr-2) + 1 + zr/2
		for xx := 1; xx < 15; xx++ {
			for zz := 1; zz < 15; zz++ {
				for yy := 1; yy < 7; yy++ {
					xd := (float64(xx) - xp) / (xr / 2)
					yd := (float64(yy) - yp) / (yr / 2)
					zd := (float64(zz) - zp) / (zr / 2)
					if xd*xd+yd*yd+zd*zd < 1 {
						grid[(xx*16+zz)*8+yy] = true
					}
				}
			}
		}
	}
	g := func(xx, zz, yy int) bool { return grid[(xx*16+zz)*8+yy] }
	edge := func(xx, zz, yy int) bool {
		return !g(xx, zz, yy) && (xx < 15 && g(xx+1, zz, yy) || xx > 0 && g(xx-1, zz, yy) ||
			zz < 15 && g(xx, zz+1, yy) || zz > 0 && g(xx, zz-1, yy) ||
			yy < 7 && g(xx, zz, yy+1) || yy > 0 && g(xx, zz, yy-1))
	}
	fluid := vpGetState(x.fluid, e.ctx, r, o)
	for xx := 0; xx < 16; xx++ {
		for zz := 0; zz < 16; zz++ {
			for yy := 0; yy < 8; yy++ {
				if !edge(xx, zz, yy) {
					continue
				}
				q := vpPos{o.x + xx, o.y + yy, o.z + zz}
				s := e.ctx.blockAt(q)
				if yy >= 4 && IsFluid(s) {
					return false
				}
				if yy < 4 && !IsSolid(s) && s != fluid {
					return false
				}
				if !x.canPlace.test(e.ctx, q) {
					return false
				}
			}
		}
	}
	caveAir := vpStateOf("cave_air", nil)
	for xx := 0; xx < 16; xx++ {
		for zz := 0; zz < 16; zz++ {
			for yy := 0; yy < 8; yy++ {
				if !g(xx, zz, yy) {
					continue
				}
				q := vpPos{o.x + xx, o.y + yy, o.z + zz}
				if x.canReplace.test(e.ctx, q) {
					if yy >= 4 {
						e.set(q.x, q.y, q.z, caveAir)
					} else {
						e.set(q.x, q.y, q.z, fluid)
					}
				}
			}
		}
	}
	barrier := vpGetState(x.barrier, e.ctx, r, o)
	if barrier != Air {
		for xx := 0; xx < 16; xx++ {
			for zz := 0; zz < 16; zz++ {
				for yy := 0; yy < 8; yy++ {
					if edge(xx, zz, yy) && (yy < 4 || r.nextIntN(2) != 0) {
						q := vpPos{o.x + xx, o.y + yy, o.z + zz}
						if IsSolid(e.ctx.blockAt(q)) && x.canBarrier.test(e.ctx, q) {
							e.set(q.x, q.y, q.z, barrier)
						}
					}
				}
			}
		}
	}
	return true
}
