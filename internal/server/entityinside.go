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
		fellY := t.y - t.contactY
		t.contactX, t.contactY, t.contactZ = t.x, t.y, t.z
		h.tickFreezing(players, t)
		h.honeySlide(players, t, fellY)
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
			case isBigDripleaf(s) && t.onGround: // BigDripleafBlock.entityInside: a load starts it tipping
				y := feet
				if onFloor {
					y = feet - 1
				}
				h.dripleafStepped(players, t.dim, blockPos{fx, y, fz}, s)
			case !onFloor && isFilledCauldron(s) && t.fireSecs > 0:
				// LayeredCauldronBlock.entityInside: a burning entity in the
				// water (or powder snow) is put out, and the cauldron loses a level.
				t.fireSecs = 0
				h.lowerCauldron(players, t.dim, cellWith(h, t.dim, fx, feet, fz, s), s)
			case !onFloor && s == lavaCauldronState:
				// LavaCauldronBlock.entityInside: lavaIgnite + lavaHurt.
				if t.hasEffect(effFireRes) == 0 {
					h.setBurning(players, t, lavaFireSecs)
					h.hurtBy(players, t, lavaDamagePerSec, dtLava, deathCause{key: causeLava})
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
			case isBigDripleaf(s) && m.grounded():
				y := int(math.Floor(m.y))
				if onFloor {
					y--
				}
				h.dripleafStepped(players, m.dim, blockPos{int(math.Floor(m.x)), y, int(math.Floor(m.z))}, s)
			case !onFloor && isFilledCauldron(s) && m.fireSecs > 0:
				m.fireSecs = 0
				h.lowerCauldron(players, m.dim, cellWith(h, m.dim, int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z)), s), s)
			case !onFloor && s == lavaCauldronState:
				if !m.resistsFire() {
					m.ignite(lavaFireSecs)
					h.hurtMobOf(nil, m, lavaDmgPerSec, dtLava)
				}
			case !onFloor && m.etype == entityRavager && isCropState(s) && h.rules.MobGriefing:
				// CropBlock.entityInside: a ravager tramples crops flat.
				h.breakBlockDrop(players, m.dim, cellWith(h, m.dim, int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z)), s), s)
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

// Honey block sliding (vanilla HoneyBlock.entityInside / isSlidingDown): a
// player falling past 0.08 a tick while pressed against a honey block's
// side — off the ground, below its top, and outside its inset face — is
// sliding: the fall resets every tick (no damage at the bottom), the slide
// sound plays now and then, and "Sticky Situation" is checked each second.
const (
	honeySlideMinFall = 0.08   // MIN_FALL_SPEED_TO_BE_CONSIDERED_SLIDING
	honeyFaceInset    = 0.4375 // the block is a 14/16 column: the face sits 1/16 in
	playerHalfWidth   = 0.3
)

func (h *hub) honeySlide(players map[int32]*tracked, t *tracked, fellY float64) {
	if t.p.onGround || fellY >= -honeySlideMinFall {
		return
	}
	w := h.worldFor(t.dim)
	if w == nil {
		return
	}
	fx, fz, feet := int(math.Floor(t.x)), int(math.Floor(t.z)), int(math.Floor(t.y))
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if dx == 0 && dz == 0 {
				continue
			}
			bx, bz := fx+dx, fz+dz
			ox, oz := math.Abs(float64(bx)+0.5-t.x), math.Abs(float64(bz)+0.5-t.z)
			if ox >= 0.5+playerHalfWidth || oz >= 0.5+playerHalfWidth {
				continue // the player's box does not reach into that cell
			}
			if ox+1e-7 <= honeyFaceInset+playerHalfWidth && oz+1e-7 <= honeyFaceInset+playerHalfWidth {
				continue // inside the face, not against it
			}
			for _, by := range []int{feet, feet + 1} {
				s := w.At(bx, by, bz)
				if !isHoneyBlock(s) || t.y > float64(by)+0.9375-1e-7 {
					continue
				}
				t.peakY = t.y // Entity.resetFallDistance: the slide costs nothing at the bottom
				if h.rng.Intn(5) == 0 {
					h.playSoundDim(players, t.dim, "minecraft:block.honey_block.slide", sndBlock, t.x, t.y, t.z, 1, 1)
				}
				if h.tick.Load()%20 == 0 {
					h.advance(players, t, "slide_down_block", advMatch{blockState: s})
				}
				return
			}
		}
	}
}

// isFilledCauldron reports a water or powder-snow cauldron with anything in it.
func isFilledCauldron(s uint32) bool {
	kind, level, ok := cauldronOf(s)
	return ok && level > 0 && (kind == cauldronWater || kind == cauldronSnow)
}

// lowerCauldron is LayeredCauldronBlock.lowerFillLevel: one level down,
// empty at zero.
func (h *hub) lowerCauldron(players map[int32]*tracked, dim int, pos blockPos, s uint32) {
	_, level, ok := cauldronOf(s)
	if !ok || level == 0 {
		return
	}
	if level == 1 {
		h.setBlockAt(players, dim, pos, cauldronState)
		return
	}
	h.setBlockAt(players, dim, pos, s-1)
}

// isCropState reports a staged crop (wheat, carrots, potatoes, beetroots).
func isCropState(s uint32) bool {
	for _, r := range cropRanges {
		if s >= r[0] && s <= r[1] {
			return true
		}
	}
	return false
}

// cellWith is the feet or body cell holding state s — blocksTouching reports
// the state without saying which of the two it came from.
func cellWith(h *hub, dim, fx, feet, fz int, s uint32) blockPos {
	if h.worldFor(dim).At(fx, feet, fz) == s {
		return blockPos{fx, feet, fz}
	}
	return blockPos{fx, feet + 1, fz}
}
