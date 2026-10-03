package worldgen

// The vanilla-mode Nether and End as whole-dimension generators
// (VanillaDimension, vanillaseams.go): each chunk is the dimension's
// vanilla terrain (vanillanether.go, vanillaend.go) at its true heights on
// the canvas, its noise biome, and then its features — the placement pass
// when one is registered for the dimension, else the engine's own Nether
// and End features and structures, which read the vanilla terrain and
// biomes through the generator's usual entry points (netherBlock,
// netherColumn, netherBiome, endBlockCol, endOuterColumn, endBiome) and
// place on the Nether's vanilla frame (netherFrame).
//
// A Nether or End generator takes its vanilla dimension from
// useVanillaDimension, which the generator-mode switch calls at boot with
// the WORLD seed (NewNetherGenerator and NewEndGenerator scramble theirs).

func init() {
	RegisterVanillaDimension(DimNether, func(ctx VanillaGenContext) VanillaDimension {
		return newVanillaNether(ctx.Seed, ctx.Gen).withPlacement(ctx)
	})
	RegisterVanillaDimension(DimEnd, func(ctx VanillaGenContext) VanillaDimension {
		return newVanillaEnd(ctx.Seed, ctx.Gen).withPlacement(ctx)
	})
}

// useVanillaDimension installs the registered vanilla generator for this
// Nether or End generator (ctx.Gen is set to g; ctx.Dim decides which).
// Must be called at boot, before any chunk is generated. It reports
// whether one was installed.
func (g *Generator) useVanillaDimension(ctx VanillaGenContext) bool {
	if !g.nether && !g.end {
		return false
	}
	ctx.Dim = DimEnd
	if g.nether {
		ctx.Dim = DimNether
	}
	ctor := vanillaDimensionCtor[ctx.Dim]
	if ctor == nil {
		return false
	}
	ctx.Gen = g
	g.vdim = ctor(ctx)
	g.forgetEndSpikes()
	return g.vdim != nil
}

// vNether and vEnd are the generator's vanilla dimension (nil in native
// mode, or in the other dimension).
func (g *Generator) vNether() *vanillaNether {
	v, _ := g.vdim.(*vanillaNether)
	return v
}

func (g *Generator) vEnd() *vanillaEnd {
	v, _ := g.vdim.(*vanillaEnd)
	return v
}

// withPlacement takes the registered placement pass for the Nether, if any.
func (v *vanillaNether) withPlacement(ctx VanillaGenContext) *vanillaNether {
	if vanillaPlacementCtor != nil {
		ctx.Dim, ctx.Terrain, ctx.Biomes, ctx.Climate = DimNether, v, v, v
		v.place = vanillaPlacementCtor(ctx)
	}
	return v
}

// withPlacement takes the registered placement pass for the End, if any.
func (v *vanillaEnd) withPlacement(ctx VanillaGenContext) *vanillaEnd {
	if vanillaPlacementCtor != nil {
		ctx.Dim, ctx.Terrain, ctx.Biomes, ctx.Climate = DimEnd, v, v, nil
		v.place = vanillaPlacementCtor(ctx)
	}
	return v
}

// column is the terrain's column on the canvas (MinY..ceiling), as the
// engine's pure column functions give it.
func vdmColumn(t *vdmTerrain, sections, lx, lz int) []uint32 {
	col := make([]uint32, sections*16)
	for y := 0; y < vdmH && y-MinY < len(col); y++ {
		col[y-MinY] = vdmStates[t.at(lx, y, lz)]
	}
	return col
}

// top is the highest terrain cell of a column, or -1.
func (t *vdmTerrain) top(lx, lz int) int {
	for y := vdmH - 1; y >= 0; y-- {
		if t.codes[y*256+lz*16+lx] != vdmAir {
			return y
		}
	}
	return -1
}

// Height is VanillaTerrain.Height for the WG heightmaps: one above the
// highest block (WORLD_SURFACE_WG) or the highest solid one
// (OCEAN_FLOOR_WG: the lava sea does not count), 0 when none.
func vdmHeight(t *vdmTerrain, kind HeightmapType, lx, lz int) int {
	for y := vdmH - 1; y >= 0; y-- {
		c := t.codes[y*256+lz*16+lx]
		if c == vdmAir || (kind == HeightOceanFloorWG && c == vdmLava) {
			continue
		}
		return y + 1
	}
	return 0
}

// --- the Nether ---

// GenerateChunk is one vanilla Nether chunk (VanillaDimension).
func (v *vanillaNether) GenerateChunk(cx, cz int32) *Chunk {
	g := v.g
	ch := NewChunk(g.sections)
	t := v.terrain(cx, cz)
	vdmFill(ch, t)
	biome := vdmNNames[t.biome[2*4+2]] // the chunk's middle quart: one biome a section
	for s := range ch.Biomes {
		ch.Biomes[s] = biome
	}
	if v.place != nil {
		v.place.Decorate(ch, cx, cz)
	} else {
		g.adaptNetherTerrain(ch, cx, cz)
		g.stampNetherPortals(ch, cx, cz)
		g.stampBastions(ch, cx, cz)
		g.stampFortress(ch, cx, cz)
		g.stampNetherFossils(ch, cx, cz)
		g.decorateNether(ch, cx, cz)
	}
	ch.computeHeightmap()
	return ch
}

