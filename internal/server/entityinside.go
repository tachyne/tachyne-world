package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Blocks that do something to whatever is standing in or on them — vanilla's
// entityInside and stepOn hooks. Cactus and fire already had their own paths;
// these are the ones that did nothing at all, so a magma block was a warm
// floor, a berry bush was decoration and a wither rose was a flower.
//
// Applies to MOBS as well as players, which is what makes a berry-bush hedge
// or a magma floor an actual defence rather than scenery.

const (
	magmaDamage      = 1.0 // MagmaBlock.stepOn: hot_floor, 1 HP
	berryBushDamage  = 1.0 // SweetBerryBushBlock: 1 HP while moving through
	witherRoseSecs   = 2   // WitherRoseBlock: Wither for 2 s
	berryMoveEpsilon = 0.003
)

var (
	magmaBlockState              = worldgen.BlockBase("magma_block")
	berryBushMin, berryBushMax   = worldgen.BlockRange("sweet_berry_bush")
	witherRoseMin, witherRoseMax = worldgen.BlockRange("wither_rose")
	cobwebState                  = worldgen.BlockBase("cobweb")
)

func isBerryBush(s uint32) bool  { return s >= berryBushMin && s <= berryBushMax }
func isWitherRose(s uint32) bool { return s >= witherRoseMin && s <= witherRoseMax }

// berryBushRipe reports whether a bush has grown enough to scratch. Vanilla
// only hurts at age > 0, so a freshly planted one is safe.
func berryBushRipe(s uint32) bool { return isBerryBush(s) && s > berryBushMin }

// entityInsideTick runs the contact effects for every player and mob. Called
// from the 1 Hz survival step, alongside the other environmental damage.
func (h *hub) entityInsideTick(players map[int32]*tracked) {
	for _, t := range players {
		if t.gamemode != gmSurvival || t.dead {
			continue
		}
		// Remember where they were BEFORE overwriting it, or the comparison
		// below is the position against itself and a bush never scratches.
		movedX := math.Abs(t.x - t.contactX)
		movedZ := math.Abs(t.z - t.contactZ)
		t.contactX, t.contactZ = t.x, t.z
		h.blocksTouching(t.dim, t.x, t.y, t.z, func(s uint32, onFloor bool) {
			switch {
			case onFloor && s == magmaBlockState:
				// Fire Resistance and Frost Walker boots spare you. Vanilla
				// ALSO spares a crouching player (isSteppingCarefully), which
				// this cannot honour: the movement packet carries sprinting but
				// not sneaking, so the server never learns you are crouched.
				if t.hasEffect(effFireRes) > 0 || t.armor[3].enchLvl(enchFrostWalker) > 0 {
					return
				}
				h.hurtBy(players, t, magmaDamage, dtHotFloor, deathCause{key: causeFire})
			case berryBushRipe(s):
				// Vanilla only scratches you while you are MOVING through the
				// bush: standing still in one is safe.
				if movedX >= berryMoveEpsilon || movedZ >= berryMoveEpsilon {
					h.hurtBy(players, t, berryBushDamage, dtSweetBerryBush, deathCause{key: causeSweetBerry})
				}
			case isWitherRose(s):
				if h.rules.Difficulty != diffPeaceful {
					h.applyEffect(players, t, effWither, 0, witherRoseSecs)
				}
			}
		})
	}
	for _, m := range h.mobs {
		if m.dying > 0 {
			continue
		}
		h.blocksTouching(m.dim, m.x, m.y, m.z, func(s uint32, onFloor bool) {
			switch {
			case onFloor && s == magmaBlockState:
				if m.resistsFire() || magmaImmune[m.etype] {
					return
				}
				h.hurtMob(nil, m, magmaDamage)
			case berryBushRipe(s):
				// Foxes and bees push through a bush unharmed (vanilla).
				if m.etype == entityFox || m.etype == entityBee {
					return
				}
				h.hurtMob(nil, m, berryBushDamage)
			case isWitherRose(s):
				// The undead are immune to wither, so a rose does not touch them.
				if h.rules.Difficulty != diffPeaceful && !ignoresPoisonAndRegen(m.etype) {
					h.applyMobEffect(h.playersRef, m, effWither, 0, witherRoseSecs)
				}
			}
		})
	}
}

// magmaImmune are the mobs that walk on magma unbothered — the ones that live
// in the Nether where the stuff is a floor.
var magmaImmune = map[int]bool{}

func init() {
	for _, e := range []int{entityZombifiedPiglin, entityMagmaCube, entityStrider,
		entityBlaze, entityWitherSkeleton, entityGhast, entityZoglin, entityWither} {
		magmaImmune[e] = true
	}
}

// blocksTouching calls fn for the block at the feet, the one at body height,
// and the one underfoot — the three an entity can be "inside" or standing on.
// onFloor marks the last of those, which is what stepOn effects need.
func (h *hub) blocksTouching(dim int, x, y, z float64, fn func(state uint32, onFloor bool)) {
	w := h.worldFor(dim)
	fx, fz := int(math.Floor(x)), int(math.Floor(z))
	feet := int(math.Floor(y))
	fn(w.At(fx, feet, fz), false)
	fn(w.At(fx, feet+1, fz), false)
	fn(w.At(fx, feet-1, fz), true)
}

// cobwebSlow reports whether an entity is caught in a cobweb. Movement itself
// is the client's business — this exists so the movement authority does not
// mistake the crawl for a stall, and so mobs in a web stop chasing.
func (h *hub) cobwebSlow(dim int, x, y, z float64) bool {
	w := h.worldFor(dim)
	fx, fz := int(math.Floor(x)), int(math.Floor(z))
	feet := int(math.Floor(y))
	return w.At(fx, feet, fz) == cobwebState || w.At(fx, feet+1, fz) == cobwebState
}
