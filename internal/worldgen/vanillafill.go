package worldgen

import (
	"encoding/json"
	"fmt"
	"math"
)

// The vanilla generator's noise stage: a dimension's noise settings
// (worldgen/noise_settings) compiled for a seed, and the chunk fill
// (NoiseBasedChunkGenerator.doFill) — final_density sampled over the
// chunk's volume, each cell's substance decided top-down by the aquifer
// (NoiseBasedAquifer over the settings' aquifer functions, or the global
// fluid rule when the settings have none), the default block where the
// density is positive.

// vtSettings is a parsed NoiseGeneratorSettings.
type vtSettings struct {
	name                       string
	minY, height               int
	seaLevel                   int
	defaultBlock, defaultFluid uint32
	legacy                     bool
	materialRule               string
	router                     map[string]*vdFn
	aquifers                   map[string]*vdFn // nil: Aquifer.createDisabled
}

// loadVTSettings reads worldgen/noise_settings/<name>.
func loadVTSettings(name string) (*vtSettings, error) {
	raw, ok := vanillaWorldgenData["noise_settings/"+vtStripNS(name)]
	if !ok {
		return nil, fmt.Errorf("noise settings %q not in the data", name)
	}
	var d struct {
		Noise struct {
			MinY   int `json:"min_y"`
			Height int `json:"height"`
		} `json:"noise"`
		SeaLevel     int                        `json:"sea_level"`
		DefaultBlock json.RawMessage            `json:"default_block"`
		DefaultFluid json.RawMessage            `json:"default_fluid"`
		Legacy       bool                       `json:"legacy_random_source"`
		MaterialRule string                     `json:"material_rule"`
		Router       map[string]json.RawMessage `json:"noise_router"`
		Aquifers     map[string]json.RawMessage `json:"aquifers"`
	}
	if err := json.Unmarshal([]byte(raw), &d); err != nil {
		return nil, fmt.Errorf("noise settings %q: %w", name, err)
	}
	s := &vtSettings{name: name, minY: d.Noise.MinY, height: d.Noise.Height, seaLevel: d.SeaLevel,
		legacy: d.Legacy, materialRule: d.MaterialRule, router: map[string]*vdFn{}}
	var err error
	if s.defaultBlock, err = vtParseState(d.DefaultBlock); err != nil {
		return nil, err
	}
	if s.defaultFluid, err = vtParseState(d.DefaultFluid); err != nil {
		return nil, err
	}
	for k, v := range d.Router {
		if s.router[k], err = vdParseJSON(v); err != nil {
			return nil, fmt.Errorf("noise settings %q router %s: %w", name, k, err)
		}
	}
	if d.Aquifers != nil {
		s.aquifers = map[string]*vdFn{}
		for k, v := range d.Aquifers {
			if s.aquifers[k], err = vdParseJSON(v); err != nil {
				return nil, fmt.Errorf("noise settings %q aquifers %s: %w", name, k, err)
			}
		}
	}
	return s, nil
}

// vtParseState reads a 26.x block state: a bare name (the default state) or
// {"id", "properties"} (or the older {"Name", "Properties"}).
func vtParseState(raw json.RawMessage) (uint32, error) {
	var name string
	if json.Unmarshal(raw, &name) == nil {
		return vtStateByName(name, nil)
	}
	var o struct {
		ID       string            `json:"id"`
		Name     string            `json:"Name"`
		Props    map[string]string `json:"properties"`
		PropsOld map[string]string `json:"Properties"`
	}
	if err := json.Unmarshal(raw, &o); err != nil {
		return 0, fmt.Errorf("block state %s: %w", raw, err)
	}
	if o.ID == "" {
		o.ID = o.Name
	}
	if o.Props == nil {
		o.Props = o.PropsOld
	}
	return vtStateByName(o.ID, o.Props)
}

// vtStateByName is a named block's state: its default with props set.
func vtStateByName(name string, props map[string]string) (uint32, error) {
	n := vtStripNS(name)
	if _, _, ok := BlockRangeOK(n); !ok {
		return 0, fmt.Errorf("block %q is not in the registry", name)
	}
	if len(props) == 0 {
		return BlockID(n), nil
	}
	s := BlockID(n)
	info, ok := InfoForState(s)
	if !ok {
		return 0, fmt.Errorf("block %q has no properties", name)
	}
	for k, v := range props {
		s = SetProperty(info, s, k, v)
	}
	return s, nil
}

