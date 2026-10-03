package worldgen

// End cities in a vanilla-mode End: where 26.3 starts them. The
// end_cities structure set spreads one candidate a 20×20-chunk region
// (RandomSpreadStructurePlacement: separation 11, triangular, salt
// 10387313); EndCityStructure takes it when the chunk's biome is the
// highlands or midlands (#has_structure/end_city), draws a rotation from
// the start's WorldgenRandom, and sits the city at the lowest of the
// terrain's top blocks at the four corners of a 5×5 box from block 7,7
// (offset by the rotation), refusing a site below y=60. The pieces are the
// engine's EndCityPieces port (endcity.go) drawing from that same random.

const (
	vdmCitySpacing    = 20
	vdmCitySeparation = 11
	vdmCitySalt       = 10387313
)

// cityIn is the city of spread region (rx, rz), if it has one.
func (v *vanillaEnd) cityIn(rx, rz int) EndCity {
	r := newJavaRandom(int64(rx)*341873128712 + int64(rz)*132897987541 + v.seed + vdmCitySalt) // setLargeFeatureWithSalt
	limit := int32(vdmCitySpacing - vdmCitySeparation)
	ox := (r.nextInt(limit) + r.nextInt(limit)) / 2
	oz := (r.nextInt(limit) + r.nextInt(limit)) / 2
	cx, cz := int32(rx*vdmCitySpacing)+ox, int32(rz*vdmCitySpacing)+oz
	if b := v.chunkBiome(cx, cz); b != vdmEHighlands && b != vdmEMidlands {
		return EndCity{}
	}
	rot := int(vcLargeFeatureRandom(v.seed, cx, cz).nextInt(4))
	bx, bz := int(cx)*16+7, int(cz)*16+7
	dx, dz := 5, 5
	switch rot {
	case 1:
		dx = -5
	case 2:
		dx, dz = -5, -5
	case 3:
		dz = -5
	}
	y := min(min(v.firstOccupied(bx, bz), v.firstOccupied(bx, bz+dz)), min(v.firstOccupied(bx+dx, bz), v.firstOccupied(bx+dx, bz+dz)))
	if y < 60 {
		return EndCity{}
	}
	return EndCity{X: bx, Y: y, Z: bz, Rot: rot, Exists: true}
}

// firstOccupied is ChunkGenerator.getFirstOccupiedHeight over
// WORLD_SURFACE_WG: the terrain's top block, -1 over the void.
func (v *vanillaEnd) firstOccupied(x, z int) int {
	return v.terrain(int32(x>>4), int32(z>>4)).top(x&15, z&15)
}

// cityRandom is the start's random as the pieces find it: the chunk's
// large-feature seed, the rotation already drawn.
func (v *vanillaEnd) cityRandom(c EndCity) endCityRNG {
	r := vcLargeFeatureRandom(v.seed, int32(floorDiv16(c.X)), int32(floorDiv16(c.Z)))
	r.nextInt(4)
	return javaCityRNG{r}
}

// javaCityRNG is a WorldgenRandom as the city pieces draw from it.
type javaCityRNG struct{ r *javaRandom }

func (j javaCityRNG) intn(n int) int { return int(j.r.nextInt(int32(n))) }
func (j javaCityRNG) boolean() bool  { return j.r.next(1) != 0 }
func (j javaCityRNG) tag() int       { return int(j.r.next(32)) }
