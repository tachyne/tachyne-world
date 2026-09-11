package worldgen

import "math"

// Coral reefs (CoralFeature and its tree, claw and mushroom shapes): warm
// oceans grow a noise-scaled count of reefs per chunk, each a random one
// of the three shapes in one of the five coral colours, dressed as vanilla
// dresses them — a coral plant or fan on top one time in four, a sea
// pickle one in twenty, wall fans on the sides one in five.

const (
	coralNoiseScale = 400.0 // warm_ocean_vegetation noise_factor
	coralNoiseRatio = 20    // …and noise_to_count_ratio
)

// Coral state layout: five colours in tube/brain/bubble/fire/horn order.
var (
	CoralBlock0   = blockBase("tube_coral_block")    // +colour
	CoralPlant0   = blockBase("tube_coral")          // +2×colour (waterlogged=true first)
	CoralFan0     = blockBase("tube_coral_fan")      // +2×colour
	CoralWallFan0 = blockBase("tube_coral_wall_fan") // +8×colour + 2×facing (n,s,w,e)
)

// coralDir is a horizontal direction in vanilla's Plane.HORIZONTAL order.
type coralDir int

const (
	coralNorth coralDir = iota
	coralSouth
	coralWest
	coralEast
)

func (d coralDir) delta() (int, int) {
	switch d {
	case coralNorth:
		return 0, -1
	case coralSouth:
		return 0, 1
	case coralWest:
		return -1, 0
	}
	return 1, 0
}

// clockwise / counterClockwise follow Direction.getClockWise about the Y axis.
func (d coralDir) clockwise() coralDir {
	switch d {
	case coralNorth:
		return coralEast
	case coralEast:
		return coralSouth
	case coralSouth:
		return coralWest
	}
	return coralNorth
}

func (d coralDir) counterClockwise() coralDir { return d.clockwise().clockwise().clockwise() }

