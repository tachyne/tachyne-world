package worldgen

// The overworld's small terrain features that stand on the surface or hang
// under the ice, before any plant grows:
//
//   - ice_spike + ice_patch (SpikeFeature, DiskFeature) in the ice spikes:
//     three packed-ice spikes a chunk, seven to ten tall, one in sixty of
//     the wide ones raised ten to forty blocks on a pillar, and two packed
//     ice disks set into the snow.
//   - forest_rock (BlockBlobFeature) in the old-growth taigas: two mossy
//     cobblestone boulders a chunk, three overlapping blobs each.
//   - blue_ice (BlueIceFeature) in the frozen oceans: up to nineteen tries a
//     chunk between y=30 and 61, each taking only where it touches packed
//     ice — the underside and flanks of the icebergs — and spreading up to
//     two hundred blocks of blue ice from there.
//
// Every draw comes from the feature's own position (newTreeRNG, hash01), not
// from a stream that reads the world, and every read that steers a feature
// is of the pure terrain model, so the passes of every chunk a feature
// straddles place the same feature. Each is skipped whole where its box
// holds a player's block (buildguard.go): tachyne regenerates terrain, and
// these came after players had built.

const (
	iceSpikeSalt   = 0x1CE5
	icePatchSalt   = 0x1CE7
	forestRockSalt = 0xF0C4
	blueIceSalt    = 0xB1CE
)

// decorateSurface places the chunk's share of the forest rocks, ice spikes
// and ice patches whose origins lie in it or its eight neighbours (none
// reaches further than three blocks from its origin). It runs before the
// vegetation, as vanilla's LOCAL_MODIFICATIONS and SURFACE_STRUCTURES steps
// run before VEGETAL_DECORATION.
func (g *Generator) decorateSurface(ch *Chunk, cx, cz int32) {
	for ncx := cx - 1; ncx <= cx+1; ncx++ {
		for ncz := cz - 1; ncz <= cz+1; ncz++ {
			ox, oz := int(ncx)*16, int(ncz)*16
			switch g.resolveBiome(ox+8, oz+8).Name {
			case "minecraft:old_growth_pine_taiga", "minecraft:old_growth_spruce_taiga",
				"minecraft:ice_spikes":
			default:
				// A chunk whose centre is neither can still hold an origin
				// that is: look at the corners before giving up on it.
				if !g.chunkTouchesBiome(ox, oz, "minecraft:ice_spikes", "minecraft:old_growth_pine_taiga", "minecraft:old_growth_spruce_taiga") {
					continue
				}
			}
			g.forestRocksOf(ch, cx, cz, ox, oz)
			g.iceSpikesOf(ch, cx, cz, ox, oz)
		}
	}
}

// chunkTouchesBiome reports whether any of a chunk's corner columns lies in
// one of the biomes.
func (g *Generator) chunkTouchesBiome(ox, oz int, names ...string) bool {
	for _, d := range [4][2]int{{0, 0}, {15, 0}, {0, 15}, {15, 15}} {
		b := g.resolveBiome(ox+d[0], oz+d[1]).Name
		for _, n := range names {
			if b == n {
				return true
			}
		}
	}
	return false
}

// chunkCell reads and writes one chunk by world coordinates; anything
// outside it reads as unknown and is not written (a neighbour's pass lays
// its own part).
type chunkCell struct {
	ch           *Chunk
	baseX, baseZ int
}

func (c chunkCell) in(x, z int) bool {
	return x >= c.baseX && x < c.baseX+16 && z >= c.baseZ && z < c.baseZ+16
}
func (c chunkCell) get(x, y, z int) uint32 { return sectionBlockAt(c.ch, x-c.baseX, y, z-c.baseZ) }
func (c chunkCell) put(x, y, z int, s uint32) {
	setSectionBlock(c.ch, x-c.baseX, y, z-c.baseZ, s, true)
}

