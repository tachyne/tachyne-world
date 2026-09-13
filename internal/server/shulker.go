package server

import (
	"math"

	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Shulkers (ShulkerPeekGoal, ShulkerAttackGoal, teleportSomewhere,
// hitByShulkerBullet, the covered armour): a shulker with nothing to shoot
// peeks open a little now and then; with a target within twenty it opens
// fully and fires a bullet every one to five and a half seconds; closed,
// it wears twenty points of armour and arrows glance off; hurt below half
// health it teleports one time in four, up to eight blocks off onto a
// floor; and one of its own bullets landing on it teleports it too —
// leaving, if there are few shulkers about, a new one where it stood.

const (
	metaIndexShulkerPeek = 17 // DATA_PEEK_ID (byte); Shulker is not ageable
	shulkerPeekIdle      = 30
	shulkerPeekOpen      = 100
	shulkerIdleOdds      = 40 // ShulkerPeekGoal: nextInt(reducedTickDelay(40))
	shulkerAttackRangeSq = 400.0
	shulkerCoveredArmor  = 20.0 // COVERED_ARMOR_MODIFIER
	shulkerCoveredSource = "covered"
	shulkerTeleportTries = 5
	shulkerTeleportReach = 8
)

func shulkerPeekMeta(m *mob) []byte {
	b := protocol.AppendVarInt(nil, m.eid)
	b = protocol.AppendU8(b, metaIndexShulkerPeek)
	b = protocol.AppendVarInt(b, metaTypeByteFox)
	b = protocol.AppendU8(b, byte(m.shPeek))
	return protocol.AppendU8(b, itemMetaEnd)
}

// setShulkerPeek is setRawPeekAmount: the shell, and the armour it wears
// while closed.
func (h *hub) setShulkerPeek(players map[int32]*tracked, m *mob, peek int8) {
	if m.shPeek == peek && (peek != 0 || m.mobAttrs().Get(attr.Armor).HasModifier(shulkerCoveredSource)) {
		return
	}
	m.shPeek = peek
	in := m.mobAttrs().Get(attr.Armor)
	if peek == 0 {
		in.RemoveModifier(shulkerCoveredSource)
		in.AddModifier(attr.Modifier{Source: shulkerCoveredSource, Amount: shulkerCoveredArmor, Op: attr.AddValue})
	} else {
		in.RemoveModifier(shulkerCoveredSource)
	}
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(shulkerPeekMeta(m)))
}

func (m *mob) shulkerClosed() bool { return m.shPeek == 0 }

// shulkerTick runs each mob update from the hostile switch.
func (h *hub) shulkerTick(players map[int32]*tracked, m *mob) {
	if m.shHurt { // hurtServer: below half health, one in four teleports
		m.shHurt = false
		if float64(m.health) < m.mobAttrs().Value(attr.MaxHealth)*0.5 && h.rng.Intn(4) == 0 {
			h.shulkerTeleport(players, m)
			return
		}
	}
	if !m.shArmored { // the shell starts closed
		m.shArmored = true
		h.setShulkerPeek(players, m, 0)
	}
	if m.shAttack > 0 {
		m.shAttack -= mobMoveInterval
	}
	t := h.nearestHuntable(players, m.dim, m.x, m.z, 20)
	if t != nil && h.rules.Difficulty != diffPeaceful {
		// ShulkerAttackGoal: open wide, a bullet every 20 + nextInt(10)·10 ticks.
		m.shPeekTicks = 0
		h.setShulkerPeek(players, m, shulkerPeekOpen)
		if m.shAttack <= 0 {
			m.shAttack = 20 + h.rng.Intn(10)*10
			h.shulkerFire(players, m, t)
		}
		return
	}
	// ShulkerPeekGoal: a glimpse out now and then.
	if m.shPeekTicks > 0 {
		m.shPeekTicks -= mobMoveInterval
		if m.shPeekTicks <= 0 {
			m.shPeekTicks = 0
			h.setShulkerPeek(players, m, 0)
		}
		return
	}
	if m.shPeek != 0 {
		h.setShulkerPeek(players, m, 0) // the target left: stop() closes it
	}
	if h.rng.Intn(shulkerIdleOdds/mobMoveInterval) == 0 {
		m.shPeekTicks = 20 * (1 + h.rng.Intn(3))
		h.setShulkerPeek(players, m, shulkerPeekIdle)
	}
}

// shulkerFire is the bullet: homing, Levitation on a hit.
func (h *hub) shulkerFire(players map[int32]*tracked, m *mob, t *tracked) {
	ux, uy, uz := aimAt(m.x, m.y+0.5, m.z, t.x, t.y+1, t.z)
	v := shulkerBulletSpeed
	a := h.launchProjectileIn(players, entityShulkerBullet, m.dim, m.x, m.y+0.5, m.z, ux*v, uy*v, uz*v)
	a.shooter, a.dmg, a.breaks = m.eid, 4, true
	a.homing, a.levitate = t.p.eid, 10
	h.playSoundDim(players, m.dim, "minecraft:entity.shulker.shoot", sndHostile, m.x, m.y, m.z, 2, (h.rng.Float32()-h.rng.Float32())*0.2+1)
}

// shulkerTeleport is teleportSomewhere: five tries within eight blocks at
// an empty cell over a solid floor (it attaches downward).
func (h *hub) shulkerTeleport(players map[int32]*tracked, m *mob) bool {
	w := h.worldFor(m.dim)
	bx, by, bz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	for i := 0; i < shulkerTeleportTries; i++ {
		x := bx + h.rng.Intn(2*shulkerTeleportReach+1) - shulkerTeleportReach
		y := by + h.rng.Intn(2*shulkerTeleportReach+1) - shulkerTeleportReach
		z := bz + h.rng.Intn(2*shulkerTeleportReach+1) - shulkerTeleportReach
		if y <= worldgen.MinY || w.At(x, y, z) != worldgen.Air || !worldgen.Collides(w.At(x, y-1, z)) {
			continue
		}
		h.playSoundDim(players, m.dim, "minecraft:entity.shulker.teleport", sndHostile, m.x, m.y, m.z, 1, 1)
		m.x, m.y, m.z = float64(x)+0.5, float64(y), float64(z)+0.5
		m.sx, m.sy, m.sz = m.x, m.y, m.z
		h.toNearbyEv(players, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, true))
		h.setShulkerPeek(players, m, 0)
		m.hasTarget, m.shPeekTicks = false, 0
		return true
	}
	return false
}

// shulkerBulletHit is hitByShulkerBullet: an open shulker struck by a
// bullet teleports and, with (n−1)/5 odds against it, leaves a new
// shulker of its colour where it stood.
func (h *hub) shulkerBulletHit(players map[int32]*tracked, m *mob) {
	ox, oy, oz := m.x, m.y, m.z
	if m.shulkerClosed() || !h.shulkerTeleport(players, m) {
		return
	}
	n := 0
	h.grid().nearby(m.dim, ox, oz, 8, func(o *mob) {
		if o.etype == entityShulker && o.dying == 0 && math.Abs(o.y-oy) <= 8 {
			n++
		}
	})
	if h.rng.Float64() < float64(n-1)/5 {
		return
	}
	if nm := h.spawnSpecies(players, entityShulker, m.dim, ox, oy, oz); nm != nil {
		nm.variant, nm.variantSet = m.variant, m.variantSet
	}
}
