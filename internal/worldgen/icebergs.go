package worldgen

import "math"

// Icebergs — IcebergFeature. In a frozen or deep frozen ocean, one chunk in
// sixteen raises a packed-ice berg at sea level and one in two hundred a
// blue-ice one: round or elliptical, three to seventeen tall over the
// water (rarely twenty-six more), up to eighteen below, snow-topped three
// times in ten, smoothed, and seven in ten (nine in ten of the ellipses)
// carved out on one side. A neighbouring chunk's bergs are replayed so a
// berg on the border is whole.

// owRegion is an overworld chunk buffer with pure-terrain reads beyond it;
// a column outside the chunk is carved cell by cell on first touch and
// kept. Columns within a chunk of the buffer sit in a grid (the features
// touch thousands, and a map hash per read showed in the profile); the
// rare column further out goes in a map.
type owRegion struct {
	g            *Generator
	ch           *Chunk
	baseX, baseZ int
	cols         map[[2]int]column
	carved       map[[2]int][]uint32
	gridCols     [regionSpan * regionSpan]*column
	gridCarved   [regionSpan * regionSpan][]uint32
	biomeChecks  map[string]int8 // chunkHasCaveBiome answers, per biome
}

const regionSpan = 48 // 16 + 16 either side

func (r *owRegion) gridIndex(x, z int) int {
	gx, gz := x-r.baseX+16, z-r.baseZ+16
	if gx < 0 || gx >= regionSpan || gz < 0 || gz >= regionSpan {
		return -1
	}
	return gx*regionSpan + gz
}

func (r *owRegion) read(x, y, z int) uint32 {
	if y < MinY || y >= MinY+len(r.ch.Sections)*16 {
		return Air
	}
	lx, lz := x-r.baseX, z-r.baseZ
	if lx >= 0 && lx < 16 && lz >= 0 && lz < 16 {
		return sectionBlockAt(r.ch, lx, y, lz)
	}
	var col []uint32
	gi := r.gridIndex(x, z)
	if gi >= 0 {
		col = r.gridCarved[gi]
	} else {
		col = r.carved[[2]int{x, z}]
	}
	if col == nil { // a column of unknowns, carved cell by cell as the features look
		n := len(r.ch.Sections) * 16
		col = make([]uint32, n)
		for i := range col {
			col[i] = unknownCell
		}
		if gi >= 0 {
			r.gridCarved[gi] = col
		} else {
			if r.carved == nil {
				r.carved = map[[2]int][]uint32{}
			}
			r.carved[[2]int{x, z}] = col
		}
	}
	if col[y-MinY] == unknownCell {
		c := r.col(x, z)
		col[y-MinY] = r.g.carve(c.block(y), x, y, z, c.h)
	}
	return col[y-MinY]
}

const unknownCell = ^uint32(0)

// col is the column model at (x, z), cached: the cave features ask for the
// height and biome of thousands of columns per chunk.
func (r *owRegion) col(x, z int) column {
	gi := r.gridIndex(x, z)
	if gi >= 0 {
		if c := r.gridCols[gi]; c != nil {
			return *c
		}
		c := r.g.columnAt(x, z)
		r.gridCols[gi] = &c
		return c
	}
	k := [2]int{x, z}
	if c, ok := r.cols[k]; ok {
		return c
	}
	c := r.g.columnAt(x, z)
	if r.cols == nil {
		r.cols = map[[2]int]column{}
	}
	r.cols[k] = c
	return c
}

// caveBiomeAt is the biome a cave feature sees at a cell (Generator
// .caveBiomeAt) through the column cache.
func (r *owRegion) caveBiomeAt(x, y, z int) string {
	c := r.col(x, z)
	if y < c.h-24 && y < SeaLevel {
		return r.g.caveBiome(x, z, y)
	}
	return c.biome.Name
}