// shuffledDirs is Plane.HORIZONTAL.shuffledCopy: Fisher–Yates over the four.
func shuffledDirs(r TreeRNG, dirs []coralDir) []coralDir {
	out := append([]coralDir(nil), dirs...)
	for i := len(out) - 1; i > 0; i-- {
		j := r.Intn(i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// coralReef is one feature run: the chunk's block access plus an overlay of
// every cell this reef has written, so a branch that crosses the chunk
// border sees the same reef from either side.
type coralReef struct {
	r       TreeRNG
	block   uint32 // the colour's coral block
	at      func(x, y, z int) uint32
	put     func(x, y, z int, s uint32)
	overlay map[[3]int]uint32
}

func (c *coralReef) get(x, y, z int) uint32 {
	if s, ok := c.overlay[[3]int{x, y, z}]; ok {
		return s
	}
	return c.at(x, y, z)
}

func (c *coralReef) set(x, y, z int, s uint32) {
	c.overlay[[3]int{x, y, z}] = s
	c.put(x, y, z, s)
}

// isCoral is #corals: the plants and fans (not the blocks).
func isCoral(s uint32) bool {
	return s >= CoralPlant0 && s < CoralFan0+10
}

// placeBlock is CoralFeature.placeCoralBlock.
func (c *coralReef) placeBlock(x, y, z int) bool {
	here := c.get(x, y, z)
	if (here != Water && !isCoral(here)) || c.get(x, y+1, z) != Water {
		return false
	}
	c.set(x, y, z, c.block)
	if c.r.Float64() < 0.25 {
		n := c.r.Intn(10) // #corals: five plants, five fans
		if n < 5 {
			c.set(x, y+1, z, CoralPlant0+uint32(n)*2)
		} else {
			c.set(x, y+1, z, CoralFan0+uint32(n-5)*2)
		}
	} else if c.r.Float64() < 0.05 {
		c.set(x, y+1, z, SeaPickle+uint32(c.r.Intn(4))*2)
	}
	for d := coralNorth; d <= coralEast; d++ {
		if c.r.Float64() >= 0.2 {
			continue
		}
		dx, dz := d.delta()
		if c.get(x+dx, y, z+dz) != Water {
			continue
		}
		c.set(x+dx, y, z+dz, CoralWallFan0+uint32(c.r.Intn(5))*8+uint32(d)*2)
	}
	return true
}

// tree is CoralTreeFeature: a trunk of 1–3, then 2–4 branches leaning out
// as they climb.
func (c *coralReef) tree(x, y, z int) {
	n := c.r.Intn(3) + 1
	for i := 0; i < n; i++ {
		if !c.placeBlock(x, y, z) {
			return
		}
		y++
	}
	bx, by, bz := x, y, z
	n2 := c.r.Intn(3) + 2
	dirs := shuffledDirs(c.r, []coralDir{coralNorth, coralSouth, coralWest, coralEast})[:n2]
	for _, d := range dirs {
		dx, dz := d.delta()
		px, py, pz := bx+dx, by, bz+dz
		n3 := c.r.Intn(5) + 2
		n4 := 0
		for i := 0; i < n3 && c.placeBlock(px, py, pz); i++ {
			py++
			if i != 0 {
				n4++
				if n4 < 2 || c.r.Float64() >= 0.25 {
					continue
				}
			}
			px, pz = px+dx, pz+dz
			n4 = 0
		}
	}
}

// claw is CoralClawFeature: a base, then 2–3 arms curling up and forward.
func (c *coralReef) claw(x, y, z int) {
	if !c.placeBlock(x, y, z) {
		return
	}
	dir := coralDir(c.r.Intn(4))
	n := c.r.Intn(2) + 2
	arms := shuffledDirs(c.r, []coralDir{dir, dir.clockwise(), dir.counterClockwise()})[:n]
	for _, d2 := range arms {
		px, py, pz := x, y, z
		n4 := c.r.Intn(2) + 1
		dx, dz := d2.delta()
		px, pz = px+dx, pz+dz
		var d3 coralDir
		up3 := false // the second leg may climb instead of running along d2
		var n3 int
		if d2 == dir {
			d3 = dir
			n3 = c.r.Intn(3) + 2
		} else {
			py++
			if c.r.Intn(2) == 0 {
				d3 = d2
			} else {
				up3 = true
			}
			n3 = c.r.Intn(3) + 3
		}
		step := func(px, py, pz int, back bool) (int, int, int) {
			if up3 {
				if back {
					return px, py - 1, pz
				}
				return px, py + 1, pz
			}
			ddx, ddz := d3.delta()
			if back {
				return px - ddx, py, pz - ddz
			}
			return px + ddx, py, pz + ddz
		}
		for i := 0; i < n4 && c.placeBlock(px, py, pz); i++ {
			px, py, pz = step(px, py, pz, false)
		}
		px, py, pz = step(px, py, pz, true)
		py++
		mdx, mdz := dir.delta()
		for i := 0; i < n3; i++ {
			px, pz = px+mdx, pz+mdz
			if !c.placeBlock(px, py, pz) {
				break
			}
			if c.r.Float64() < 0.25 {
				py++
			}
		}
	}
}

// mushroom is CoralMushroomFeature: a hollow box 3–5 on a side, sunk 1–3
// into the floor, with its edges and corners knocked off and a tenth of the
// shell missing.
func (c *coralReef) mushroom(x, y, z int) {
	n := c.r.Intn(3) + 3
	n2 := c.r.Intn(3) + 3
	n3 := c.r.Intn(3) + 3
	n4 := c.r.Intn(3) + 1
	for i := 0; i <= n2; i++ {
		for j := 0; j <= n; j++ {
			for k := 0; k <= n3; k++ {
				px, py, pz := x+i, y+j-n4, z+k
				edgeIJ := (i == 0 || i == n2) && (j == 0 || j == n)
				edgeKJ := (k == 0 || k == n3) && (j == 0 || j == n)
				edgeIK := (i == 0 || i == n2) && (k == 0 || k == n3)
				shell := i == 0 || i == n2 || j == 0 || j == n || k == 0 || k == n3
				if edgeIJ || edgeKJ || edgeIK || !shell {
					continue
				}
				if c.r.Float64() < 0.1 {
					continue
				}
				c.placeBlock(px, py, pz)
			}
		}
	}
}

// decorateCoral runs the chunk's warm_ocean_vegetation attempts: a
// noise-scaled count, each a random shape in a random colour at the floor
// of a random column.
func (g *Generator) decorateCoral(r TreeRNG, ox, oz, baseX, baseZ int, at func(x, y, z int) uint32, put func(x, y, z int, s uint32)) {
	n := int(math.Ceil(g.humid.Noise2(float64(ox)/coralNoiseScale, float64(oz)/coralNoiseScale) * coralNoiseRatio))
	for i := 0; i < n; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		shape := r.Intn(3)
		colour := r.Intn(5)
		if pr, ok := seafloorRules[g.resolveBiome(x, z).Name]; !ok || !pr.coral {
			continue
		}
		y, ok := g.seafloorCol(x, z)
		if !ok {
			continue
		}
		reef := &coralReef{r: r, block: CoralBlock0 + uint32(colour), overlay: map[[3]int]uint32{},
			at: func(x, y, z int) uint32 { return g.virtualAt(at, baseX, baseZ, x, y, z) }, put: put}
		switch shape {
		case 0:
			reef.tree(x, y, z)
		case 1:
			reef.claw(x, y, z)
		default:
			reef.mushroom(x, y, z)
		}
	}
}

// virtualAt reads a cell through the chunk when it is inside it, and from
// the column model outside it: solid under the floor, water up to sea
// level, air above. Both chunks a reef straddles see the same thing.
func (g *Generator) virtualAt(at func(x, y, z int) uint32, baseX, baseZ, x, y, z int) uint32 {
	if x >= baseX && x < baseX+16 && z >= baseZ && z < baseZ+16 {
		return at(x, y, z)
	}
	col := g.columnAt(x, z)
	switch {
	case y < col.h:
		return Stone
	case y <= SeaLevel-1:
		return Water
	}
	return Air
}
