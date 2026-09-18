package worldgen

import "math"

// Snow and ice — FreezeTopLayerFeature, the last decoration step. In every
// column the cell over the top motion-blocking block is checked: where the
// biome is cold enough at that height (Biome.coldEnoughToSnow: the base
// temperature, lowered above y=80 by altitude and a noise, below 0.15),
// still water freezes to ice and an air cell over a sturdy top gets a snow
// layer, the grass, podzol or mycelium under it turning snowy. Vanilla also
// asks for block light under ten; the generator has no lit blocks.

const snowTemperature = 0.15

var (
	Snow      = blockBase("snow") // one layer
	snowyTops = map[uint32]bool{GrassBlock: true, Podzol: true, Mycelium: true}
)

// heightAdjustedTemperature is Biome.getHeightAdjustedTemperature: above
// y=80 the temperature drops by 0.05 per 40 blocks, the boundary wobbling
// with a noise scaled to eight blocks.
func (g *Generator) heightAdjustedTemperature(biome string, x, y, z int) float64 {
	t := biomeTemperature[biome]
	if frozenModifier[biome] && g.surfN != nil { // TemperatureModifier.FROZEN: patches of open water
		large := g.surfN.frozenA.FBm(float64(x)*0.05, float64(z)*0.05, 3, 2, 0.5) * 14 // vanilla's simplex sum runs about twice this Perlin's
		edge := g.surfN.frozenB.Noise2(float64(x)*0.2, float64(z)*0.2)
		if large+edge < 0.3 && g.surfN.frozenB.Noise2(float64(x)*0.09, float64(z)*0.09) < 0.8 {
			t = 0.2
		}
	}
	if y > 80 {
		n := g.detail.Noise2(float64(x)/8, float64(z)/8) * 8
		return t - (n+float64(y)-80)*0.05/40
	}
	return t
}

func (g *Generator) coldEnoughToSnow(biome string, x, y, z int) bool {
	return g.heightAdjustedTemperature(biome, x, y, z) < snowTemperature
}

// freezeTopLayer runs FreezeTopLayerFeature over a chunk.
func (g *Generator) freezeTopLayer(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	top := MinY + len(ch.Sections)*16 - 1
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := baseX+lx, baseZ+lz
			// MOTION_BLOCKING: the first non-air cell from the top (fluids count)
			y := top
			for y > MinY && sectionBlockAt(ch, lx, y, lz) == Air {
				y--
			}
			if y >= top || y <= MinY {
				continue
			}
			below := sectionBlockAt(ch, lx, y, lz)
			above := y + 1
			biome := g.resolveBiome(wx, wz).Name
			if !g.coldEnoughToSnow(biome, wx, above, wz) {
				continue
			}
			if below == Snow { // a layer the ground cover laid: the grass under it turns snowy (GrassBlock.updateShape)
				if under := sectionBlockAt(ch, lx, y-1, lz); snowyTops[under] {
					if info, ok := InfoForState(under); ok && info.HasProperty("snowy") {
						setSectionBlock(ch, lx, y-1, lz, SetProperty(info, under, "snowy", "true"), true)
					}
				}
				continue
			}
			if below == Water { // shouldFreeze: a still water source
				setSectionBlock(ch, lx, y, lz, Ice, true)
				continue
			}
			if snowSurvivesOn(below) { // shouldSnow: Blocks.SNOW.canSurvive
				setSectionBlock(ch, lx, above, lz, Snow, true)
				if snowyTops[below] {
					if info, ok := InfoForState(below); ok && info.HasProperty("snowy") {
						setSectionBlock(ch, lx, y, lz, SetProperty(info, below, "snowy", "true"), true)
					}
				}
			}
		}
	}
}

// snowSurvivesOn is SnowLayerBlock.canSurvive's floor test: not ice, packed
// ice or a barrier, and a full-faced solid.
func snowSurvivesOn(s uint32) bool {
	if s == Ice || s == PackedIce || s == BlueIce || s == Air || IsFluid(s) || s == Snow {
		return false // (a thin snow layer has no sturdy top)
	}
	return Collides(s) || IsLeaves(s) // leaves carry snow too
}

// pi-free helper for the icebergs' angles lives here to share the file.
var twoPi = 2 * math.Pi
