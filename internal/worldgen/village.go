package worldgen

// Villages: a deterministic well site on flat, dry land; the buildings, beds,
// job-sites and bell are assembled from the real vanilla jigsaw templates for
// the site's biome (village_jigsaw.go). The old hand-built cottage layout was
// removed once the templates went live.

const (
	villageCell = 384
	villageOdds = 0.85 // most cells roll a village (they were far too rare to ever find)
	villageFlat = 14   // max height spread across the site — pieces terrace on the slope

	// Protected spawn zone: Legion's castle is here and the migrated (larger)
	// jigsaw village would crowd it, so no village generates within this radius.
	villGuardX, villGuardZ = 33, -95
	villGuardR2            = 256 * 256
)

// Torch is defined here for historical reasons; it is also used by the
// stronghold generator.
var Torch = blockBase("torch")

// Village is the exported layout for one cell.
type Village struct {
	X, Z    int // well centre
	Y       int
	Variant string // biome variant: plains | desert | savanna | snowy | taiga
	Exists  bool
}

// villageVariant maps a site biome to its vanilla village style. Village
// placement stays flatness/dry-gated (not biome-gated) as tachyne always has;
// this only picks which template set stamps, so a desert biome gets a sandstone
// village instead of oak cottages. Anything without a village style falls back
// to plains (vanilla's default and the widest template set).
func villageVariant(biome string) string {
	switch biome {
	case "minecraft:desert":
		return "desert"
	case "minecraft:savanna", "minecraft:savanna_plateau":
		return "savanna"
	case "minecraft:snowy_plains", "minecraft:ice_spikes", "minecraft:snowy_slopes":
		return "snowy"
	case "minecraft:taiga", "minecraft:snowy_taiga",
		"minecraft:old_growth_pine_taiga", "minecraft:old_growth_spruce_taiga":
		return "taiga"
	default:
		return "plains"
	}
}

// VillageIn rolls the village for the cell containing (wx,wz).
func (g *Generator) VillageIn(wx, wz int) Village {
	ox, oz := cellOrigin(wx, villageCell), cellOrigin(wz, villageCell)
	if hash01(g.seed, ox, oz, 0x71A6E) >= villageOdds {
		return Village{}
	}
	cx := ox + villageCell/4 + int(hash01(g.seed, ox, oz, 0x71)*float64(villageCell/2))
	cz := oz + villageCell/4 + int(hash01(g.seed, ox, oz, 0x72)*float64(villageCell/2))
	// Protected zone: no village near the spawn area / Legion's castle, where the
	// migrated (larger) jigsaw village would crowd an established player build.
	if dx, dz := cx-villGuardX, cz-villGuardZ; dx*dx+dz*dz < villGuardR2 {
		return Village{}
	}
	// Flatness + dry-land gate: sample the site.
	lo, hi := 1<<30, -(1 << 30)
	for _, d := range [][2]int{{0, 0}, {24, 0}, {-24, 0}, {0, 24}, {0, -24}, {16, 16}, {-16, -16}} {
		h := g.Height(cx+d[0], cz+d[1])
		if h < lo {
			lo = h
		}
		if h > hi {
			hi = h
		}
	}
	if hi-lo > villageFlat || lo <= SeaLevel+1 {
		return Village{}
	}
	// Site only — the buildings, beds, job-sites and bell come from the real
	// vanilla templates via AssembleVillage (village_jigsaw.go). The variant
	// (biome style) is fixed by the biome at the well.
	return Village{X: cx, Z: cz, Y: g.Height(cx, cz),
		Variant: villageVariant(g.BiomeName(cx, cz)), Exists: true}
}

// cos01/sin01 take a turn fraction [0,1) instead of radians (used by end.go).
func cos01(t float64) float64 { return cosApprox(t) }
func sin01(t float64) float64 { return cosApprox(t - 0.25) }

// cosApprox: cheap cosine of a turn via a parabola pair.
func cosApprox(t float64) float64 {
	t -= float64(int(t))
	if t < 0 {
		t++
	}
	x := 4*t - 2
	if x < 0 {
		x = -x
	}
	x -= 1 // cos-shaped in [-1,1]
	return x * (2 - absF(x))
}

func absF(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// stampVillages stamps the real vanilla village pieces overlapping this chunk.
func (g *Generator) stampVillages(ch *Chunk, cx, cz int32) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	for ddx := -1; ddx <= 1; ddx++ {
		for ddz := -1; ddz <= 1; ddz++ {
			v := g.VillageIn(baseX+8+ddx*villageCell, baseZ+8+ddz*villageCell)
			if !v.Exists {
				continue
			}
			g.StampPieces(ch, cx, cz, g.AssembleVillage(v))
		}
	}
}
