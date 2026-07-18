package worldgen

// Deep-dark sculk generation. The deep_dark cave biome (see caveBiome) already
// exists; this carpets its cave floors with sculk and studs them with the sculk
// block family — dense floor coverage plus scattered sensors, catalysts, and
// CAN-SUMMON shriekers, so a wandering player's vibrations can rouse a Warden.
//
// A faithful-feel stand-in for SculkPatchFeature + the Ancient City sculk (not a
// structure port). Placement is a pure function of world coords, so a patch that
// straddles a chunk border agrees across the seam.

var (
	wgSculk         = blockBase("sculk")
	wgSculkVein     = blockBase("sculk_vein")
	wgSculkSensor   = blockBase("sculk_sensor")   // +1 = inactive, unpowered (default)
	wgSculkShrieker = blockBase("sculk_shrieker") // +3 = can_summon, not shrieking, dry
	wgSculkCatalyst = blockBase("sculk_catalyst") // +1 = bloom false
)

// deepDarkTopY caps how high sculk floors generate; the per-section deep_dark
// gate below is the real bound (deep_dark sections centre below y -32).
const deepDarkTopY = -16

// placeSculk stamps sculk onto the deep_dark cave floors of a generated chunk.
func (g *Generator) placeSculk(ch *Chunk, cx, cz int32) {
	deep := false
	for s := 0; s < g.sections; s++ {
		if ch.Biomes[s] == "minecraft:deep_dark" {
			deep = true
			break
		}
	}
	if !deep {
		return
	}
	baseX, baseZ := int(cx)*16, int(cz)*16
	for lx := 0; lx < 16; lx++ {
		for lz := 0; lz < 16; lz++ {
			wx, wz := baseX+lx, baseZ+lz
			for wy := caveMinY; wy <= deepDarkTopY; wy++ {
				s := (wy - MinY) / 16
				if s < 0 || s >= len(ch.Biomes) || ch.Biomes[s] != "minecraft:deep_dark" {
					continue
				}
				b := chBlock(ch, lx, wy, lz)
				if !sculkFloorable(b) || chBlock(ch, lx, wy+1, lz) != Air {
					continue // only carpet a cave FLOOR (solid with air above)
				}
				if sculk01(g.seed, wx, wy, wz, 0x5c010) < 0.85 {
					chSet(ch, lx, wy, lz, wgSculk)
				}
				// A feature stands in the air cell on top of the floor.
				switch f := sculk01(g.seed, wx, wy, wz, 0x5c020); {
				case f < 0.004:
					chSet(ch, lx, wy+1, lz, wgSculkShrieker+3) // can_summon → Warden path
				case f < 0.012:
					chSet(ch, lx, wy+1, lz, wgSculkSensor+1)
				case f < 0.018:
					chSet(ch, lx, wy+1, lz, wgSculkCatalyst+1)
				}
			}
		}
	}
}

// sculkFloorable reports whether a block may be carpeted with sculk — a solid
// full block that is neither bedrock nor already part of the sculk family.
func sculkFloorable(b uint32) bool {
	switch {
	case b == Air || b == Bedrock || b == wgSculk:
		return false
	case b >= wgSculkVein && b < wgSculkVein+128,
		b >= wgSculkSensor && b < wgSculkSensor+96,
		b >= wgSculkShrieker && b < wgSculkShrieker+8,
		b >= wgSculkCatalyst && b < wgSculkCatalyst+2:
		return false
	}
	return IsSolidFull(b)
}

// chBlock/chSet read and write a chunk cell by local x/z and WORLD y.
func chBlock(ch *Chunk, lx, wy, lz int) uint32 {
	s := (wy - MinY) / 16
	if s < 0 || s >= len(ch.Sections) {
		return Air
	}
	ly := (wy - MinY) - s*16
	return ch.Sections[s][(ly*16+lz)*16+lx]
}
func chSet(ch *Chunk, lx, wy, lz int, v uint32) {
	s := (wy - MinY) / 16
	if s < 0 || s >= len(ch.Sections) {
		return
	}
	ly := (wy - MinY) - s*16
	ch.Sections[s][(ly*16+lz)*16+lx] = v
}

// sculk01 hashes a world cell to [0,1) for deterministic, cross-chunk-consistent
// placement decisions.
func sculk01(seed int64, x, y, z int, salt uint64) float64 {
	return float64(cellHash(uint64(seed)^salt, x, y, z)>>11) / float64(1<<53)
}
