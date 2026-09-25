package server

import (
	"math"

	api "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Mob freezing. Players had their frost clock (tickFreezing); mobs stood in
// powder snow for ever and never felt it. Vanilla runs the same clock for
// every living entity: InsideBlockEffectType.FREEZE adds a tick while the
// entity is inside powder snow and can freeze, LivingEntity.aiStep thaws two
// a tick otherwise, and once fully frozen (140) a point of freeze damage
// lands every 40 ticks — five for the Nether mobs in #freeze_hurts_extra_types.
// While frosted, the mob is slowed (tryAddFrost: −0.05 × the frozen
// fraction on MOVEMENT_SPEED), and the synced TICKS_FROZEN field draws the
// frost on it.
//
// Powder snow is also where a burning mob is put out (EXTINGUISH), taking
// the snow with it when mob griefing is on (PowderSnowBlock.entityInside's
// runBefore), and where it is slowed to 0.9 of its step (makeStuckInBlock
// 0.9, 1.5, 0.9 — only once its position is inside the snow, the
// getInBlockState test). The rabbit, the fox, the endermite, the
// silverfish and anything in leather boots walk on top of it instead
// (canEntityWalkOnPowderSnow).

var (
	// #freeze_immune_entity_types; the skeleton's canFreeze is false too
	// (it turns into a stray instead, strayfreeze.go).
	freezeImmuneTypes = entitySet("stray", "polar_bear", "snow_golem", "wither")
	// #freeze_hurts_extra_types: freeze damage lands five times over.
	freezeHurtsExtra = entitySet("strider", "blaze", "magma_cube")
	// #powder_snow_walkable_mobs.
	powderSnowWalkers = entitySet("rabbit", "endermite", "silverfish", "fox")

	itemLeatherBoots      = int32(itemByName["leather_boots"])
	itemLeatherHorseArmor = int32(itemByName["leather_horse_armor"])
)

const (
	freezeExtraMult   = 5.0
	frostSpeedPerFull = -0.05         // LivingEntity.tryAddFrost, in vanilla MOVEMENT_SPEED units
	frostSpeedSource  = "powder_snow" // SPEED_MODIFIER_POWDER_SNOW_ID
	powderSnowStuckXZ = 0.9           // PowderSnowBlock.entityInside: makeStuckInBlock(0.9, 1.5, 0.9)
)

// canFreeze is LivingEntity.canFreeze for a mob: not a freeze-immune kind,
// not a skeleton, not a sulfur cube carrying a block, and nothing from
// #freeze_immune_wearables in an armour slot (leather armour, and leather
// horse armour in the body slot).
func (m *mob) canFreeze() bool {
	if freezeImmuneTypes[m.etype] || m.etype == entitySkeleton || m.hasBody() {
		return false
	}
	for _, g := range m.gear {
		if g.count > 0 && leatherArmour[g.item] {
			return false
		}
	}
	return !(m.armorSt.count > 0 && m.armorSt.item == itemLeatherHorseArmor)
}

// canWalkOnPowderSnow is PowderSnowBlock.canEntityWalkOnPowderSnow.
func canWalkOnPowderSnow(m *mob) bool {
	return powderSnowWalkers[m.etype] || (m.gear[3].count > 0 && m.gear[3].item == itemLeatherBoots)
}

// mobFeetAt is where a walker's feet come to rest in column (x, z) from
// height yHint: the world's floor, or for a mob that walks on powder snow,
// the top of the first powder snow on the way down — and it climbs out of
// snow it is standing inside, the way it climbs out of a placed block.
func (h *hub) mobFeetAt(m *mob, x, z, yHint int) int {
	w := h.worldFor(m.dim)
	y := w.MobFeetFrom(x, z, yHint)
	if !canWalkOnPowderSnow(m) {
		return y
	}
	if isPowderSnow(w.At(x, yHint, z)) {
		top := yHint
		for i := 0; i < 8 && isPowderSnow(w.At(x, top, z)); i++ {
			top++
		}
		return top
	}
	for cy := yHint - 1; cy >= y; cy-- {
		if isPowderSnow(w.At(x, cy, z)) {
			return cy + 1
		}
	}
	return y
}

// mobFreezeStep runs a mob's frost clock over one mob update
// (mobMoveInterval ticks).
func (h *hub) mobFreezeStep(players map[int32]*tracked, m *mob) {
	in := h.mobInPowderSnow(m)
	if in && (m.fireSecs > 0 || m.burning) {
		h.mobPutOutInSnow(players, m)
	}
	was := m.ticksFrozen
	if in && m.canFreeze() {
		m.ticksFrozen = min(freezeTicks, m.ticksFrozen+mobMoveInterval)
	} else {
		m.ticksFrozen = max(0, m.ticksFrozen-2*mobMoveInterval)
	}
	if m.ticksFrozen != was && (m.ticksFrozen/5 != was/5 || m.ticksFrozen == 0 || m.ticksFrozen == freezeTicks) {
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(frozenMetadata(m.eid, m.ticksFrozen)))
	}
	h.mobFrostSpeed(m)
	// tickCount % 40 == 0, for an update that covers mobMoveInterval ticks.
	if m.ticksFrozen >= freezeTicks && m.canFreeze() && h.rules.FreezeDamage &&
		h.tick.Load()%freezeHurtEvery < mobMoveInterval {
		dmg := float64(freezeDamage)
		if freezeHurtsExtra[m.etype] {
			dmg *= freezeExtraMult
		}
		h.hurtMobOf(players, m, dmg, dtFreeze)
	}
}