// vtGen is a dimension's vanilla generator for a seed: the settings, the
// RandomState and the compiled router and aquifer functions.
type vtGen struct {
	set *vtSettings
	rs  *vtState

	final, temperature, vegetation, continents, erosion, depth, ridges, chunkSurface vdSampler
	aqBarrier, aqFlood, aqSpread, aqLava, aqExclusion, aqSurface                     vdSampler
	aqRandom                                                                         vtPositional

	water, lava, air uint32
}

func newVTGen(settings string, seed int64) (*vtGen, error) {
	set, err := loadVTSettings(settings)
	if err != nil {
		return nil, err
	}
	g := &vtGen{set: set, rs: newVTState(seed, set.legacy), water: Water, lava: Lava, air: Air}
	for _, f := range []struct {
		key string
		dst *vdSampler
	}{
		{"final_density", &g.final}, {"temperature", &g.temperature}, {"vegetation", &g.vegetation},
		{"continents", &g.continents}, {"erosion", &g.erosion}, {"depth", &g.depth}, {"ridges", &g.ridges},
		{"chunk_surface_level", &g.chunkSurface},
	} {
		fn, ok := set.router[f.key]
		if !ok {
			return nil, fmt.Errorf("noise settings %q: router has no %s", settings, f.key)
		}
		if *f.dst, err = g.rs.compile(fn); err != nil {
			return nil, fmt.Errorf("noise settings %q %s: %w", settings, f.key, err)
		}
	}
	if set.aquifers != nil {
		for _, f := range []struct {
			key string
			dst *vdSampler
		}{
			{"barrier", &g.aqBarrier}, {"fluid_level_floodedness", &g.aqFlood}, {"fluid_level_spread", &g.aqSpread},
			{"lava", &g.aqLava}, {"exclusion", &g.aqExclusion}, {"surface_level", &g.aqSurface},
		} {
			fn, ok := set.aquifers[f.key]
			if !ok {
				return nil, fmt.Errorf("noise settings %q: aquifers have no %s", settings, f.key)
			}
			if *f.dst, err = g.rs.compile(fn); err != nil {
				return nil, fmt.Errorf("noise settings %q aquifer %s: %w", settings, f.key, err)
			}
		}
		g.aqRandom = g.rs.randomFactory("minecraft:aquifer")
	}
	return g, nil
}

// vtNoiseChunk is NoiseChunk: one volume's sampler context and aquifer.
type vtNoiseChunk struct {
	g   *vtGen
	ctx *vdCtx
	vol vtVolume
	aq  *vaqChunk // nil: the disabled aquifer (global fluid only)
}

// vtAqSrc is the aquifer's view of the settings' functions in a context.
type vtAqSrc struct{ nc *vtNoiseChunk }

func (s vtAqSrc) barrier(x, y, z int) float32 { return s.nc.g.aqBarrier.value(s.nc.ctx, x, y, z) }
func (s vtAqSrc) flood(x, y, z int) float32   { return s.nc.g.aqFlood.value(s.nc.ctx, x, y, z) }
func (s vtAqSrc) spread(x, y, z int) float32  { return s.nc.g.aqSpread.value(s.nc.ctx, x, y, z) }
func (s vtAqSrc) lava(x, y, z int) float32    { return s.nc.g.aqLava.value(s.nc.ctx, x, y, z) }
func (s vtAqSrc) nextInts(gx, gy, gz int32) (int32, int32, int32) {
	r := s.nc.g.aqRandom.at(gx, gy, gz)
	return r.nextInt(10), r.nextInt(9), r.nextInt(10)
}

// newNoiseChunk is new NoiseChunk over vol (a caching context; the aquifer
// built at once, as vanilla builds it in the constructor).
func (g *vtGen) newNoiseChunk(vol vtVolume, beard vdSampler) *vtNoiseChunk {
	nc := &vtNoiseChunk{g: g, ctx: newVDCtx(), vol: vol}
	nc.ctx.beard = beard
	if g.aqBarrier != nil {
		surface := func(x, z int) int {
			return int(math.Floor(float64(g.aqSurface.value(nc.ctx, x, 0, z))))
		}
		surfaceVol := func(v vtVolume) []float32 {
			b := vdBuf(v.size())
			g.aqSurface.volume(nc.ctx, b, v)
			return b
		}
		excluded := func(x, y, z int) bool { return g.aqExclusion.value(nc.ctx, x, y, z) > 0 }
		nc.aq = newVaqChunkVol(vtAqSrc{nc}, vol.minX, vol.minY, vol.minZ, vol.maxX(), vol.maxY(), vol.maxZ(),
			surface, surfaceVol, excluded)
		nc.aq.global = g.globalFluid
	}
	return nc
}

