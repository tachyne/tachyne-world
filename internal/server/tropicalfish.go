package server

// The tropical fish's variant — the one mob in the sea that is not all one
// colour. Vanilla packs a pattern and two dye colours into a single int and
// syncs it as DATA_ID_TYPE_VARIANT, and from that the client draws all 3,072
// combinations. Without it every tropical fish in the world is the same white
// kob.
//
// TropicalFish is a WaterAnimal, not an AgeableMob — checked against 26.2,
// where WaterAnimal still extends PathfinderMob and only the separate
// AgeableWaterCreature carries the extra field — so the index is 17 on every
// client this server speaks to, with no shift.

// metaIndexFishVariant is DATA_ID_TYPE_VARIANT: AbstractFish's FROM_BUCKET
// takes 16, so the fish's own first field is 17.
const metaIndexFishVariant = 17

// tropicalPattern is a pattern's packed id: the body size in the low byte,
// the pattern's own index in the next.
type tropicalPattern struct {
	large bool
	shape int32
}

func (p tropicalPattern) packed() int32 {
	base := int32(0)
	if p.large {
		base = 1
	}
	return base | p.shape<<8
}

// The twelve patterns, in vanilla's enum order: six on the small body, six on
// the large one.
var (
	patKob       = tropicalPattern{false, 0}
	patSunstreak = tropicalPattern{false, 1}
	patSnooper   = tropicalPattern{false, 2}
	patDasher    = tropicalPattern{false, 3}
	patBrinely   = tropicalPattern{false, 4}
	patSpotty    = tropicalPattern{false, 5}
	patFlopper   = tropicalPattern{true, 0}
	patStripey   = tropicalPattern{true, 1}
	patGlitter   = tropicalPattern{true, 2}
	patBlockfish = tropicalPattern{true, 3}
	patBetty     = tropicalPattern{true, 4}
	patClayfish  = tropicalPattern{true, 5}
)

// Dye ordinals, as DyeColor numbers them (dyeColors in sign.go is the same
// order, which is what makes these readable).
const (
	dyeWhite     = 0
	dyeOrange    = 1
	dyeLightBlue = 3
	dyeYellow    = 4
	dyeLime      = 5
	dyePink      = 6
	dyeGray      = 7
	dyeCyan      = 9
	dyePurple    = 10
	dyeBlue      = 11
	dyeRed       = 14
)

// packTropicalVariant is TropicalFish.packVariant.
func packTropicalVariant(p tropicalPattern, body, pattern int32) int32 {
	return p.packed()&0xFFFF | (body&0xFF)<<16 | (pattern&0xFF)<<24
}

// tropicalCommon is COMMON_VARIANTS: the twenty-two fish that have names in
// the game and make up nine spawns in ten.
var tropicalCommon = []int32{
	packTropicalVariant(patStripey, dyeOrange, dyeGray),
	packTropicalVariant(patFlopper, dyeGray, dyeGray),
	packTropicalVariant(patFlopper, dyeGray, dyeBlue),
	packTropicalVariant(patClayfish, dyeWhite, dyeGray),
	packTropicalVariant(patSunstreak, dyeBlue, dyeGray),
	packTropicalVariant(patKob, dyeOrange, dyeWhite),
	packTropicalVariant(patSpotty, dyePink, dyeLightBlue),
	packTropicalVariant(patBlockfish, dyePurple, dyeYellow),
	packTropicalVariant(patClayfish, dyeWhite, dyeRed),
	packTropicalVariant(patSpotty, dyeWhite, dyeYellow),
	packTropicalVariant(patGlitter, dyeWhite, dyeGray),
	packTropicalVariant(patClayfish, dyeWhite, dyeOrange),
	packTropicalVariant(patDasher, dyeCyan, dyePink),
	packTropicalVariant(patBrinely, dyeLime, dyeLightBlue),
	packTropicalVariant(patBetty, dyeRed, dyeWhite),
	packTropicalVariant(patSnooper, dyeGray, dyeRed),
	packTropicalVariant(patBlockfish, dyeRed, dyeWhite),
	packTropicalVariant(patFlopper, dyeWhite, dyeYellow),
	packTropicalVariant(patKob, dyeRed, dyeWhite),
	packTropicalVariant(patSunstreak, dyeGray, dyeWhite),
	packTropicalVariant(patDasher, dyeCyan, dyeYellow),
	packTropicalVariant(patFlopper, dyeYellow, dyeYellow),
}

// tropicalPatterns is every pattern, for the one spawn in ten that is drawn
// freely rather than from the common list.
var tropicalPatterns = []tropicalPattern{
	patKob, patSunstreak, patSnooper, patDasher, patBrinely, patSpotty,
	patFlopper, patStripey, patGlitter, patBlockfish, patBetty, patClayfish,
}

// rollTropicalVariant is TropicalFish.finalizeSpawn: nine times in ten one of
// the named fish, and otherwise a pattern and two colours drawn at random —
// which is where the rare one nobody has seen before comes from.
func (h *hub) rollTropicalVariant() int32 {
	if h.rng.Float64() < 0.9 {
		return tropicalCommon[h.rng.Intn(len(tropicalCommon))]
	}
	p := tropicalPatterns[h.rng.Intn(len(tropicalPatterns))]
	return packTropicalVariant(p, int32(h.rng.Intn(len(dyeColors))), int32(h.rng.Intn(len(dyeColors))))
}
