package worldgen

// Earth mode: terrain from a real digital elevation model instead of noise.
// The DEM replaces exactly ONE thing — landHeight — and every downstream
// system (soil profile, ocean fill to sea level, biome resolution incl.
// beaches at the real waterline and snowy summits via the altitude lapse,
// decorations, heightmap, lighting) works unchanged, because they all derive
// from Height(). Rivers and caves are disabled in earth mode: the DEM already
// carries real valleys, and carving noise tunnels through real mountains
// would falsify them.
//
// Grids are produced by scripts/gen_earthdem.py from Copernicus GLO-30 (30 m
// global, ESA open data) and embedded via internal/worldgen/earthdata. The
// format is documented in the script. Generation stays DETERMINISTIC — the
// whole sharding overlap/cache design keeps working because every pod embeds
// the same grid.

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
)

//go:embed earthdata/*.bin.gz
var earthData embed.FS

// metresPerDegLat is the WGS84 mean; longitude metres shrink with cos(lat).
const metresPerDegLat = 111320.0

// seabedDepth is the v1 ocean floor below sea level (GLO-30 is a surface
// model: the sea reads ~0 m, carrying no bathymetry). Real bathymetry (GEBCO)
// is a later layer.
const seabedDepth = 10

// EarthDEM is an embedded elevation grid plus the local flat-earth mapping
// from block space to geographic coordinates: block (0,0) sits at (RefLat,
// RefLon); +x is east, +z is south (Minecraft convention); 1 block = 1 metre
// horizontally at the reference parallel.
type EarthDEM struct {
	Name           string  `json:"name"`
	LatN           float64 `json:"lat_n"` // north edge of the grid
	LonW           float64 `json:"lon_w"` // west edge of the grid
	D              float64 `json:"d"`     // degrees per sample (both axes)
	W              int     `json:"w"`
	H              int     `json:"h"`
	RefLat         float64 `json:"ref_lat"` // geographic position of block (0,0)
	RefLon         float64 `json:"ref_lon"`
	data           []int16 // metres, row-major, north→south / west→east
	mLat, mLon     float64 // metres per degree at the reference parallel
	vscale         float64 // metres of real elevation per block above sea level
	invD           float64
	maxRow, maxCol int
}

// LoadEarthDEM loads an embedded grid by name (e.g. "capetown") and fixes the
// vertical scale: block y = SeaLevel + elevation/vscale. The scale exists
// because the world is 384 blocks tall and mountains are not — pick it so the
// region's summit clears the build ceiling comfortably.
func LoadEarthDEM(name string, vscale float64) (*EarthDEM, error) {
	f, err := earthData.Open("earthdata/" + name + ".bin.gz")
	if err != nil {
		return nil, fmt.Errorf("earth: no embedded DEM %q: %w", name, err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, err
	}
	if len(raw) < 10 || !bytes.HasPrefix(raw, []byte("TDEM1\n")) {
		return nil, fmt.Errorf("earth: %q is not a TDEM1 grid", name)
	}
	n := binary.LittleEndian.Uint32(raw[6:10])
	var d EarthDEM
	if err := json.Unmarshal(raw[10:10+n], &d); err != nil {
		return nil, err
	}
	body := raw[10+n:]
	if len(body) != d.W*d.H*2 {
		return nil, fmt.Errorf("earth: %q grid is %d bytes, want %d", name, len(body), d.W*d.H*2)
	}
	d.data = make([]int16, d.W*d.H)
	for i := range d.data {
		d.data[i] = int16(binary.LittleEndian.Uint16(body[2*i:]))
	}
	d.mLat = metresPerDegLat
	d.mLon = metresPerDegLat * math.Cos(d.RefLat*math.Pi/180)
	d.invD = 1 / d.D
	d.maxRow, d.maxCol = d.H-1, d.W-1
	if vscale < 1 {
		vscale = 1
	}
	d.vscale = vscale
	return &d, nil
}

// sample bilinearly interpolates the elevation (metres) at a geographic point,
// clamping to the grid edge (the region map should end the world inside it).
func (d *EarthDEM) sample(lat, lon float64) float64 {
	// Continuous grid coordinates (pixel centres at +0.5).
	fr := (d.LatN-lat)*d.invD - 0.5
	fc := (lon-d.LonW)*d.invD - 0.5
	r0 := clampInt(int(math.Floor(fr)), 0, d.maxRow)
	c0 := clampInt(int(math.Floor(fc)), 0, d.maxCol)
	r1 := minInt(r0+1, d.maxRow)
	c1 := minInt(c0+1, d.maxCol)
	tr := clamp01(fr - float64(r0))
	tc := clamp01(fc - float64(c0))
	at := func(r, c int) float64 { return float64(d.data[r*d.W+c]) }
	top := at(r0, c0)*(1-tc) + at(r0, c1)*tc
	bot := at(r1, c0)*(1-tc) + at(r1, c1)*tc
	return top*(1-tr) + bot*tr
}

// heightAt maps a block column to its surface height. Land (elevation ≥ 1 m)
// rises from just above sea level by the vertical scale; the sea (the DSM
// reads ~0 m over water) gets a flat v1 seabed. ceiling is the world's
// exclusive top build limit — summits taller than the world clip flat there
// (pick vscale/ceiling together so the region's peaks fit; see docs/EARTH.md).
func (d *EarthDEM) heightAt(wx, wz, ceiling int) int {
	lat := d.RefLat - (float64(wz)+0.5)/d.mLat
	lon := d.RefLon + (float64(wx)+0.5)/d.mLon
	m := d.sample(lat, lon)
	if m < 1 {
		return SeaLevel - seabedDepth
	}
	h := SeaLevel + int(math.Round(m/d.vscale))
	return clampInt(h, SeaLevel+1, ceiling-2)
}

// BlockToLatLon reports the geographic position of a block column — for spawn
// planning, logs and tests.
func (d *EarthDEM) BlockToLatLon(wx, wz int) (lat, lon float64) {
	return d.RefLat - float64(wz)/d.mLat, d.RefLon + float64(wx)/d.mLon
}

// LatLonToBlock is the inverse of BlockToLatLon.
func (d *EarthDEM) LatLonToBlock(lat, lon float64) (wx, wz int) {
	return int(math.Round((lon - d.RefLon) * d.mLon)), int(math.Round((d.RefLat - lat) * d.mLat))
}

// SetEarth switches the generator to DEM terrain. Must be called before any
// generation (boot time); nether/end generators are never earth-mode.
func (g *Generator) SetEarth(d *EarthDEM) { g.earth = d }

// Earth reports the active DEM (nil = procedural noise terrain).
func (g *Generator) Earth() *EarthDEM { return g.earth }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
