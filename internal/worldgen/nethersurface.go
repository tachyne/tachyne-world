package worldgen

// The nether's surfaces — SurfaceRuleData.nether. Each biome dresses the
// caverns' floors and ceilings: the forests lay their nylium on the floor
// (bare netherrack where the netherrack noise runs high, a wart block where
// the wart noise peaks), the soul sand valley lines floor and ceiling with
// soul sand and soul soil by a selector noise, the basalt deltas with basalt
// above and basalt or blackstone below, and the wastes get a soul sand layer
// and gravel about the lava level. The vertical anchors are vanilla's,
// shifted onto the engine's lava sea (vanilla's sits at y=31).

const (
	netherFloorDepth   = 6 // UNDER_FLOOR: stoneDepthCheck(0, true, 6, FLOOR)
	netherCeilingDepth = 6 // UNDER_CEILING
)

var (
	SoulSoil   = blockBase("soul_soil")
	Basalt     = blockID("basalt")
	Blackstone = blockBase("blackstone")
)

// netherNoises are the surface-rule noises, seeded apart from the terrain's.
type netherNoises struct {
	netherrack, wart, selector, soulLayer, gravelLayer, patch *Perlin
}

func newNetherNoises(seed int64) *netherNoises {
	return &netherNoises{
		netherrack:  NewPerlin(seed ^ 0x6E01),
		wart:        NewPerlin(seed ^ 0x6E02),
		selector:    NewPerlin(seed ^ 0x6E03),
		soulLayer:   NewPerlin(seed ^ 0x6E04),
		gravelLayer: NewPerlin(seed ^ 0x6E05),
		patch:       NewPerlin(seed ^ 0x6E06),
	}
}

// The noise conditions. Vanilla's are normal noises with the octaves and
// thresholds named in NoiseData; these are the engine's Perlin at the same
// scales, thresholds set for the same sort of coverage.
func (n *netherNoises) bareNetherrack(x, z int) bool { // NETHERRACK -3 [1,0,0,0.35] > 0.54
	return n.netherrack.FBm(float64(x)/8, float64(z)/8, 2, 8, 0.35) > 0.38
}
func (n *netherNoises) wartBlock(x, z int) bool { // NETHER_WART -3 [1,0,0,0.9] > 1.17
	return n.wart.FBm(float64(x)/8, float64(z)/8, 2, 8, 0.9) > 0.8
}
func (n *netherNoises) stateSelector(x, z int) bool { // NETHER_STATE_SELECTOR -4 > 0
	return n.selector.Noise2(float64(x)/16, float64(z)/16) > 0
}
func (n *netherNoises) soulSandLayer(x, z int) bool { // SOUL_SAND_LAYER -8, four octaves > -0.012
	return n.soulLayer.FBm(float64(x)/256, float64(z)/256, 4, 2, 1) > -0.012
}
func (n *netherNoises) gravelLayerAt(x, z int) bool { // GRAVEL_LAYER
	return n.gravelLayer.FBm(float64(x)/256, float64(z)/256, 4, 2, 1) > -0.012
}
func (n *netherNoises) patchAt(x, z int) bool { // PATCH -5 > -0.012
	return n.patch.Noise2(float64(x)/32, float64(z)/32) > -0.012
}

// netherColumn is one column of the nether after its surface rules: the raw
// terrain (netherBlock) with every netherrack cell near a floor or ceiling
// dressed for its biome. Pure in (x, z), so chunk generation and the
// decorators' out-of-chunk reads agree.
func (g *Generator) netherColumn(x, z int) []uint32 {
	n := g.sections * 16
	col := make([]uint32, n)
	for i := range col {
		col[i] = g.netherBlock(x, MinY+i, z)
	}
	biome := g.netherBiome(x, z)
	nn := g.netherN
	if nn == nil {
		return col
	}
	solid := func(s uint32) bool { return s != Air && s != Lava }
	// floorDepth: solids counted down from the air above; ceilingDepth:
	// solids counted up from the air below.
	floorDepth := make([]int, n)
	ceilDepth := make([]int, n)
	depth := 1 << 20
	for i := n - 1; i >= 0; i-- {
		if !solid(col[i]) {
			depth = 0
			floorDepth[i] = -1
			continue
		}
		floorDepth[i] = depth
		depth++
	}
	depth = 1 << 20
	for i := 0; i < n; i++ {
		if !solid(col[i]) {
			depth = 0
			ceilDepth[i] = -1
			continue
		}
		ceilDepth[i] = depth
		depth++
	}
	lava := NetherLavaSea
	aboveLavaLevel := func(y int) bool { return y >= lava }             // yBlockCheck(31)
	aboveLavaSurface := func(y int) bool { return y >= lava+1 }         // yBlockCheck(32)
	inLavaBand := func(y int) bool { return y >= lava-1 && y < lava+4 } // yStartCheck(30) && !yStartCheck(35)
	gravelPatch := func(y int) bool { return nn.patchAt(x, z) && inLavaBand(y) }
	for i := 0; i < n; i++ {
		if col[i] != Netherrack {
			continue // ores, glowstone crusts and soul-sand patches keep their own rules
		}
		y := MinY + i
		onFloor := floorDepth[i] == 0
		underFloor := floorDepth[i] >= 0 && floorDepth[i] < netherFloorDepth
		underCeil := ceilDepth[i] >= 0 && ceilDepth[i] < netherCeilingDepth
		switch biome {
		case "minecraft:basalt_deltas":
			switch {
			case underCeil:
				col[i] = Basalt
			case underFloor && gravelPatch(y):
				col[i] = Gravel
			case underFloor && nn.stateSelector(x, z):
				col[i] = Basalt
			case underFloor:
				col[i] = Blackstone
			}
		case "minecraft:soul_sand_valley":
			switch {
			case underCeil || underFloor:
				if underFloor && gravelPatch(y) {
					col[i] = Gravel
				} else if nn.stateSelector(x, z) {
					col[i] = SoulSand
				} else {
					col[i] = SoulSoil
				}
			}
		case "minecraft:warped_forest", "minecraft:crimson_forest":
			if onFloor && !nn.bareNetherrack(x, z) && aboveLavaLevel(y) {
				warped := biome == "minecraft:warped_forest"
				switch {
				case nn.wartBlock(x, z) && warped:
					col[i] = WarpedWartBlock
				case nn.wartBlock(x, z):
					col[i] = NetherWartBlock
				case warped:
					col[i] = WarpedNylium
				default:
					col[i] = CrimsonNylium
				}
			}
		default: // nether wastes
			switch {
			case underFloor && nn.soulSandLayer(x, z) && inLavaBand(y):
				col[i] = SoulSand
			case onFloor && aboveLavaLevel(y) && y < lava+4 && nn.gravelLayerAt(x, z) && aboveLavaSurface(y):
				col[i] = Gravel
			}
		}
	}
	return col
}
