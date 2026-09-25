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
	case isBell(state):
		// BellBlock.onProjectileHit → onHit(requireHitFromCorrectSide): only
		// a hit on the bell's ringing side, below its top, rings it; a player
		// who shot it is credited with the ring (Stats.BELL_RING).
		face, hitY := projectileEntry(a, pos)
		if face >= 0 && bellProperHit(state, face, float32(hitY)) && h.ringBell(players, a.dim, pos, face) {
			if t := players[a.shooter]; t != nil {
				h.incCustom(t, "bell_ring", 1)
			}
		}
	case isTarget(state):
		// Targets already had a handler; it lives here now with the rest, and
		// it works in every dimension rather than only the overworld.
		h.hitTarget(players, a.dim, pos, state, a.x, a.y, a.z, true, a)

	case inRanges(candleRanges, state):
		// Only a BURNING projectile lights a candle, and only an unlit one.
		if a.fire {
			h.lightCandle(players, a.dim, pos, state)
		}
	case isButton(state): // ButtonBlock.entityInside: an arrow presses a wooden button
		if _, _, _, wooden := buttonKind(state); wooden {
			h.inDim(a.dim, func() { h.pressButton(players, pos, state) })
		}
	case isTNT(state): // TntBlock.onProjectileHit: a burning projectile primes it
		if a.fire {
			h.primeTNTBy(players, a.dim, pos.x, pos.y, pos.z, 80, a.shooter) // the shooter owns it
		}
	case isCampfireBlock(state): // CampfireBlock.onProjectileHit: a burning projectile lights it
		if a.fire {
			h.lightBlock(players, a.dim, pos, state, "")
		}

	case inRanges(amethystChimeSet, state):
		// Amethyst rings when struck — pitch varies, which is the whole charm.
		h.playSoundDim(players, a.dim, "minecraft:block.amethyst_block.chime", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5,
			1, 0.5+h.rng.Float32()*1.2)

	case isLightningRodState(state):
		// A Channeling trident on a rod in a storm calls the bolt down on it.
		h.channelingStrike(players, a, a.dim, float64(pos.x)+0.5, float64(pos.y), float64(pos.z)+0.5, nil)

	case isBigDripleaf(state):
		// BigDripleafBlock.onProjectileHit: tips all the way at once.
		h.dripleafShot(players, a.dim, pos, state)

	case isDecoratedPot(state) && h.projectileMayBreak(players, a):
		// A direct hit shatters it. Vanilla cracks it and then destroys it,
		// which drops the pot's contents along with the pot — and only as
		// DecoratedPotBlock.onProjectileHit allows: an impact projectile, the
		// projectiles_can_break_blocks rule, and a shooter who may interact
		// (not an adventure player; a mob only with mob griefing).
		h.breakPotByProjectile(players, a.dim, pos)

	case isChorusFlower(state) && h.projectileMayBreak(players, a):
		// ChorusFlowerBlock.onProjectileHit: destroyBlock with the projectile
		// as the breaker, which is what satisfies the loot table's "this"
		// entity condition — so a shot flower drops itself.
		h.toNearbyEv(players, a.dim, float64(pos.x), float64(pos.z), blockBreakEvent(pos.x, pos.y, pos.z, state))
		h.setBlockAt(players, a.dim, pos, worldgen.Air)
		if h.rules.DoTileDrops {
			h.spawnBlockDrop(players, a.dim, itemChorusFlower, 1, pos.x, pos.y, pos.z)
		}

	case pointedDripHi > 0 && state >= pointedDripLo && state <= pointedDripHi:
		// Only a THROWN TRIDENT, and only one still travelling, shears
		// dripstone off — an arrow just sticks in it.
		if a.pickupStack.item == itemTrident && projectileSpeed(a) > 0.6 && h.rules.ProjectilesBreak {
			h.breakBlockDrop(players, a.dim, pos, state)
		}
	}
}

// impactProjectiles is #impact_projectiles: the projectiles Projectile.mayBreak
// lets break a block at all.
var impactProjectiles = func() map[int]bool {
	out := map[int]bool{}
	for _, n := range []string{"arrow", "spectral_arrow", "firework_rocket", "snowball",
		"fireball", "small_fireball", "egg", "trident", "dragon_fireball", "wither_skull",
		"wind_charge", "breeze_wind_charge"} {
		out[entityID(n)] = true
	}
	return out
}()

var itemChorusFlower = int32(itemByName["chorus_flower"])

// projectileMayBreak is Projectile.mayInteract && mayBreak: an impact
// projectile, projectiles_can_break_blocks on, and an owner allowed to change
// the world — a player who is not in adventure mode (there is no can_break
// component here to let one through), a mob only under mobGriefing. A
// projectile with no owner, or one whose owner is gone, passes.
func (h *hub) projectileMayBreak(players map[int32]*tracked, a *arrowEntity) bool {
	if !impactProjectiles[a.etype] || !h.rules.ProjectilesBreak {
		return false
	}
	if t := players[a.shooter]; t != nil {
		return t.gamemode != gmAdventure
	}
	if a.shooter != 0 && h.mobs[a.shooter] != nil {
		return h.rules.MobGriefing
	}
	return true
}

// projectileEntry is the face a projectile flying along its velocity from
// where it is enters the block at pos through (0 down … 5 east), and the
// height within the block where it strikes — the BlockHitResult's direction
// and location. face is -1 for a projectile that is not moving.
func projectileEntry(a *arrowEntity, pos blockPos) (face int32, hitY float64) {
	from := [3]float64{a.x, a.y, a.z}
	d := [3]float64{a.vx, a.vy, a.vz}
	lo := [3]float64{float64(pos.x), float64(pos.y), float64(pos.z)}
	axis, best := -1, math.Inf(-1)
	for i := 0; i < 3; i++ {
		if d[i] == 0 {
			continue
		}
		plane := lo[i]
		if d[i] < 0 {
			plane++
		}
		if t := (plane - from[i]) / d[i]; t > best {
			axis, best = i, t
		}
	}
	if axis < 0 {
		return -1, 0
	}
	hitY = from[1] + d[1]*max(best, 0) - lo[1]
	switch axis {
	case 0:
		face = 5 // moving west, it enters through the east face
		if d[0] > 0 {
			face = 4
		}
	case 1:
		face = 1
		if d[1] > 0 {
			face = 0
		}
	default:
		face = 3
		if d[2] > 0 {
			face = 2
		}
	}
	return face, hitY
}

// projectileSpeed is how fast the projectile is travelling this tick.
func projectileSpeed(a *arrowEntity) float64 {
	return math.Sqrt(a.vx*a.vx + a.vy*a.vy + a.vz*a.vz)
}

// breakPotByProjectile shatters a decorated pot, spilling what it held.
func (h *hub) breakPotByProjectile(players map[int32]*tracked, dim int, pos blockPos) {
	sh, _ := h.potSherds.get(dim, pos.x, pos.y, pos.z)
	h.spillContainer(players, dim, pos.x, pos.y, pos.z, h.worldFor(dim).At(pos.x, pos.y, pos.z), worldgen.Air)
	h.setBlockAt(players, dim, pos, worldgen.Air)
	h.dropPotShards(players, dim, pos, sh) // cracked: the loot table's dynamic sherds entry
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
	h.lightBlock(players, dim, pos, state, sndFlintSteelUse)
}