// forestRocksOf is placed_feature forest_rock for the origin chunk at
// (ox, oz): count 2, in_square, MOTION_BLOCKING — the ground, since it runs
// before the trees.
func (g *Generator) forestRocksOf(ch *Chunk, cx, cz int32, ox, oz int) {
	c := chunkCell{ch, int(cx) * 16, int(cz) * 16}
	r := newTreeRNG(g.seed^forestRockSalt, ox, oz)
	for i := 0; i < 2; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		switch g.resolveBiome(x, z).Name {
		case "minecraft:old_growth_pine_taiga", "minecraft:old_growth_spruce_taiga":
		default:
			continue
		}
		col := g.columnAt(x, z)
		top := col.topBlock()
		// BlockBlobFeature walks down to #forest_rock_can_place_on; the
		// column's floor is that or the rock does not sit.
		if !forestRockBase(top) || g.carve(top, x, col.h-1, z, col.h) == Air || col.h <= MinY+3 {
			continue
		}
		y := col.h
		// Three blobs, each shifting the origin by up to one west, north and
		// down: the rock reaches three blocks one way and one the other.
		if g.builtIn(x-4, y-4, z-4, x+2, y+2, z+2) {
			continue
		}
		rr := newTreeRNG(g.seed^forestRockSalt^0x55, x, z)
		for b := 0; b < 3; b++ {
			xr, yr, zr := rr.Intn(2), rr.Intn(2), rr.Intn(2)
			tr := float32(xr+yr+zr)*0.333 + 0.5
			for dx := -xr; dx <= xr; dx++ {
				for dy := -yr; dy <= yr; dy++ {
					for dz := -zr; dz <= zr; dz++ {
						if float32(dx*dx+dy*dy+dz*dz) <= tr*tr && c.in(x+dx, z+dz) {
							c.put(x+dx, y+dy, z+dz, MossyCobblestone)
						}
					}
				}
			}
			x += -1 + rr.Intn(2)
			y -= rr.Intn(2)
			z += -1 + rr.Intn(2)
		}
	}
}

// forestRockBase is #forest_rock_can_place_on: #substrate_overworld and
// #base_stone_overworld.
func forestRockBase(s uint32) bool {
	if IsDirtTag(s) {
		return true
	}
	switch s {
	case Stone, stoneGranite, stoneDiorite, stoneAndesite, stoneTuff, Deepslate:
		return true
	}
	return false
}

// iceSpikeReplaceable is #ice_spike_replaceable (#substrate_overworld,
// snow block, ice) — what a spike or its pillar may take besides air.
func iceSpikeReplaceable(s uint32) bool {
	return s == SnowBlock || s == Ice || IsDirtTag(s)
}

// icePatchTarget is ice_patch's target list.
func icePatchTarget(s uint32) bool {
	switch s {
	case Dirt, GrassBlock, Podzol, CoarseDirt, Mycelium, SnowBlock, Ice:
		return true
	}
	return false
}

// iceSpikesOf is placed_features ice_spike (count 3) and ice_patch (count
// 2) for the origin chunk at (ox, oz), in that order.
func (g *Generator) iceSpikesOf(ch *Chunk, cx, cz int32, ox, oz int) {
	c := chunkCell{ch, int(cx) * 16, int(cz) * 16}
	r := newTreeRNG(g.seed^iceSpikeSalt, ox, oz)
	for i := 0; i < 3; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		if g.resolveBiome(x, z).Name != "minecraft:ice_spikes" {
			continue
		}
		// MOTION_BLOCKING, then down through the air to the snow block the
		// spike must stand on.
		col := g.columnAt(x, z)
		if col.topBlock() != SnowBlock || g.carve(SnowBlock, x, col.h-1, z, col.h) == Air {
			continue
		}
		g.iceSpike(c, x, col.h-1, z)
	}
	r = newTreeRNG(g.seed^icePatchSalt, ox, oz)
	for i := 0; i < 2; i++ {
		x, z := ox+r.Intn(16), oz+r.Intn(16)
		radius := 2 + r.Intn(2)
		if g.resolveBiome(x, z).Name != "minecraft:ice_spikes" {
			continue
		}
		// MOTION_BLOCKING, one down, and that block must be snow.
		col := g.columnAt(x, z)
		y := col.h - 1
		if col.topBlock() != SnowBlock || g.carve(SnowBlock, x, y, z, col.h) == Air {
			continue
		}
		if g.builtIn(x-radius, y-1, z-radius, x+radius, y+4, z+radius) {
			continue
		}
		// DiskFeature, half height 1: each column of the circle from one
		// above the origin to one below it.
		for dx := -radius; dx <= radius; dx++ {
			for dz := -radius; dz <= radius; dz++ {
				if dx*dx+dz*dz > radius*radius || !c.in(x+dx, z+dz) {
					continue
				}
				for py := y + 1; py > y-2; py-- {
					if icePatchTarget(c.get(x+dx, py, z+dz)) {
						c.put(x+dx, py, z+dz, PackedIce)
					}
				}
			}
		}
	}
}