// chunkHasCaveBiome samples a chunk's caves at its four quarter columns and
// three depths for the biome; a chunk with none of it skips that biome's
// features outright (the cave biomes vary over hundreds of blocks, so a
// chunk that straddles a border loses at most a sliver).
func (r *owRegion) chunkHasCaveBiome(ox, oz int, biome string) bool {
	key := biome + ":" + itoaSmall(ox) + ":" + itoaSmall(oz)
	if v, ok := r.biomeChecks[key]; ok {
		return v == 1
	}
	found := false
	for _, dx := range [2]int{4, 12} {
		for _, dz := range [2]int{4, 12} {
			c := r.col(ox+dx, oz+dz)
			for _, y := range [3]int{-50, -20, 10} {
				if y < c.h-24 && y < SeaLevel && r.g.caveBiome(ox+dx, oz+dz, y) == biome {
					found = true
				}
			}
		}
	}
	if r.biomeChecks == nil {
		r.biomeChecks = map[string]int8{}
	}
	if found {
		r.biomeChecks[key] = 1
	} else {
		r.biomeChecks[key] = 0
	}
	return found
}

func itoaSmall(n int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	var b [12]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			break
		}
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func (r *owRegion) set(x, y, z int, s uint32) {
	lx, lz := x-r.baseX, z-r.baseZ
	if lx < 0 || lx >= 16 || lz < 0 || lz >= 16 || y < MinY || y >= MinY+len(r.ch.Sections)*16 {
		return
	}
	setSectionBlock(r.ch, lx, y, lz, s, true)
}

func isFrozenOcean(name string) bool {
	return name == "minecraft:frozen_ocean" || name == "minecraft:deep_frozen_ocean"
}

// stampIcebergs replays the 3×3 chunks' iceberg draws into this chunk.
func (g *Generator) stampIcebergs(ch *Chunk, cx, cz int32) {
	reg := &owRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{}}
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			ncx, ncz := cx+dcx, cz+dcz
			ox, oz := int(ncx)*16, int(ncz)*16
			if !isFrozenOcean(g.resolveBiome(ox+8, oz+8).Name) {
				continue
			}
			r := newTreeRNG(g.seed^0x1CEB, ox, oz)
			if r.Intn(16) == 0 { // ICEBERG_PACKED
				x, z := ox+r.Intn(16), oz+r.Intn(16)
				if isFrozenOcean(g.resolveBiome(x, z).Name) {
					g.iceberg(r, x, z, PackedIce, reg)
				}
			}
			if r.Intn(200) == 0 { // ICEBERG_BLUE
				x, z := ox+r.Intn(16), oz+r.Intn(16)
				if isFrozenOcean(g.resolveBiome(x, z).Name) {
					g.iceberg(r, x, z, BlueIce, reg)
				}
			}
		}
	}
}

func isIcebergState(s uint32) bool { return s == PackedIce || s == SnowBlock || s == BlueIce }

func ceilF(f float64) int { return int(math.Ceil(f)) }

