package server

import (
	"math"
	"strconv"

	"github.com/tachyne/tachyne-common/protocol"
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

// Freezing (vanilla Entity/LivingEntity): standing in powder snow counts
// up to 140 frozen ticks unless a piece of leather armour is worn; fully
// frozen, a point of freeze damage lands every 40 ticks; out of the snow
// the count thaws two a tick. The client draws the frost from the synced
// TICKS_FROZEN field.
const (
	freezeTicks       = 140
	freezeHurtEvery   = 40
	freezeDamage      = 1
	metaIndexFrozen   = 7   // Entity DATA_TICKS_FROZEN (INT), same on every served version
	hayFallMultiplier = 0.2 // HayBlock.fallOn
)

var (
	slimeMin, slimeMax                 = worldgen.BlockRange("slime_block")
	honeyMin, honeyMax                 = worldgen.BlockRange("honey_block")
	hayMin, hayMax                     = worldgen.BlockRange("hay_block")
	turtleEggMin, turtleEggMax         = worldgen.BlockRange("turtle_egg")
	redstoneOreMin, redstoneOreMax     = worldgen.BlockRange("redstone_ore")
	dsRedstoneOreMin, dsRedstoneOreMax = worldgen.BlockRange("deepslate_redstone_ore")
	leatherArmour                      = map[int32]bool{
		int32(itemByName["leather_helmet"]): true, int32(itemByName["leather_chestplate"]): true,
		int32(itemByName["leather_leggings"]): true, int32(itemByName["leather_boots"]): true,
	}
)

func isHay(s uint32) bool        { return s >= hayMin && s <= hayMax }
func isSlimeBlock(s uint32) bool { return s >= slimeMin && s <= slimeMax }
func isHoneyBlock(s uint32) bool { return s >= honeyMin && s <= honeyMax }
func isTurtleEgg(s uint32) bool  { return s >= turtleEggMin && s <= turtleEggMax }
func isRedstoneOre(s uint32) bool {
	return (s >= redstoneOreMin && s <= redstoneOreMax) || (s >= dsRedstoneOreMin && s <= dsRedstoneOreMax)
}

// canFreeze is vanilla's: any leather armour piece keeps the cold out.
func (t *tracked) canFreeze() bool {
	for _, a := range t.armor {
		if a.count > 0 && leatherArmour[a.item] {
			return false
		}
	}
	return true
}

// frozenMetadata syncs the frost overlay.
func frozenMetadata(eid int32, ticks int) []byte {
	b := protocol.AppendVarInt(nil, eid)
	b = protocol.AppendU8(b, metaIndexFrozen)
	b = protocol.AppendVarInt(b, metaTypeInt)
	b = protocol.AppendVarInt(b, int32(ticks))
	return protocol.AppendU8(b, 0xff)
}

// tickFreezing runs one player's freeze clock.
func (h *hub) tickFreezing(players map[int32]*tracked, t *tracked) {
	w := h.worldFor(t.dim)
	fx, fz, feet := int(math.Floor(t.x)), int(math.Floor(t.z)), int(math.Floor(t.y))
	inSnow := w.At(fx, feet, fz) == powderSnowBlock || w.At(fx, feet+1, fz) == powderSnowBlock
	was := t.frozen
	if inSnow && t.canFreeze() {
		t.frozen = min(freezeTicks, t.frozen+1)
	} else {
		t.frozen = max(0, t.frozen-2)
	}
	if t.frozen != was && (t.frozen%5 == 0 || t.frozen == freezeTicks) {
		t.p.trySendEv(metaEv(frozenMetadata(t.p.eid, t.frozen)))
	}
	if t.frozen >= freezeTicks && h.tick.Load()%freezeHurtEvery == 0 {
		h.hurtBy(players, t, freezeDamage, dtFreeze, deathCause{key: causeFreeze})
	}
}

// crushTurtleEgg breaks one egg of a clutch (TurtleEggBlock.destroyEgg).
func (h *hub) crushTurtleEgg(players map[int32]*tracked, dim, x, y, z int, s uint32) {
	info, ok := worldgen.InfoForState(s)
	if !ok {
		return
	}
	h.playSound(players, "minecraft:block.turtle_egg.break", sndBlock, float64(x)+0.5, float64(y), float64(z)+0.5, 0.7, 0.9+h.rng.Float32()*0.2)
	eggs := 1
	if n, err := strconv.Atoi(worldgen.GetProperty(info, s, "eggs")); err == nil {
		eggs = n
	}
	if eggs <= 1 {
		h.setBlockLive(players, dim, x, y, z, 0)
		return
	}
	h.setBlockLive(players, dim, x, y, z, worldgen.SetProperty(info, s, "eggs", strconv.Itoa(eggs-1)))
}

// fallDamageOn is the fall damage for landing on a block: hay bales and
// honey soften it to a fifth, a bed halves the drop, a slime block
// catches it whole unless the player is sneaking (which suppresses the
// bounce), powder snow catches it entirely.
func fallDamageOn(landed uint32, dist, grace float64, sneaking bool) float64 {
	switch {
	case landed == powderSnowBlock:
		return 0
	case isSlimeBlock(landed) && !sneaking:
		return 0
	case isBedBlock(landed):
		dist *= 0.5
	}
	if dist <= grace {
		return 0
	}
	hurt := dist - grace
	if isHay(landed) || isHoneyBlock(landed) {
		hurt *= hayFallMultiplier
	}
	return math.Floor(hurt)
}

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
		h.tickFreezing(players, t)
		if t.dead {
			continue
		}
		fx, fz, feet := int(math.Floor(t.x)), int(math.Floor(t.z)), int(math.Floor(t.y))
		h.blocksTouching(t.dim, t.x, t.y, t.z, func(s uint32, onFloor bool) {
			switch {
			case onFloor && isTurtleEgg(s): // TurtleEggBlock.stepOn: one in a hundred a tick, not when sneaking
				if !t.p.sneaking && h.rng.Intn(100) == 0 {
					h.crushTurtleEgg(players, t.dim, fx, feet-1, fz, s)
				}
			case onFloor && isRedstoneOre(s) && !boolProp(s, "lit"): // RedStoneOreBlock.stepOn: lights up
				if !t.p.sneaking {
					h.setBlockLive(players, t.dim, fx, feet-1, fz, setBoolProp(s, "lit", true))
				}
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
				h.hurtMobOf(nil, m, magmaDamage, dtHotFloor)
			case berryBushRipe(s):
				// Foxes and bees push through a bush unharmed (vanilla).
				if m.etype == entityFox || m.etype == entityBee {
					return
				}
				h.hurtMobOf(nil, m, berryBushDamage, dtSweetBerryBush)
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
