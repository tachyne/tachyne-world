package worldgen

// Ruined portal variants — RuinedPortalStructure's setups and
// RuinedPortalPiece's processors, by biome: the desert's lie partly buried,
// the jungle's stand overgrown with vines and jungle leaves, the mountains'
// sit in the rock or on it with an air pocket, the ocean's rest on the sea
// floor (lava to magma), the swamp's on the water's bed with vines, and
// everywhere else half stand on the surface and half hide underground.
// Each is aged (BlockAgeProcessor: cracked and mossy masonry by its
// mossiness, crying obsidian one time in seven), its gold pilfered
// (gold blocks gone three times in ten), its lava turned to magma or, in
// the cold, netherrack, and stood on a spread of netherrack and magma with
// drips beneath (spreadNetherrack, addNetherrackDripColumnsBelowPortal).

const (
	plLand = iota
	plPartlyBuried
	plOceanFloor
	plMountain
	plUnderground
)

type portalSetup struct {
	airPocket float64
	canBeCold bool
	mossiness float64
	overgrown bool
	vines     bool
	placement int
	weight    float64
}

// portalProps are the chosen setup's rolls for one portal.
type portalProps struct {
	cold, airPocket, overgrown, vines bool
	mossiness                         float64
	placement                         int
}

var (
	portalStandard = []portalSetup{{1, true, 0.2, false, false, plUnderground, 0.5}, {0.5, true, 0.2, false, false, plLand, 0.5}}
	portalDesert   = []portalSetup{{0, false, 0, false, false, plPartlyBuried, 1}}
	portalJungle   = []portalSetup{{0.5, false, 0.8, true, true, plLand, 1}}
	portalMountain = []portalSetup{{1, true, 0.2, false, false, plMountain, 0.5}, {0.5, true, 0.2, false, false, plLand, 0.5}}
	portalOcean    = []portalSetup{{0, true, 0.8, false, false, plOceanFloor, 1}}
	portalSwamp    = []portalSetup{{0, false, 0.5, false, true, plOceanFloor, 1}}

	portalBiomeSets = map[string][]portalSetup{
		"minecraft:desert": portalDesert,
		"minecraft:jungle": portalJungle, "minecraft:bamboo_jungle": portalJungle, "minecraft:sparse_jungle": portalJungle,
		"minecraft:swamp": portalSwamp, "minecraft:mangrove_swamp": portalSwamp,
		"minecraft:badlands": portalMountain, "minecraft:eroded_badlands": portalMountain, "minecraft:wooded_badlands": portalMountain,
		"minecraft:windswept_hills": portalMountain, "minecraft:windswept_forest": portalMountain, "minecraft:windswept_gravelly_hills": portalMountain,
		"minecraft:savanna_plateau": portalMountain, "minecraft:windswept_savanna": portalMountain, "minecraft:stony_shore": portalMountain,
		"minecraft:meadow": portalMountain, "minecraft:frozen_peaks": portalMountain, "minecraft:jagged_peaks": portalMountain,
		"minecraft:stony_peaks": portalMountain, "minecraft:snowy_slopes": portalMountain, "minecraft:cherry_grove": portalMountain,
		"minecraft:ocean": portalOcean, "minecraft:deep_ocean": portalOcean, "minecraft:cold_ocean": portalOcean, "minecraft:deep_cold_ocean": portalOcean,
		"minecraft:frozen_ocean": portalOcean, "minecraft:deep_frozen_ocean": portalOcean, "minecraft:lukewarm_ocean": portalOcean,
		"minecraft:deep_lukewarm_ocean": portalOcean, "minecraft:warm_ocean": portalOcean,
	}
)

// portalSetupsFor is the ruined_portals structure set's choice by biome.
func portalSetupsFor(biome string) []portalSetup {
	if s, ok := portalBiomeSets[biome]; ok {
		return s
	}
	return portalStandard
}

// pickPortalSetup is findGenerationPoint's weighted pick.
func pickPortalSetup(setups []portalSetup, roll float64) portalSetup {
	total := 0.0
	for _, s := range setups {
		total += s.weight
	}
	for _, s := range setups {
		roll -= s.weight / total
		if roll < 0 {
			return s
		}
	}
	return setups[len(setups)-1]
}

// portalY is findSuitableY for the overworld placements: the origin's Y from
// the surface (its top block) and the template's height.
func portalY(placement, surfaceTop, ySpan, minY int, r1, r2 float64) int {
	switch placement {
	case plMountain:
		maxY := surfaceTop - ySpan
		if 70 < maxY {
			return 70 + int(r1*float64(maxY-70+1))
		}
		return maxY
	case plUnderground:
		lo, maxY := minY+15, surfaceTop-ySpan
		if lo < maxY {
			return lo + int(r1*float64(maxY-lo+1))
		}
		return maxY
	case plPartlyBuried:
		return surfaceTop - ySpan + 2 + int(r2*7)
	}
	return surfaceTop
}

