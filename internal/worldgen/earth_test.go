package worldgen

import (
	"strings"
	"testing"
)

// The Cape Town grid must place real landmarks at plausible block heights
// (Copernicus GLO-30, vertical 1:4.5, sea level y=63). Tolerances are loose:
// the DSM is a surface model and landmark coordinates are approximate.
func TestEarthCapeTown(t *testing.T) {
	dem, err := LoadEarthDEM("capetown", 4.5)
	if err != nil {
		t.Fatal(err)
	}
	g := NewGenerator(1)
	g.SetEarth(dem)

	cases := []struct {
		name     string
		lat, lon float64
		lo, hi   int // acceptable surface-height band (block y)
	}{
		{"Table Mountain plateau", -33.9575, 18.4034, 270, 310}, // ~1000+ m
		{"Signal Hill", -33.9166, 18.4062, 110, 140},            // ~290 m
		{"City centre", -33.9180, 18.4232, 64, 75},              // ~20 m
		{"Table Bay (sea)", -33.8600, 18.4500, SeaLevel - seabedDepth, SeaLevel - seabedDepth},
		{"Muizenberg beach", -34.1080, 18.4700, 64, 68},
	}
	for _, c := range cases {
		wx, wz := dem.LatLonToBlock(c.lat, c.lon)
		h := g.Height(wx, wz)
		if h < c.lo || h > c.hi {
			t.Errorf("%s at block (%d,%d): height %d, want %d..%d", c.name, wx, wz, h, c.lo, c.hi)
		}
	}

	// The sea must BE sea: a Table Bay column fills with water up to sea level
	// and reads as an ocean biome.
	wx, wz := dem.LatLonToBlock(-33.8600, 18.4500)
	if b := g.BlockAt(wx, SeaLevel-1, wz); b != Water {
		t.Errorf("Table Bay at y=%d is block %d, want water", SeaLevel-1, b)
	}
	if biome := g.BiomeName(wx, wz); biome != "minecraft:ocean" && biome != "minecraft:deep_ocean" &&
		biome != "minecraft:cold_ocean" && biome != "minecraft:deep_cold_ocean" &&
		biome != "minecraft:lukewarm_ocean" && biome != "minecraft:deep_lukewarm_ocean" {
		t.Errorf("Table Bay biome = %s, want an ocean", biome)
	}

	// Cape Town is close to sea level and NOT snowy (found from a real phone
	// login: the noise-world altitude lapse and block-height mountain tiers,
	// both vscale× too aggressive per real metre, buried Signal Hill in grove
	// snow and glaciated Table Mountain). Nothing on the peninsula may resolve
	// to any snow-covered biome — snow needs ~2,600 m real; the peninsula tops
	// out at 1,085 m. Scan a coarse grid over the WHOLE region, not just
	// landmarks.
	snowy := func(b string) bool {
		return strings.HasPrefix(b, "minecraft:snowy") || strings.HasPrefix(b, "minecraft:frozen") ||
			b == "minecraft:ice_spikes" || b == "minecraft:grove"
	}
	for wz := -41000; wz <= 53000; wz += 800 {
		for wx := -15500; wx <= 53400; wx += 800 {
			if b := g.BiomeName(wx, wz); snowy(b) {
				t.Fatalf("snow at block (%d,%d): %s — Cape Town is not the Alps", wx, wz, b)
			}
		}
	}

	// Block (0,0) is the reference point (Cape Town city centre).
	if lat, lon := dem.BlockToLatLon(0, 0); lat != -33.92 || lon != 18.42 {
		t.Errorf("block origin = %f,%f", lat, lon)
	}

	// Full chunk generation works end to end in earth mode: the Signal Hill
	// chunk has a sane heightmap and a grass-or-stone surface, the Table Bay
	// chunk is sea (heightmap at the seabed, water above it).
	shx, shz := dem.LatLonToBlock(-33.9166, 18.4062)
	ch := g.GenerateChunk(int32(shx>>4), int32(shz>>4))
	if hm := ch.Heightmap[0]; hm < 100 || hm > 160 {
		t.Errorf("Signal Hill chunk heightmap[0] = %d, want ~110-140", hm)
	}
	sea := g.GenerateChunk(int32(wx>>4), int32(wz>>4))
	if hm := sea.Heightmap[0]; int(hm) != SeaLevel-1 {
		// MOTION_BLOCKING includes fluids: over the sea it sits at the water
		// surface, not the seabed.
		t.Errorf("Table Bay chunk heightmap[0] = %d, want water surface %d", hm, SeaLevel-1)
	}

	// Earth mode must be deterministic (the sharding overlap depends on it):
	// two independent generators agree everywhere.
	g2 := NewGenerator(99) // different seed — terrain must not care
	dem2, _ := LoadEarthDEM("capetown", 4.5)
	g2.SetEarth(dem2)
	for _, c := range cases {
		wx, wz := dem.LatLonToBlock(c.lat, c.lon)
		if g.Height(wx, wz) != g2.Height(wx, wz) {
			t.Errorf("%s: height differs across generators/seeds", c.name)
		}
	}
}

// TestTallWorldTrueScale: with the ceiling raised and vscale 1, the landmarks
// stand at their REAL elevations — the tall-world project's whole point.
// Table Mountain's summit plateau must be ~1,050-1,090 blocks up, not 285.
func TestTallWorldTrueScale(t *testing.T) {
	dem, err := LoadEarthDEM("capetown", 1.0)
	if err != nil {
		t.Fatal(err)
	}
	g := NewGenerator(1)
	g.SetCeiling(1664)
	g.SetEarth(dem)
	if g.SectionCount() != (1664+64)/16 {
		t.Fatalf("sections = %d", g.SectionCount())
	}
	cases := []struct {
		name     string
		lat, lon float64
		lo, hi   int
	}{
		{"Table Mountain plateau", -33.9575, 18.4034, SeaLevel + 950, SeaLevel + 1100},
		{"Signal Hill", -33.9166, 18.4062, SeaLevel + 250, SeaLevel + 330},
		{"City centre", -33.9180, 18.4232, SeaLevel + 1, SeaLevel + 40},
	}
	for _, c := range cases {
		wx, wz := dem.LatLonToBlock(c.lat, c.lon)
		h := g.Height(wx, wz)
		if h < c.lo || h > c.hi {
			t.Errorf("%s: height %d not in [%d, %d]", c.name, h, c.lo, c.hi)
		}
	}
	// A generated summit chunk must actually carry blocks above y=320.
	wx, wz := dem.LatLonToBlock(-33.9575, 18.4034)
	ch := g.GenerateChunk(int32(wx>>4), int32(wz>>4))
	if len(ch.Sections) != g.SectionCount() {
		t.Fatalf("chunk has %d sections, want %d", len(ch.Sections), g.SectionCount())
	}
	if ch.MaxHeight() <= 320 {
		t.Fatalf("summit chunk tops out at %d — nothing above the vanilla ceiling", ch.MaxHeight())
	}
}