// iceSpike is SpikeFeature with packed ice on the snow block at (x, y, z).
// The spike's shape comes from its own position; the ragged rim and the
// pillar runs are drawn per cell and per column, so the chunk passes a spike
// spans lay the same spike.
func (g *Generator) iceSpike(c chunkCell, x, y, z int) {
	sr := newTreeRNG(g.seed^iceSpikeSalt^0x77, x, z)
	oy := y + sr.Intn(4)
	height := sr.Intn(4) + 7
	width := height/4 + sr.Intn(2)
	if width > 1 && sr.Intn(60) == 0 {
		oy += 10 + sr.Intn(30) // the rare spike on a tall pillar
	}
	// The body reaches width (at most three) each way and height both up
	// and down; the pillar goes down to y=50.
	if g.builtIn(x-3, 50, z-3, x+3, oy+height, z+3) {
		return
	}
	take := func(px, py, pz int) {
		if !c.in(px, pz) {
			return
		}
		if s := c.get(px, py, pz); s == Air || iceSpikeReplaceable(s) {
			c.put(px, py, pz, PackedIce)
		}
	}
	for yOff := 0; yOff < height; yOff++ {
		scale := (1 - float32(yOff)/float32(height)) * float32(width)
		nw := ceilF(float64(scale))
		for xo := -nw; xo <= nw; xo++ {
			fdx := float32(absInt(xo)) - 0.25
			for zo := -nw; zo <= nw; zo++ {
				fdz := float32(absInt(zo)) - 0.25
				if !(xo == 0 && zo == 0) && fdx*fdx+fdz*fdz > scale*scale {
					continue
				}
				rim := xo == -nw || xo == nw || zo == -nw || zo == nw
				if rim && hash01(g.seed^int64(x)*0x1CE5^int64(z)*0x51ED, xo*64+yOff, zo, iceSpikeSalt) > 0.75 {
					continue // the ragged rim
				}
				take(x+xo, oy+yOff, z+zo)
				if yOff != 0 && nw > 1 {
					take(x+xo, oy-yOff, z+zo)
				}
			}
		}
	}
	pw := width - 1
	if pw < 0 {
		pw = 0
	} else if pw > 1 {
		pw = 1
	}
	for xo := -pw; xo <= pw; xo++ {
		for zo := -pw; zo <= pw; zo++ {
			px, pz := x+xo, z+zo
			if !c.in(px, pz) {
				continue
			}
			pr := newTreeRNG(g.seed^iceSpikeSalt^int64(oy)<<20, px, pz)
			run := 50
			if absInt(xo) == 1 && absInt(zo) == 1 {
				run = pr.Intn(5)
			}
			for py := oy - 1; py > 50; {
				s := c.get(px, py, pz)
				if s != Air && !iceSpikeReplaceable(s) && s != PackedIce {
					break
				}
				c.put(px, py, pz, PackedIce)
				py--
				if run--; run <= 0 {
					py -= pr.Intn(5) + 1
					run = pr.Intn(5)
				}
			}
		}
	}
}

