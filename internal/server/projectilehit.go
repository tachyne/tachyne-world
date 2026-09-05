package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// What a projectile does to the block it strikes.
//
// Vanilla hangs this on the block as `onProjectileHit`, and ten blocks
// override it. tachyne had the loud ones — TNT, campfires, bells, targets,
// chorus flowers — but each was wired at whatever call site happened to notice
// it, and the rest were simply missing: an arrow through a candle did nothing,
// amethyst never chimed, a decorated pot shrugged off a direct hit.
//
// One dispatch replaces the scattered special cases, so a block gains a
// projectile reaction by adding a branch here rather than by finding the right
// place in the flight loop.

var (
	// Every candle colour, plus the plain one. Vanilla puts the lighting rule
	// on AbstractCandleBlock, which candle CAKES share — so they light too.
	candleRanges     = candleStateRanges()
	pointedDripLo    uint32
	pointedDripHi    uint32
	amethystChimeSet = amethystChimeRanges()
)

type stateRange struct{ lo, hi uint32 }

// candleStateRanges collects the state span of every candle and candle cake.
func candleStateRanges() []stateRange {
	var out []stateRange
	for _, n := range candleBlockNames() {
		if lo, hi, ok := worldgen.BlockRangeOK(n); ok {
			out = append(out, stateRange{lo, hi})
		}
	}
	return out
}

func candleBlockNames() []string {
	names := []string{"candle", "candle_cake"}
	for _, c := range dyeColors {
		names = append(names, c+"_candle", c+"_candle_cake")
	}
	return names
}

// amethystChimeRanges is the set vanilla's AmethystBlock covers: the solid
// block, the budding block, and every bud stage.
func amethystChimeRanges() []stateRange {
	var out []stateRange
	names := append([]string{"amethyst_block", "budding_amethyst"}, amethystChain...)
	for _, n := range names {
		if lo, hi, ok := worldgen.BlockRangeOK(n); ok {
			out = append(out, stateRange{lo, hi})
		}
	}
	return out
}

func init() {
	pointedDripLo, pointedDripHi, _ = worldgen.BlockRangeOK("pointed_dripstone")
}

func inRanges(rs []stateRange, s uint32) bool {
	for _, r := range rs {
		if s >= r.lo && s <= r.hi {
			return true
		}
	}
	return false
}

// projectileHitBlock resolves a projectile striking a block face. Returns
// whether the block consumed the hit in a way that should stop the projectile
// sticking (currently only a block it destroys).
func (h *hub) projectileHitBlock(players map[int32]*tracked, a *arrowEntity, pos blockPos, state uint32) {
	switch {
	case isTarget(state):
		// Targets already had a handler; it lives here now with the rest, and
		// it works in every dimension rather than only the overworld.
		h.hitTarget(players, pos, state, a.x, a.y, a.z, true, a)

	case inRanges(candleRanges, state):
		// Only a BURNING projectile lights a candle, and only an unlit one.
		if a.fire {
			h.lightCandle(players, a.dim, pos, state)
		}

	case inRanges(amethystChimeSet, state):
		// Amethyst rings when struck — pitch varies, which is the whole charm.
		h.playSoundDim(players, a.dim, "minecraft:block.amethyst_block.chime", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5,
			1, 0.5+h.rng.Float32()*1.2)

	case isDecoratedPot(state):
		// A direct hit shatters it. Vanilla cracks it and then destroys it,
		// which drops the pot's contents along with the pot.
		h.breakPotByProjectile(players, a.dim, pos)

	case pointedDripHi > 0 && state >= pointedDripLo && state <= pointedDripHi:
		// Only a THROWN TRIDENT, and only one still travelling, shears
		// dripstone off — an arrow just sticks in it.
		if a.pickupStack.item == itemTrident && projectileSpeed(a) > 0.6 {
			h.breakBlockDrop(players, a.dim, pos, state)
		}
	}
}

// projectileSpeed is how fast the projectile is travelling this tick.
func projectileSpeed(a *arrowEntity) float64 {
	return math.Sqrt(a.vx*a.vx + a.vy*a.vy + a.vz*a.vz)
}

// breakPotByProjectile shatters a decorated pot, spilling what it held.
func (h *hub) breakPotByProjectile(players map[int32]*tracked, dim int, pos blockPos) {
	state := h.worldFor(dim).At(pos.x, pos.y, pos.z)
	h.spillContainer(players, dim, pos.x, pos.y, pos.z, worldgen.Air)
	h.setBlockAt(players, dim, pos, worldgen.Air)
	h.dropLoose(players, dim, pos, state)
	h.playSoundDim(players, dim, "minecraft:block.decorated_pot.shatter", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
}

// breakBlockDrop knocks a block out and drops it.
func (h *hub) breakBlockDrop(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	h.setBlockAt(players, dim, pos, worldgen.Air)
	h.dropLoose(players, dim, pos, state)
}

// lightCandle sets a candle (or candle cake) alight.
func (h *hub) lightCandle(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	info, ok := worldgen.InfoForState(state)
	if !ok || worldgen.GetProperty(info, state, "lit") == "true" {
		return
	}
	lit := worldgen.SetProperty(info, state, "lit", "true")
	if lit == state {
		return
	}
	h.setBlockAt(players, dim, pos, lit)
	h.playSoundDim(players, dim, "minecraft:item.flintandsteel.use", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
}
