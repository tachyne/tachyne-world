package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The ravager (Ravager.aiStep / blockedByItem / roar / doHurtTarget): a bite
// stops it for ten ticks; a bite caught on a shield either stuns it for
// forty ticks (half the time — it then roars, damaging and hurling
// everything within four blocks) or hurls the shield-bearer instead; while
// stunned, roaring or biting it cannot move; and a ravager stopped by
// leaves in its path tramples them (mob griefing).

const (
	ravagerAttackTicks = 10 // ATTACK_DURATION
	ravagerStunTicks   = 40 // STUN_DURATION
	ravagerRoarTicks   = 20 // roarTick after a stun; the roar lands at 10
	ravagerRoarReach   = 4.0
	ravagerRoarDamage  = 6
	ravagerStrongPush  = 4.0 // strongKnockback: push(d/d3·4, 0.2, …)
	entityStatusStun   = 39  // Ravager.handleEntityEvent: the stun animation
	entityStatusAttack = 4   // the bite animation
)

// ravagerImmobile is isImmobile.
func (m *mob) ravagerImmobile() bool {
	return m.ravAttackTick > 0 || m.ravStunTick > 0 || m.ravRoarTick > 0
}

// ravagerStep runs each mob update. Returns whether it holds the ravager.
func (h *hub) ravagerStep(players map[int32]*tracked, m *mob) bool {
	for i := 0; i < mobMoveInterval; i++ {
		if m.ravRoarTick > 0 {
			m.ravRoarTick--
			if m.ravRoarTick == 10 {
				h.ravagerRoar(players, m)
			}
		}
		if m.ravAttackTick > 0 {
			m.ravAttackTick--
		}
		if m.ravStunTick > 0 {
			m.ravStunTick--
			if m.ravStunTick == 0 {
				h.playSoundDim(players, m.dim, "minecraft:entity.ravager.roar", sndHostile, m.x, m.y, m.z, 1, 1)
				m.ravRoarTick = ravagerRoarTicks
			}
		}
	}
	if m.ravagerImmobile() {
		m.vx, m.vz = 0, 0
		m.hasTarget = m.hasTarget && m.ravStunTick == 0 && m.ravRoarTick == 0 // hasLineOfSight is false while stunned or roaring
		return true
	}
	// Leaves in the way go (aiStep: horizontalCollision + mobGriefing).
	if h.rules.MobGriefing && (m.vx != 0 || m.vz != 0) && !h.mobStepOK(m, m.x+m.vx, m.z+m.vz) {
		h.ravagerTrample(players, m)
	}
	return false
}

// ravagerTrample breaks every leaves block in the ravager's inflated box.
func (h *hub) ravagerTrample(players map[int32]*tracked, m *mob) {
	w := h.worldFor(m.dim)
	b := m.box()
	half := b.w/2 + 0.2
	minX, maxX := int(math.Floor(m.x-half)), int(math.Floor(m.x+half))
	minZ, maxZ := int(math.Floor(m.z-half)), int(math.Floor(m.z+half))
	minY, maxY := int(math.Floor(m.y)), int(math.Floor(m.y+b.h+0.2))
	for x := minX; x <= maxX; x++ {
		for y := minY; y <= maxY; y++ {
			for z := minZ; z <= maxZ; z++ {
				if s := w.At(x, y, z); worldgen.IsLeaves(s) {
					h.breakBlockDrop(players, m.dim, blockPos{x, y, z}, s)
				}
			}
		}
	}
}

// ravagerBite is doHurtTarget's extras: the ten-tick pause, the animation
// and the sound.
func (h *hub) ravagerBite(players map[int32]*tracked, m *mob) {
	m.ravAttackTick = ravagerAttackTicks
	h.toNearbyEv(players, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusAttack))
	h.playSoundDim(players, m.dim, "minecraft:entity.ravager.attack", sndHostile, m.x, m.y, m.z, 1, 1)
}

// ravagerBlocked is blockedByItem: a bite caught on a shield stuns it half
// the time, else hurls the bearer.
func (h *hub) ravagerBlocked(players map[int32]*tracked, m *mob, t *tracked) {
	if m.ravRoarTick > 0 {
		return
	}
	if h.rng.Float64() < 0.5 {
		m.ravStunTick = ravagerStunTicks
		h.playSoundDim(players, m.dim, "minecraft:entity.ravager.stunned", sndHostile, m.x, m.y, m.z, 1, 1)
		h.toNearbyEv(players, m.dim, m.x, m.z, entityStatus(m.eid, entityStatusStun))
		h.knockback(t, m.x, m.z)
		return
	}
	h.ravagerHurlPlayer(m, t)
}

// ravagerHurlPlayer is strongKnockback on a player.
func (h *hub) ravagerHurlPlayer(m *mob, t *tracked) {
	dx, dz := t.x-m.x, t.z-m.z
	d3 := math.Max(dx*dx+dz*dz, 0.001)
	t.p.trySendEv(attachproto.Velocity{EID: t.p.eid, VX: dx / d3 * ravagerStrongPush, VY: 0.2, VZ: dz / d3 * ravagerStrongPush})
}

// ravagerRoar is roar(): six damage to everything within four that is not
// an illager, and a strong shove for every mob (players are only hurt).
func (h *hub) ravagerRoar(players map[int32]*tracked, m *mob) {
	for _, t := range players {
		if t.dim != m.dim || t.dead || t.gamemode != gmSurvival && t.gamemode != gmAdventure {
			continue
		}
		if dist3(t.x, t.y, t.z, m.x, m.y, m.z) > ravagerRoarReach+1 {
			continue
		}
		h.hurtFrom(players, t, ravagerRoarDamage, dtMobAttack, deathCause{key: causeMob, by: mobDisplayName(m.etype)}, from(m.x, m.z))
	}
	h.grid().nearby(m.dim, m.x, m.z, ravagerRoarReach+1, func(o *mob) {
		if o == m || o.etype == entityRavager || o.dying > 0 || math.Abs(o.y-m.y) > ravagerRoarReach+1 {
			return
		}
		if !isIllager(o.etype) {
			o.lastAttacker = m.eid
			o.hurtKind(ravagerRoarDamage, dtMobAttack)
			if o.health <= 0 {
				h.killMob(players, o)
				return
			}
		}
		dx, dz := o.x-m.x, o.z-m.z
		d3 := math.Max(dx*dx+dz*dz, 0.001)
		o.vx, o.vz, o.kb, o.reroute = dx/d3*ravagerStrongPush*mobMoveInterval, dz/d3*ravagerStrongPush*mobMoveInterval, 3, 0
	})
	h.spawnParticles(players, particlePoof, m.x, m.y+1, m.z, 1.5, 0.1, 20)
}

// isIllager is the AbstractIllager family the roar spares.
func isIllager(etype int) bool {
	return etype == entityPillager || etype == entityVindicator || etype == entityEvoker || etype == entityIllusioner
}