// iceberg is IcebergFeature.place at sea level.
func (g *Generator) iceberg(r TreeRNG, ox, oz int, main uint32, reg *owRegion) {
	oy := SeaLevel
	snowOnTop := r.Float64() > 0.7
	angle := r.Float64() * twoPi
	ellA := 11 - r.Intn(5)
	ellC := 3 + r.Intn(3)
	isEllipse := r.Float64() > 0.7
	over := 3 + r.Intn(15)
	if isEllipse {
		over = 6 + r.Intn(6)
	}
	if !isEllipse && r.Float64() > 0.9 {
		over += 7 + r.Intn(19)
	}
	under := over + r.Intn(11)
	if under > 18 {
		under = 18
	}
	width := over + r.Intn(7) - r.Intn(5)
	if width > 11 {
		width = 11
	}
	a := 11
	if isEllipse {
		a = ellA
	}
	radiusRound := func(yOff, height int) int {
		k := 3.5 - r.Float64()
		scale := (1 - math.Pow(float64(yOff), 2)/(float64(height)*k)) * float64(width)
		if height > 15+r.Intn(5) {
			t := yOff
			if yOff < 3+r.Intn(6) {
				t = yOff / 2
			}
			scale = (1 - float64(t)/(float64(height)*k*0.4)) * float64(width)
		}
		return ceilF(scale / 2)
	}
	radiusEllipse := func(yOff, height int) int {
		scale := (1 - math.Pow(float64(yOff), 2)/float64(height)) * float64(width)
		return ceilF(scale / 2)
	}
	radiusSteep := func(yOff, height int) int {
		k := 1 + r.Float64()/2
		scale := (1 - float64(yOff)/(float64(height)*k)) * float64(width)
		return ceilF(scale / 2)
	}
	sdEllipse := func(xo, zo, cxo, czo, ea, ec int, ang float64) float64 {
		dx, dz := float64(xo-cxo), float64(zo-czo)
		return math.Pow((dx*math.Cos(ang)-dz*math.Sin(ang))/float64(ea), 2) + math.Pow((dx*math.Sin(ang)+dz*math.Cos(ang))/float64(ec), 2) - 1
	}
	sdCircle := func(xo, zo, radius int) float64 {
		off := 10 * clampF(r.Float64(), 0.2, 0.8) / float64(radius)
		return off + float64(xo*xo) + float64(zo*zo) - float64(radius*radius)
	}
	ellipseC := func(yOff, height int) int {
		if yOff > 0 && height-yOff <= 3 {
			return ellC - (4 - (height - yOff))
		}
		return ellC
	}
	setBerg := func(x, y, z, hDiff, height int) {
		s := reg.read(x, y, z)
		if !(s == Air || s == SnowBlock || s == Ice || s == Water) {
			return
		}
		randomness := !isEllipse || r.Float64() > 0.05
		div := 2
		if isEllipse {
			div = 3
		}
		m := height / div
		if m < 1 {
			m = 1
		}
		if snowOnTop && s != Water && float64(hDiff) <= float64(r.Intn(m))+float64(height)*0.6 && randomness {
			reg.set(x, y, z, SnowBlock)
		} else {
			reg.set(x, y, z, main)
		}
	}
	gen := func(height, xo, yOff, zo, radius, ea int) {
		var sd float64
		if isEllipse {
			sd = sdEllipse(xo, zo, 0, 0, ea, ellipseC(yOff, height), angle)
		} else {
			sd = sdCircle(xo, zo, radius)
		}
		if sd >= 0 {
			return
		}
		compare := -0.5
		if !isEllipse {
			compare = float64(-6 - r.Intn(3))
		}
		if sd > compare && r.Float64() > 0.9 {
			return
		}
		setBerg(ox+xo, oy+yOff, oz+zo, height-yOff, height)
	}
	for xo := -a; xo < a; xo++ {
		for zo := -a; zo < a; zo++ {
			for yOff := 0; yOff < over; yOff++ {
				radius := 0
				if isEllipse {
					radius = radiusEllipse(yOff, over)
				} else {
					radius = radiusRound(yOff, over)
				}
				if isEllipse || xo < radius {
					gen(over, xo, yOff, zo, radius, a)
				}
			}
		}
	}
	g.icebergSmooth(reg, ox, oy, oz, width, over, isEllipse, ellA)
	for xo := -a; xo < a; xo++ {
		for zo := -a; zo < a; zo++ {
			for yOff := -1; yOff > -under; yOff-- {
				na := a
				if isEllipse {
					na = ceilF(float64(a) * (1 - math.Pow(float64(yOff), 2)/(float64(under)*8)))
				}
				radius := radiusSteep(-yOff, under)
				if xo < radius {
					gen(under, xo, yOff, zo, radius, na)
				}
			}
		}
	}
	cut := r.Float64() > 0.7
	if isEllipse {
		cut = r.Float64() > 0.1
	}
	if cut {
		g.icebergCutOut(r, reg, width, over, ox, oy, oz, isEllipse, ellA, angle, ellC, radiusRound, radiusSteep, sdEllipse)
	}
}