// mobFrostSpeed is removeFrost + tryAddFrost: the slow follows the frozen
// fraction, and only while the mob stands on something.
func (h *hub) mobFrostSpeed(m *mob) {
	if m.attrs == nil && m.ticksFrozen <= 0 {
		return // never frosted: leave a mob with no attribute map without one
	}
	in := m.mobAttrs().Get(api.MovementSpeed)
	in.RemoveModifier(frostSpeedSource)
	if m.ticksFrozen <= 0 {
		return
	}
	w := h.worldFor(m.dim)
	if w.At(floorInt(m.x), floorInt(m.y-0.5), floorInt(m.z)) == 0 { // getBlockStateOnLegacy is air
		return
	}
	pct := math.Min(float64(m.ticksFrozen), freezeTicks) / freezeTicks
	in.AddModifier(api.Modifier{Source: frostSpeedSource, Amount: frostSpeedPerFull * pct * attrToStep, Op: api.AddValue})
}

// mobPutOutInSnow is the EXTINGUISH a burning mob gets in powder snow, and
// the runBefore that melts the snow it is in when mob griefing is on.
func (h *hub) mobPutOutInSnow(players map[int32]*tracked, m *mob) {
	if h.rules.MobGriefing {
		w := h.worldFor(m.dim)
		fx, fy, fz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
		for _, y := range []int{fy, fy + 1} {
			if s := w.At(fx, y, fz); isPowderSnow(s) {
				h.meltPowderSnow(players, m.dim, blockPos{fx, y, fz}, s)
				break
			}
		}
	}
	m.fireSecs = 0
	if m.burning {
		m.burning = false
		h.toTracking(players, m.eid, m.dim, m.x, m.z, metaEv(fireMetadata(m.eid, false)))
	}
}

// meltPowderSnow is Level.destroyBlock(pos, false) on powder snow: it
// breaks, with no drop.
func (h *hub) meltPowderSnow(players map[int32]*tracked, dim int, pos blockPos, s uint32) {
	h.toNearbyEv(players, dim, float64(pos.x), float64(pos.z), blockBreakEvent(pos.x, pos.y, pos.z, s))
	h.setBlockAt(players, dim, pos, 0)
	h.vib(dim, freqBlockDestroy, pos.x, pos.y, pos.z, 0)
}
