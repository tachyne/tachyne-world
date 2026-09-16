package worldgen

// Fossils — FossilFeature. In deserts, swamps and mangrove swamps, one chunk
// in sixty-four gets a fossil in the upper rock (coal ore in its bones) and
// one in sixty-four one in the deepslate (deepslate diamond ore): one of
// four spines or four skulls from vanilla's templates, turned at random,
// sunk fifteen to twenty-four below the lowest ground over its footprint,
// nine bones in ten kept (the rest rotted away) and one ore block in ten of
// the overlay, and skipped where more than four of its corners open on air
// or fluid.

var fossilBiomes = map[string]bool{"minecraft:desert": true, "minecraft:swamp": true, "minecraft:mangrove_swamp": true}

var fossilNames = []string{"spine_1", "spine_2", "spine_3", "spine_4", "skull_1", "skull_2", "skull_3", "skull_4"}

// stampFossils replays the 3×3 chunks' fossil draws into this chunk.
func (g *Generator) stampFossils(ch *Chunk, cx, cz int32) {
	if TemplateByName("fossil/spine_1") == nil {
		return
	}
	reg := &owRegion{g: g, ch: ch, baseX: int(cx) * 16, baseZ: int(cz) * 16, cols: map[[2]int]column{}}
	for dcx := int32(-1); dcx <= 1; dcx++ {
		for dcz := int32(-1); dcz <= 1; dcz++ {
			ncx, ncz := cx+dcx, cz+dcz
			ox, oz := int(ncx)*16, int(ncz)*16
			r := newTreeRNG(g.seed^0xF055, ox, oz)
			if r.Intn(64) == 0 { // FOSSIL_UPPER: y 0..top
				x, z := ox+r.Intn(16), oz+r.Intn(16)
				y := r.Intn(MinY + len(ch.Sections)*16)
				if fossilBiomes[g.resolveBiome(x, z).Name] {
					g.fossil(r, reg, x, y, z, false)
				}
			}
			if r.Intn(64) == 0 { // FOSSIL_LOWER: bottom..-8
				x, z := ox+r.Intn(16), oz+r.Intn(16)
				y := MinY + r.Intn(-8-MinY+1)
				if fossilBiomes[g.resolveBiome(x, z).Name] {
					g.fossil(r, reg, x, y, z, true)
				}
			}
		}
	}
}

// fossil is FossilFeature.place.
func (g *Generator) fossil(r TreeRNG, reg *owRegion, x, y, z int, deep bool) {
	rot := r.Intn(4)
	name := fossilNames[r.Intn(len(fossilNames))]
	base, overlay := TemplateByName("fossil/"+name), TemplateByName("fossil/"+name+"_coal")
	if base == nil || overlay == nil {
		return
	}
	sx, _, sz := base.rotatedSize(rot)
	lowX, lowZ := x-sx/2, z-sz/2
	lowest := y
	for dx := 0; dx < sx; dx++ {
		for dz := 0; dz < sz; dz++ {
			if h := g.Height(lowX+dx, lowZ+dz); h < lowest { // OCEAN_FLOOR_WG
				lowest = h
			}
		}
	}
	ty := lowest - 15 - r.Intn(10)
	if ty < MinY+10 {
		ty = MinY + 10
	}
	_, sy, _ := base.rotatedSize(rot)
	empty := 0
	for _, c := range [8][3]int{{0, 0, 0}, {1, 0, 0}, {0, 1, 0}, {1, 1, 0}, {0, 0, 1}, {1, 0, 1}, {0, 1, 1}, {1, 1, 1}} {
		s := reg.read(lowX+c[0]*(sx-1), ty+c[1]*(sy-1), lowZ+c[2]*(sz-1))
		if s == Air || s == Water || s == Lava {
			empty++
		}
	}
	if empty > 4 {
		return
	}
	seed := int64(newTreeRNG(g.seed, x, z).Intn(1 << 30))
	stampFossilPart(reg, base, lowX, ty, lowZ, rot, seed, 0.9, nil)
	remap := func(s uint32) uint32 { return s }
	if deep {
		remap = func(s uint32) uint32 {
			if s == CoalOre {
				return DeepslateDiamondOre
			}
			return s
		}
	}
	stampFossilPart(reg, overlay, lowX, ty, lowZ, rot, seed^0x5EED, 0.1, remap)
}

// stampFossilPart places a template's non-air blocks at integrity (the
// BlockRotProcessor), through a remap, clipped to the region's chunk.
func stampFossilPart(reg *owRegion, t *Template, ox, oy, oz, rot int, seed int64, integrity float64, remap func(uint32) uint32) {
	for _, b := range t.Blocks {
		state := t.resolved[rot&3][b[3]]
		if state == tmplSkip || state == Air {
			continue
		}
		rx, ry, rz := t.rotatePos(b[0], b[1], b[2], rot)
		wx, wy, wz := ox+rx, oy+ry, oz+rz
		if integrity < 1 && hash01(seed, wx, wz*8192+wy, 0xF0551) >= integrity {
			continue
		}
		if remap != nil {
			state = remap(state)
		}
		reg.set(wx, wy, wz, state)
	}
}