// stampBlueIce is placed_feature blue_ice: in the frozen oceans, 0-19 tries
// per chunk at y 30-61, each needing water where it stands (or just under
// it) and packed ice beside or above it. Icebergs are what give it packed
// ice, so the tries read a scratch copy of every berg that can reach them
// (the 5×5 chunks around this one) over the pure terrain; each try spreads
// from its own blue ice alone, so a chunk pass never depends on which
// neighbours it replays. It runs after the icebergs.
func (g *Generator) stampBlueIce(ch *Chunk, cx, cz int32) {
	frozen := false
	for dcx := int32(-2); dcx <= 2 && !frozen; dcx++ {
		for dcz := int32(-2); dcz <= 2 && !frozen; dcz++ {
			frozen = isFrozenOcean(g.resolveBiome(int(cx+dcx)*16+8, int(cz+dcz)*16+8).Name)
		}
	}
	if !frozen {
		return
	}
	view := &owRegion{g: g, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{},
		capture: map[[3]int]uint32{}}
	for dcx := int32(-2); dcx <= 2; dcx++ {
		for dcz := int32(-2); dcz <= 2; dcz++ {
			g.icebergsOf(cx+dcx, cz+dcz, view)
		}
	}
	if len(view.capture) == 0 {
		return // no berg in reach: nothing for blue ice to cling to
	}
	c := chunkCell{ch, int(cx) * 16, int(cz) * 16}
	for ncx := cx - 1; ncx <= cx+1; ncx++ {
		for ncz := cz - 1; ncz <= cz+1; ncz++ {
			ox, oz := int(ncx)*16, int(ncz)*16
			r := newTreeRNG(g.seed^blueIceSalt, ox, oz)
			n := r.Intn(20)
			for i := 0; i < n; i++ {
				x, z := ox+r.Intn(16), oz+r.Intn(16)
				y := 30 + r.Intn(32)
				if !isFrozenOcean(g.resolveBiome(x, z).Name) {
					continue
				}
				g.blueIce(c, view, x, y, z)
			}
		}
	}
}

// blueIce is BlueIceFeature at (x, y, z), read through view, writing the
// chunk's share.
func (g *Generator) blueIce(c chunkCell, view *owRegion, x, y, z int) {
	if y > SeaLevel-1 {
		return
	}
	if view.read(x, y, z) != Water && view.read(x, y-1, z) != Water {
		return
	}
	packed := false
	for _, d := range [5][3]int{{0, 1, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}} {
		if view.read(x+d[0], y+d[1], z+d[2]) == PackedIce {
			packed = true
			break
		}
	}
	if !packed {
		return
	}
	// The spread reaches two blocks across and five down or four up.
	if g.builtIn(x-3, y-6, z-3, x+3, y+5, z+3) {
		return
	}
	placed := map[[3]int]bool{{x, y, z}: true}
	isBlue := func(px, py, pz int) bool { return placed[[3]int{px, py, pz}] || view.read(px, py, pz) == BlueIce }
	br := newTreeRNG(g.seed^blueIceSalt^int64(y)<<24, x, z)
	for i := 0; i < 200; i++ {
		yOff := br.Intn(5) - br.Intn(6)
		d := 3
		if yOff < 2 {
			d += yOff / 2
		}
		if d < 1 {
			continue
		}
		px, py, pz := x+br.Intn(d)-br.Intn(d), y+yOff, z+br.Intn(d)-br.Intn(d)
		if placed[[3]int{px, py, pz}] {
			continue
		}
		s := view.read(px, py, pz)
		if s != Air && s != Water && s != PackedIce && s != Ice {
			continue
		}
		for _, n := range [6][3]int{{0, 1, 0}, {0, -1, 0}, {1, 0, 0}, {-1, 0, 0}, {0, 0, 1}, {0, 0, -1}} {
			if isBlue(px+n[0], py+n[1], pz+n[2]) {
				placed[[3]int{px, py, pz}] = true
				break
			}
		}
	}
	for p := range placed {
		if c.in(p[0], p[2]) {
			c.put(p[0], p[1], p[2], BlueIce)
		}
	}
}