// SurfaceY is a safe spawn height: a cavern floor above the lava sea.
func (v *vanillaNether) SurfaceY(x, z int) float64 { return float64(v.g.NetherFloor(x, z)) }

// Height, MinY, Ceiling and SeaLevel make the Nether a VanillaTerrain: the
// dimension's own limits (y 0..255, sea level 32).
func (v *vanillaNether) Height(kind HeightmapType, x, z int) int {
	return vdmHeight(v.terrain(int32(x>>4), int32(z>>4)), kind, x&15, z&15)
}
func (v *vanillaNether) MinY() int     { return 0 }
func (v *vanillaNether) Ceiling() int  { return 256 }
func (v *vanillaNether) SeaLevel() int { return vdmNSeaLevel }

// blockBiome is the biome at a block as BiomeManager.getBiome answers it:
// the noise biome of the quart its fuzzed zoom picks.
func (v *vanillaNether) blockBiome(x, y, z int) string {
	qx, _, qz := vdmZoom(v.zoom, x, y, z)
	return vdmNNames[v.noiseBiome(qx, qz)]
}

// column is the terrain column (x, z) on the canvas.
func (v *vanillaNether) column(x, z int) []uint32 {
	return vdmColumn(v.terrain(int32(x>>4), int32(z>>4)), v.g.sections, x&15, z&15)
}

// --- the End ---

// GenerateChunk is one vanilla End chunk (VanillaDimension): the terrain,
// the obsidian spikes (from the dimension's floor, the square above y=65
// cleared) with their cages, caps and fire, the inactive exit podium, then
// the features.
func (v *vanillaEnd) GenerateChunk(cx, cz int32) *Chunk {
	g := v.g
	ch := NewChunk(g.sections)
	t := v.terrain(cx, cz)
	vdmFill(ch, t)
	bx, bz := int(cx)*16, int(cz)*16
	if spikes := g.EndSpikes(); len(spikes) > 0 {
		for lz := 0; lz < 16; lz++ {
			for lx := 0; lx < 16; lx++ {
				x, z := bx+lx, bz+lz
				if x*x+z*z > endSpikeReach*endSpikeReach {
					continue
				}
				for y := 0; y < vdmH; y++ {
					if s, ok := endSpikeCell(spikes, x, y, z); ok {
						setSectionBlock(ch, lx, y, lz, s, true)
					}
				}
			}
		}
	}
	g.endSpikeTops(ch, cx, cz)
	g.stampEndPodium(ch, cx, cz)
	biome := vdmENames[t.biome[0]]
	for s := range ch.Biomes {
		ch.Biomes[s] = biome
	}
	if v.place != nil {
		v.place.Decorate(ch, cx, cz)
	} else {
		g.decorateEnd(ch, cx, cz)
		g.stampEndCities(ch, cx, cz)
	}
	ch.computeHeightmap()
	return ch
}

// SurfaceY is a safe spawn height: over the column's top block, or the
// main island's usual height over the void.
func (v *vanillaEnd) SurfaceY(x, z int) float64 {
	if y := v.terrain(int32(x>>4), int32(z>>4)).top(x&15, z&15); y >= 0 {
		return float64(y + 1)
	}
	return float64(EndSurfaceY + 2)
}

// Height, MinY, Ceiling and SeaLevel make the End a VanillaTerrain.
func (v *vanillaEnd) Height(kind HeightmapType, x, z int) int {
	return vdmHeight(v.terrain(int32(x>>4), int32(z>>4)), kind, x&15, z&15)
}
func (v *vanillaEnd) MinY() int     { return 0 }
func (v *vanillaEnd) Ceiling() int  { return 256 }
func (v *vanillaEnd) SeaLevel() int { return 0 }

// cell is the End's generated block with the spikes in: what the engine's
// End features read beyond the chunk they decorate.
func (v *vanillaEnd) cell(x, y, z int) uint32 {
	if y >= 0 && x*x+z*z <= endSpikeReach*endSpikeReach {
		if s, ok := endSpikeCell(v.g.EndSpikes(), x, y, z); ok {
			return s
		}
	}
	return v.BlockAt(x, y, z)
}

// outerColumn is endOuterColumn's answer from the vanilla terrain: the top
// and bottom end stone of the column.
func (v *vanillaEnd) outerColumn(x, z int) (top, bottom int, ok bool) {
	t := v.terrain(int32(x>>4), int32(z>>4))
	top = t.top(x&15, z&15)
	if top < 0 {
		return 0, 0, false
	}
	bottom = top
	for bottom > 0 && t.at(x&15, bottom-1, z&15) != vdmAir {
		bottom--
	}
	return top, bottom, true
}
