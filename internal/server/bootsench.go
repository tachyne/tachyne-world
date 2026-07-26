package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// The boots enchantments that change where you can walk: Frost Walker, which
// freezes water under your feet, and Soul Speed, which stops soul sand from
// dragging on you.
//
// Both are LOCATION_CHANGED effects in vanilla — they fire from where the
// player is, not from anything they do — so both hang off the movement path.

const (
	// FROST_WALKER's ReplaceDisk: radius 3 at level 1, +1 per level, clamped.
	frostRadiusBase = 3
	frostRadiusMax  = 16
	// FrostedIceBlock: MAX_AGE 3, the first tick 60-120 ticks after placement
	// and 20-40 between attempts thereafter.
	frostedMaxAge      = 3
	frostedFirstMin    = 60
	frostedFirstSpan   = 61 // 60..120 inclusive
	frostedRetryMin    = 20
	frostedRetrySpan   = 21 // 20..40 inclusive
	frostedNeighbours  = 4  // NEIGHBORS_TO_AGE: fewer than this and it ages anyway
	frostedMeltDivisor = 3  // a 1-in-3 chance to try melting on any tick

	// SOUL_SPEED's MOVEMENT_SPEED effect: 0.0405 at level 1, +0.0105 per level.
	soulSpeedBase   = 0.0405
	soulSpeedPerLvl = 0.0105
	soulSpeedSource = "enchantment:soul_speed"
)

var (
	frostedIceMin, frostedIceMax = worldgen.BlockRange("frosted_ice")
	soulSandBase                 = worldgen.BlockBase("soul_sand")
	soulSoilBase                 = worldgen.BlockBase("soul_soil")
)

// frostWalk freezes the water a player with Frost Walker is standing over.
// Vanilla only does it while ON THE GROUND and not riding anything, and only
// over a water SOURCE with air above it — so you skate across a lake surface
// but cannot pave a waterfall.
func (h *hub) frostWalk(players map[int32]*tracked, t *tracked) {
	lvl := t.armor[3].enchLvl(enchFrostWalker) // boots
	if lvl == 0 || !t.onGround || t.dim != 0 {
		return
	}
	radius := frostRadiusBase + lvl - 1
	if radius > frostRadiusMax {
		radius = frostRadiusMax
	}
	w := h.worldFor(t.dim)
	fy := int(math.Floor(t.y)) - 1 // the ReplaceDisk offset is (0, −1, 0)
	cx, cz := int(math.Floor(t.x)), int(math.Floor(t.z))
	for dx := -radius; dx <= radius; dx++ {
		for dz := -radius; dz <= radius; dz++ {
			if dx*dx+dz*dz > radius*radius {
				continue // a disk, not a square
			}
			x, z := cx+dx, cz+dz
			if w.At(x, fy, z) != worldgen.WaterBase {
				continue // a source only: flowing water is not frozen
			}
			if w.At(x, fy+1, z) != worldgen.Air {
				continue // BlockPredicate.matchesTag(above, AIR)
			}
			h.setBlockAt(players, t.dim, blockPos{x, fy, z}, frostedIceMin)
			// FrostedIceBlock.onPlace schedules the first melt attempt.
			h.scheduleIn(t.dim, blockPos{x, fy, z}, uint64(frostedFirstMin+h.rng.Intn(frostedFirstSpan)))
		}
	}
}

// tickFrostedIce ports FrostedIceBlock.tick: mostly it just ages, and it ages
// faster when it is out on its own. Without this, Frost Walker would pave the
// ocean permanently.
func (h *hub) tickFrostedIce(players map[int32]*tracked, dim int, pos blockPos, state uint32) bool {
	if state < frostedIceMin || state > frostedIceMax {
		return false
	}
	age := int(state - frostedIceMin)
	lonely := h.frostedNeighboursFewerThan(dim, pos, frostedNeighbours)
	// NOTE the light source: ordinary ice melts on BLOCK light alone, but
	// frosted ice uses getMaxLocalRawBrightness outside the End — so daylight
	// DOES thaw it. That difference is the whole reason a Frost Walker path
	// across a sunlit lake closes behind you.
	if (h.rng.Intn(frostedMeltDivisor) == 0 || lonely) &&
		h.frostedBrightness(dim, pos) > 11-age &&
		h.slightlyMelt(players, dim, pos, age) {
		// Melting nudges the neighbours along with it.
		for _, d := range sixDirs {
			n := blockPos{pos.x + d.x, pos.y + d.y, pos.z + d.z}
			ns := h.worldFor(dim).Block(n.x, n.y, n.z)
			if ns >= frostedIceMin && ns <= frostedIceMax {
				h.scheduleIn(dim, n, uint64(frostedRetryMin+h.rng.Intn(frostedRetrySpan)))
			}
		}
		return true
	}
	h.scheduleIn(dim, pos, uint64(frostedRetryMin+h.rng.Intn(frostedRetrySpan)))
	return true
}

// frostedBrightness is FrostedIceBlock's light test: the combined sky+block
// brightness in the overworld and Nether, block light alone in the End.
func (h *hub) frostedBrightness(dim int, pos blockPos) int {
	if dim == 2 { // the End has no sky cycle to melt anything
		return h.blockLight(dim, pos.x, pos.y, pos.z)
	}
	return h.plantBrightness(dim, pos.x, pos.y, pos.z, 0)
}

// slightlyMelt ages the ice one step, or turns it back to water at the last
// one. Reports whether the block actually went.
func (h *hub) slightlyMelt(players map[int32]*tracked, dim int, pos blockPos, age int) bool {
	if age < frostedMaxAge {
		h.setBlockAt(players, dim, pos, frostedIceMin+uint32(age)+1)
		return false
	}
	h.setBlockAt(players, dim, pos, worldgen.WaterBase)
	h.scheduleAroundIn(dim, pos, waterDelay)
	return true
}

// frostedNeighboursFewerThan is FrostedIceBlock.fewerNeigboursThan: ice with
// company lasts, ice on its own goes.
func (h *hub) frostedNeighboursFewerThan(dim int, pos blockPos, want int) bool {
	n := 0
	w := h.worldFor(dim)
	for _, d := range sixDirs {
		s := w.Block(pos.x+d.x, pos.y+d.y, pos.z+d.z)
		if s >= frostedIceMin && s <= frostedIceMax {
			if n++; n >= want {
				return false
			}
		}
	}
	return true
}

// refreshSoulSpeed applies Soul Speed's MOVEMENT_SPEED modifier while the
// player is standing on soul sand or soul soil, and drops it as soon as they
// step off — vanilla re-evaluates the whole LOCATION_CHANGED effect on every
// position change, which comes to the same thing.
func (h *hub) refreshSoulSpeed(t *tracked) {
	in := t.playerAttrs().Get(attr.MovementSpeed)
	lvl := t.armor[3].enchLvl(enchSoulSpeed)
	if lvl == 0 || !t.onGround || !h.onSoulBlock(t) {
		in.RemoveModifier(soulSpeedSource)
		return
	}
	amount := soulSpeedBase + soulSpeedPerLvl*float64(lvl-1)
	in.AddModifier(attr.Modifier{Source: soulSpeedSource, Amount: amount, Op: attr.AddValue})
}

// onSoulBlock reports whether the block underfoot is in #soul_speed_blocks.
func (h *hub) onSoulBlock(t *tracked) bool {
	under := h.worldFor(t.dim).At(int(math.Floor(t.x)), int(math.Floor(t.y))-1, int(math.Floor(t.z)))
	return under == soulSandBase || under == soulSoilBase
}
