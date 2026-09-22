package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Eyeblossoms and nether portals — the two random-tick one-offs left after the
// plant, melting and geology passes.
//
// An eyeblossom is the pale garden's clock: it opens through the night and
// shuts at first light. A nether portal quietly breeds zombified piglins, at a
// rate set by the difficulty, which is why a portal left standing in the
// Nether becomes a piglin farm.

var (
	openEyeblossom   = worldgen.BlockBase("open_eyeblossom")
	closedEyeblossom = worldgen.BlockBase("closed_eyeblossom")
	// A potted eyeblossom keeps the same hours. FlowerPotBlock.randomTick
	// switches it on its own — with the long sound, and without waking
	// anything: a flower in a pot starts no wave and joins none.
	pottedOpenEyeblossom   = worldgen.BlockBase("potted_open_eyeblossom")
	pottedClosedEyeblossom = worldgen.BlockBase("potted_closed_eyeblossom")
	netherPortalBase       = worldgen.BlockBase("nether_portal")
	netherPortalMax        = func() uint32 { _, hi, _ := worldgen.BlockRangeOK("nether_portal"); return hi }()
)

// portalPiglinOdds is vanilla's roll: nextInt(2000) < difficulty id, so easy
// breeds slowly and hard several times as often.
const portalPiglinOdds = 2000

// The pale garden's night, in ticks of the day. Vanilla does not ask how
// bright the sky is here: the day timeline keyframes the eyeblossom's open
// state straight onto the clock, TRUE at 12600 and FALSE at 23401, and
// keyframes the creaking's waking hours onto the very same pair. Those two
// numbers ARE the rule, which is why neither of the engine's sky-brightness
// curves belongs in it.
const (
	paleNightStart = 12600
	paleNightEnd   = 23401
)

// tickEyeblossom opens the flower at night and closes it at dawn.
//
// Only under a real sky: the day timeline is tagged in_overworld, so in the
// Nether or the End there is nothing to tell the flower what hour it is and it
// keeps whichever face it was planted with, forever.
func (h *hub) tickEyeblossom(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	return h.switchEyeblossom(players, dim, x, y, z, state, true)
}

// eyeblossomRipple is the box tryChangingState wakes when a flower turns:
// every eyeblossom still wearing the old face within three blocks across and
// two up or down is scheduled to follow, at a delay drawn from its distance.
// That is what makes a pale garden turn in a wave rather than all at once.
const (
	eyeblossomRippleXZ  = 3
	eyeblossomRippleY   = 2
	eyeblossomDelayLo   = 5  // ticks per block of distance, low end
	eyeblossomDelayHigh = 10 // …and high
)

// switchEyeblossom turns one flower if the hour says so. A flower reached by
// the RANDOM tick is the one that starts a wave: it plays the long sound and
// wakes its neighbours. A flower that was woken plays the short one, and wakes
// its own neighbours in turn — which is how the wave crosses the garden.
func (h *hub) switchEyeblossom(players map[int32]*tracked, dim, x, y, z int, state uint32, spontaneous bool) bool {
	var want uint32
	potted := false
	switch state {
	case openEyeblossom, closedEyeblossom:
		if dim != dimOverworld {
			return true
		}
		if h.nightNow() {
			want = openEyeblossom
		} else {
			want = closedEyeblossom
		}
	case pottedOpenEyeblossom, pottedClosedEyeblossom:
		if dim != dimOverworld {
			return true
		}
		potted = true
		if h.nightNow() {
			want = pottedOpenEyeblossom
		} else {
			want = pottedClosedEyeblossom
		}
	default:
		return false
	}
	if want == state {
		return true
	}
	h.setBlockAt(players, dim, blockPos{x, y, z}, want)
	h.vib(dim, freqBlockChange, x, y, z, 0)
	kind := "close"
	if want == openEyeblossom || want == pottedOpenEyeblossom {
		kind = "open"
	}
	length := "short"
	if spontaneous || potted {
		length = "long"
	}
	h.playSoundDim(players, dim, "minecraft:block.eyeblossom."+kind+"_"+length, sndBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
	if !potted {
		h.wakeEyeblossomsAround(dim, blockPos{x, y, z}, state)
	}
	return true
}

// wakeEyeblossomsAround schedules every neighbour still wearing `old`.
func (h *hub) wakeEyeblossomsAround(dim int, from blockPos, old uint32) {
	w := h.worldFor(dim)
	for dx := -eyeblossomRippleXZ; dx <= eyeblossomRippleXZ; dx++ {
		for dy := -eyeblossomRippleY; dy <= eyeblossomRippleY; dy++ {
			for dz := -eyeblossomRippleXZ; dz <= eyeblossomRippleXZ; dz++ {
				if dx == 0 && dy == 0 && dz == 0 {
					continue
				}
				p := blockPos{from.x + dx, from.y + dy, from.z + dz}
				if w.At(p.x, p.y, p.z) != old {
					continue
				}
				d := math.Sqrt(float64(dx*dx + dy*dy + dz*dz))
				lo, hi := int(d*eyeblossomDelayLo), int(d*eyeblossomDelayHigh)
				delay := lo
				if hi > lo {
					delay += h.rng.Intn(hi - lo + 1)
				}
				h.scheduleIn(dim, p, uint64(max(1, delay)))
			}
		}
	}
}

// nightNow reports whether the sun is down, on the boundary the pale garden
// works to — the eyeblossom and the creaking heart both switch on it, because
// vanilla drives both from the same pair of keyframes.
//
// It is deliberately a clock window and not skyDarken: a thunderstorm at noon
// darkens the sky far enough to spawn monsters, and it still does not open an
// eyeblossom.
func (h *hub) nightNow() bool {
	dt := h.dayTime.Load() % dayLengthTicks
	return dt >= paleNightStart && dt < paleNightEnd
}

// tickNetherPortal breeds zombified piglins out of a standing portal.
func (h *hub) tickNetherPortal(players map[int32]*tracked, dim, x, y, z int, state uint32) bool {
	if netherPortalMax == 0 || state < netherPortalBase || state > netherPortalMax {
		return false
	}
	if dim != 1 || h.rules.Difficulty == diffPeaceful || !h.rules.DoMobSpawning {
		return true
	}
	if h.rng.Intn(portalPiglinOdds) >= h.rules.Difficulty {
		return true
	}
	// Walk to the foot of the portal and stand the piglin on the block below.
	fy := y
	for fy > worldgen.MinY+1 {
		st := h.worldFor(dim).At(x, fy-1, z)
		if st < netherPortalBase || st > netherPortalMax {
			break
		}
		fy--
	}
	if !worldgen.IsSolidFull(h.worldFor(dim).At(x, fy-1, z)) {
		return true
	}
	h.spawnHostileY(players, entityZombifiedPiglin, float64(x)+0.5, float64(fy), float64(z)+0.5)
	return true
}