// portal block families for the processors
var (
	rpStoneBricks    = blockBase("stone_bricks")
	rpStone          = blockBase("stone")
	rpChiseledBricks = blockBase("chiseled_stone_bricks")
	rpCrackedBricks  = blockBase("cracked_stone_bricks")
	rpMossyBricks    = blockBase("mossy_stone_bricks")
	rpBrickStairs    = rangeOf("stone_brick_stairs")
	rpMossyStairs    = rangeOf("mossy_stone_brick_stairs")
	rpStoneSlab      = rangeOf("stone_slab")
	rpBrickSlab      = rangeOf("stone_brick_slab")
	rpMossySlab      = rangeOf("mossy_stone_brick_slab")
	rpBrickWall      = rangeOf("stone_brick_wall")
	rpMossyWall      = rangeOf("mossy_stone_brick_wall")
	rpObsidian       = blockBase("obsidian")
	rpGoldBlock      = blockBase("gold_block")
	rpNetherrack     = blockBase("netherrack")
	rpMagma          = blockBase("magma_block")
	rpVineBase       = blockBase("vine")
	rpJungleLeaves   = withProps("jungle_leaves", "persistent", "true")
)

func rangeOf(name string) [2]uint32 {
	lo, hi := BlockRange(name)
	return [2]uint32{lo, hi}
}

func in(s uint32, r [2]uint32) bool { return s >= r[0] && s <= r[1] }

// portalRoll is a positional random in [0,1) for a portal's cell, so every
// chunk the portal straddles makes the same call.
func portalRoll(seed int64, x, y, z int, salt uint64) float64 {
	return hash01(seed, x, z*8192+y, salt)
}