// globalFluid is the settings' global fluid picker
// (NoiseBasedChunkGenerator.createFluidPicker): lava below
// min(-54, sea level), the default fluid up to sea level above.
func (g *vtGen) globalFluid(y int) vaqStatus {
	if y < min(vanillaLavaLevel, g.set.seaLevel) {
		return vaqStatus{vanillaLavaLevel, true}
	}
	if g.set.defaultFluid == Lava {
		return vaqStatus{int32(g.set.seaLevel), true}
	}
	if g.set.defaultFluid == Air {
		return vaqStatus{vaqWayBelow, false} // no fluid at any height
	}
	return vaqStatus{int32(g.set.seaLevel), false}
}

// substance is computeSubstance as a block: nil (-1) for the default block.
func (nc *vtNoiseChunk) substance(x, y, z int, density float64) uint32 {
	var code int8
	if nc.aq != nil {
		code = nc.aq.substance(x, y, z, density)
	} else if density > 0 {
		code = vaqSolid
	} else {
		code = nc.g.globalFluid(y).at(y)
	}
	switch code {
	case vaqSolid:
		return nc.g.set.defaultBlock
	case vaqWater:
		return nc.g.set.waterState()
	case vaqLava:
		return Lava
	}
	return Air
}

// waterState is the settings' "water": the default fluid (lava in the
// Nether, where the sea is lava).
func (s *vtSettings) waterState() uint32 {
	if s.defaultFluid == Air {
		return Air
	}
	return s.defaultFluid
}

// fillChunk is doFill: the noise-stage blocks of chunk (cx, cz) into ch.
func (g *vtGen) fillChunk(ch *Chunk, cx, cz int32, beard vdSampler) *vtNoiseChunk {
	vol := vtVol(16, g.set.height, 16, int(cx)*16, g.set.minY, int(cz)*16)
	nc := g.newNoiseChunk(vol, beard)
	dens := vdBuf(vol.size())
	g.final.volume(nc.ctx, dens, vol)
	for z := 0; z < 16; z++ {
		bz := vol.blockZ(z)
		for x := 0; x < 16; x++ {
			bx := vol.blockX(x)
			for y := vol.sy - 1; y >= 0; y-- {
				by := vol.blockY(y)
				s := nc.substance(bx, by, bz, float64(dens[vol.index(x, y, z)]))
				if s != Air {
					ch.setGen(x, by, z, s)
				}
			}
		}
	}
	return nc
}

// setGen writes a generated block at chunk-local (x, z), world y; cells
// outside the chunk's sections are dropped.
func (ch *Chunk) setGen(x, y, z int, s uint32) {
	dy := y - MinY
	if dy < 0 || dy >= len(ch.Sections)*16 {
		return
	}
	ch.Sections[dy>>4][((dy&15)*16+z)*16+x] = s
}

// getGen reads a block at chunk-local (x, z), world y (air outside).
func (ch *Chunk) getGen(x, y, z int) uint32 {
	dy := y - MinY
	if dy < 0 || dy >= len(ch.Sections)*16 {
		return Air
	}
	return ch.Sections[dy>>4][((dy&15)*16+z)*16+x]
}

// baseColumn is iterateNoiseColumn: the noise stage's blocks of one column
// (its own NoiseChunk, as vanilla makes one per column), top down, calling
// stop with each block until it returns true; it returns the y it stopped
// at, or MinY-1.
func (g *vtGen) baseColumn(x, z int, stop func(y int, s uint32) bool) int {
	vol := vtVol(1, g.set.height, 1, x, g.set.minY, z)
	nc := g.newNoiseChunk(vol, nil)
	dens := vdBuf(vol.size())
	g.final.volume(nc.ctx, dens, vol)
	for y := vol.sy - 1; y >= 0; y-- {
		by := vol.blockY(y)
		if stop(by, nc.substance(x, by, z, float64(dens[y]))) {
			return by
		}
	}
	return g.set.minY - 1
}