// icebergSmooth is IcebergFeature.smooth: berg blocks over air go (and the
// one above them), and a berg block with three open sides goes.
func (g *Generator) icebergSmooth(reg *owRegion, ox, oy, oz, width, height int, isEllipse bool, ellA int) {
	a := width / 2
	if isEllipse {
		a = ellA
	}
	for x := -a; x <= a; x++ {
		for z := -a; z <= a; z++ {
			for yOff := 0; yOff <= height; yOff++ {
				px, py, pz := ox+x, oy+yOff, oz+z
				s := reg.read(px, py, pz)
				if !isIcebergState(s) && s != Snow {
					continue
				}
				if reg.read(px, py-1, pz) == Air {
					reg.set(px, py, pz, Air)
					reg.set(px, py+1, pz, Air)
				} else if isIcebergState(s) {
					open := 0
					for _, o := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
						if !isIcebergState(reg.read(px+o[0], py, pz+o[1])) {
							open++
						}
					}
					if open >= 3 {
						reg.set(px, py, pz, Air)
					}
				}
			}
		}
	}
}

// icebergCutOut is IcebergFeature.generateCutOut: an elliptical bite off
// one side, air above the water and water below.
func (g *Generator) icebergCutOut(r TreeRNG, reg *owRegion, width, height, ox, oy, oz int, isEllipse bool, ellA int, angle float64, ellC int,
	radiusRound, radiusSteep func(int, int) int, sdEllipse func(int, int, int, int, int, int, float64) float64) {
	sx, sz := 1, 1
	if r.Intn(2) == 0 {
		sx = -1
	}
	if r.Intn(2) == 0 {
		sz = -1
	}
	maxI := func(v int) int {
		if v < 1 {
			return 1
		}
		return v
	}
	xOff := r.Intn(maxI(width/2 - 2))
	if r.Intn(2) == 0 {
		xOff = width/2 + 1 - r.Intn(maxI(width-width/2-1))
	}
	zOff := r.Intn(maxI(width/2 - 2))
	if r.Intn(2) == 0 {
		zOff = width/2 + 1 - r.Intn(maxI(width-width/2-1))
	}
	if isEllipse {
		xOff = r.Intn(maxI(ellA - 5))
		zOff = xOff
	}
	lx, lz := sx*xOff, sz*zOff
	ang := r.Float64() * twoPi
	if isEllipse {
		ang = angle + math.Pi/2
	}
	carve := func(radius, yOff int, underWater bool) {
		ca := radius + 1 + ellA/3
		cc := radius - 3
		if cc > 3 {
			cc = 3
		}
		cc += ellC/2 - 1
		if cc < 1 {
			cc = 1
		}
		for xo := -ca; xo < ca; xo++ {
			for zo := -ca; zo < ca; zo++ {
				if sdEllipse(xo, zo, lx, lz, ca, cc, ang) >= 0 {
					continue
				}
				px, py, pz := ox+xo, oy+yOff, oz+zo
				s := reg.read(px, py, pz)
				if isIcebergState(s) || s == SnowBlock {
					if underWater {
						reg.set(px, py, pz, Water)
					} else {
						reg.set(px, py, pz, Air)
						if reg.read(px, py+1, pz) == Snow {
							reg.set(px, py+1, pz, Air)
						}
					}
				}
			}
		}
	}
	for yOff := 0; yOff < height-3; yOff++ {
		carve(radiusRound(yOff, height), yOff, false)
	}
	for yOff := -1; yOff > -height+r.Intn(5); yOff-- {
		carve(radiusSteep(-yOff, height), yOff, true)
	}
}