// stampRuinedPortalVariant places the template through the piece's
// processors and runs its post-processing, clipped to the chunk.
func (g *Generator) stampRuinedPortalVariant(ch *Chunk, cx, cz int32, p RuinedPortal, t *Template) {
	baseX, baseZ := int(cx)*16, int(cz)*16
	seed := g.seed ^ int64(p.X)*0x9E37 ^ int64(p.Z)*0x7F4A
	set := func(wx, wy, wz int, s uint32) {
		lx, lz := wx-baseX, wz-baseZ
		if lx < 0 || lx >= 16 || lz < 0 || lz >= 16 || wy < MinY || wy >= MinY+len(ch.Sections)*16 {
			return
		}
		setSectionBlock(ch, lx, wy, lz, s, true)
	}
	reg := &owRegion{g: g, ch: ch, baseX: baseX, baseZ: baseZ}
	sx, sy, sz := t.rotatedSize(p.Rot)
	stairsFacing := [4]string{"north", "south", "west", "east"}
	// the template, through the rules and the block ageing
	for _, b := range t.Blocks {
		state := t.resolved[p.Rot&3][b[3]]
		if state == tmplSkip {
			continue
		}
		rx, ry, rz := t.rotatePos(b[0], b[1], b[2], p.Rot)
		wx, wy, wz := p.X+rx, p.Y+ry, p.Z+rz
		name := trimNS(t.Palette[b[3]].Name)
		if name == "air" && !p.Props.airPocket {
			continue // STRUCTURE_AND_AIR: the air stays whatever the ground was
		}
		roll := func(salt uint64) float64 { return portalRoll(seed, wx, wy, wz, salt) }
		switch {
		case state == rpGoldBlock:
			if roll(1) < 0.3 {
				state = Air
			}
		case IsLava(state):
			switch {
			case p.Props.placement == plOceanFloor:
				state = rpMagma
			case p.Props.cold:
				state = rpNetherrack
			case roll(2) < 0.2:
				state = rpMagma
			}
		case state == rpNetherrack:
			if !p.Props.cold && roll(3) < 0.07 {
				state = rpMagma
			}
		}
		// BlockAgeProcessor
		mossy := func(salt uint64) bool { return roll(salt) < p.Props.mossiness }
		switch {
		case state == rpStoneBricks || state == rpStone || state == rpChiseledBricks:
			if roll(4) < 0.5 {
				facing := stairsFacing[int(roll(5)*4)]
				if mossy(6) {
					if roll(7) < 0.5 {
						state = rpMossyBricks
					} else {
						state = withProps("mossy_stone_brick_stairs", "facing", facing)
					}
				} else if roll(7) < 0.5 {
					state = rpCrackedBricks
				} else {
					state = withProps("stone_brick_stairs", "facing", facing)
				}
			}
		case in(state, rpBrickStairs):
			if roll(4) < 0.5 {
				if mossy(6) {
					if roll(7) < 0.5 {
						state = rpMossyStairs[0] + (state - rpBrickStairs[0])
					} else {
						state = rpMossySlab[0]
					}
				} else if roll(7) < 0.5 {
					state = rpStoneSlab[0]
				} else {
					state = rpBrickSlab[0]
				}
			}
		case in(state, rpBrickSlab):
			if mossy(6) {
				state = rpMossySlab[0] + (state - rpBrickSlab[0])
			}
		case in(state, rpBrickWall):
			if mossy(6) {
				state = rpMossyWall[0] + (state - rpBrickWall[0])
			}
		case state == rpObsidian:
			if roll(8) < 0.15 {
				state = CryingObsidian
			}
		}
		set(wx, wy, wz, state)
	}
	// spreadNetherrack: a disc about the centre, on the ground for surface
	// placements, at the portal's floor otherwise, netherrack or magma with
	// drips below
	minX, minZ, minY := p.X, p.Z, p.Y
	cX, cZ := minX+sx/2, minZ+sz/2
	followGround := p.Props.placement == plLand || p.Props.placement == plOceanFloor
	probs := [14]float64{1, 1, 1, 1, 1, 1, 1, 0.9, 0.9, 0.8, 0.7, 0.6, 0.4, 0.2}
	avgWidth := (sx + sz) / 2
	distAdj := int(portalRoll(seed, minX, minY, minZ, 9) * float64(max(1, 8-avgWidth/2)))
	placeNetherrackOrMagma := func(x, y, z int) {
		if !p.Props.cold && portalRoll(seed, x, y, z, 10) < 0.07 {
			set(x, y, z, rpMagma)
		} else {
			set(x, y, z, rpNetherrack)
		}
	}
	dripColumn := func(x, y, z int) {
		placeNetherrackOrMagma(x, y, z)
		for cap := 8; cap > 0 && portalRoll(seed, x, y, z, 11) < 0.5; cap-- {
			y--
			placeNetherrackOrMagma(x, y, z)
		}
	}
	for x := cX - 14; x <= cX+14; x++ {
		for z := cZ - 14; z <= cZ+14; z++ {
			if x < baseX-1 || x > baseX+16 || z < baseZ-1 || z > baseZ+16 {
				continue // beyond this chunk (and its clipped neighbours' cells)
			}
			d := absInt(x-cX) + absInt(z-cZ) + distAdj
			if d < 0 {
				d = 0
			}
			if d >= len(probs) || portalRoll(seed, x, 0, z, 12) >= probs[d] {
				continue
			}
			surfaceY := reg.col(x, z).h - 1
			y := surfaceY
			if !followGround && minY < y {
				y = minY
			}
			if absInt(y-minY) > 3 {
				continue
			}
			s := reg.read(x, y, z)
			if s == Air || s == rpObsidian || IsLava(s) {
				continue
			}
			placeNetherrackOrMagma(x, y, z)
			if p.Props.overgrown && portalRoll(seed, x, y, z, 13) < 0.5 && reg.read(x, y+1, z) == Air {
				set(x, y+1, z, rpJungleLeaves)
			}
			dripColumn(x, y-1, z)
		}
	}
	// addNetherrackDripColumnsBelowPortal
	for x := minX + 1; x < minX+sx-1; x++ {
		for z := minZ + 1; z < minZ+sz-1; z++ {
			if reg.read(x, minY, z) == rpNetherrack {
				dripColumn(x, minY-1, z)
			}
		}
	}
	// vines on the masonry, jungle leaves over the netherrack
	if p.Props.vines || p.Props.overgrown {
		for x := minX; x < minX+sx; x++ {
			for z := minZ; z < minZ+sz; z++ {
				for y := minY; y < minY+sy; y++ {
					s := reg.read(x, y, z)
					if p.Props.vines && s != Air && !in(s, rangeOf("vine")) {
						d := [4][3]int{{0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}[int(portalRoll(seed, x, y, z, 14)*4)]
						nx, nz := x+d[0], z+d[2]
						if reg.read(nx, y, nz) == Air && Collides(s) && !IsReplaceable(s) {
							back := map[[3]int]string{{0, 0, -1}: "south", {0, 0, 1}: "north", {-1, 0, 0}: "east", {1, 0, 0}: "west"}[d]
							set(nx, y, nz, withProps("vine", back, "true"))
						}
					}
					if p.Props.overgrown && s == rpNetherrack && reg.read(x, y+1, z) == Air && portalRoll(seed, x, y, z, 15) < 0.5 {
						set(x, y+1, z, rpJungleLeaves)
					}
				}
			}
		}
	}
}
