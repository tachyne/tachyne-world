package server

import (
	"math"

	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// Axolotls (Axolotl.hurtServer + AxolotlAi PLAY_DEAD / FIGHT activities +
// applySupportingEffects): hurt under water, one time in three (more when
// the blow was big or it is already below half) an axolotl plays dead for
// two hundred ticks, regenerating and untouchable by its foes' attention;
// it hunts fish, squid and tadpoles within eight blocks — resting for two
// minutes after a hunt — and always fights drowned and guardians; and when
// its target dies at a player's hand within twenty blocks, that player is
// given regeneration and relieved of mining fatigue.

const (
	metaIndexAxolotlDead = 18 // DATA_PLAYING_DEAD (1.21.5; the ageable shift applies on 26.2)
	axPlayDeadTicks      = 200
	axHuntCooldown       = 2400
	axDetectSq           = 64.0 // AxolotlAttackablesSensor: within 8
	axBiteTicks          = 20   // MeleeAttack(20)
	axChaseSpeed         = 0.6  // getSpeedModifierChasing in water
	axSupportRange       = 20.0
	axRegenCap           = 2400 // applySupportingEffects: up to 2400 ticks
	axRegenAdd           = 100
)

func axolotlHuntTarget(etype int) bool { // #axolotl_hunt_targets
	switch etype {
	case entityTropicalFish, entityPufferfish, entitySalmon, entityCod, entitySquid, entityGlowSquid, entityTadpole:
		return true
	}
	return false
}

func axolotlAlwaysHostile(etype int) bool { // #axolotl_always_hostiles
	return etype == entityDrowned || etype == entityGuardian || etype == entityElderGuardian
}

func axolotlDeadMeta(m *mob) []byte { return boolMeta(m.eid, metaIndexAxolotlDead, m.axDead > 0) }

// axolotlStep runs each mob update. Returns whether it holds the axolotl.
func (h *hub) axolotlStep(players map[int32]*tracked, m *mob) bool {
	now := h.tick.Load()
	// hurtServer's roll, from the hit the mob recorded.
	if m.axHurt {
		m.axHurt = false
		dmg := m.axHurtDmg
		if m.axDead == 0 && h.rng.Intn(3) == 0 && (float64(h.rng.Intn(3)) < dmg || float64(m.health)/m.mobAttrs().Value(attr.MaxHealth) < 0.5) &&
			dmg < float64(m.health) && h.inWater(m.dim, m.x, m.y, m.z) {
			m.axDead = axPlayDeadTicks
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(axolotlDeadMeta(m)))
			h.applyMobEffect(players, m, effRegen, 0, axPlayDeadTicks/20) // PlayDead.start
			m.axTarget = 0
		}
	}
	if m.axDead > 0 {
		m.axDead -= mobMoveInterval
		if m.axDead <= 0 || !h.inWater(m.dim, m.x, m.y, m.z) { // ValidatePlayDead: out of water it stops
			m.axDead = 0
			h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(axolotlDeadMeta(m)))
			return false
		}
		m.vx, m.vy, m.vz = 0, 0, 0
		return true
	}
	if m.axBiteCD > 0 {
		m.axBiteCD -= mobMoveInterval
	}
	if m.loveTicks > 0 || m.tempted {
		return false
	}
	// The target it has, or the nearest attackable within eight.
	var target *mob
	if o := h.mobs[m.axTarget]; o != nil && o.dim == m.dim && dist3sq(o.x, o.y, o.z, m.x, m.y, m.z) <= axDetectSq*2 {
		target = o
		if o.dying > 0 || o.health <= 0 { // the hunt is over
			if t := players[o.lastAttacker]; t != nil && dist3(t.x, t.y, t.z, m.x, m.y, m.z) <= axSupportRange {
				h.axolotlSupport(players, t) // onStopAttacking: a player finished it
			}
			if axolotlHuntTarget(o.etype) {
				m.axHuntCD = now + axHuntCooldown
			}
			m.axTarget, target = 0, nil
		}
	}
	if target == nil {
		m.axTarget = 0
		bestD := axDetectSq
		h.grid().nearby(m.dim, m.x, m.z, 8, func(o *mob) {
			if o == m || o.dying > 0 || !h.inWater(o.dim, o.x, o.y, o.z) {
				return
			}
			if !axolotlAlwaysHostile(o.etype) && !(axolotlHuntTarget(o.etype) && now >= m.axHuntCD) {
				return
			}
			if d := dist3sq(o.x, o.y, o.z, m.x, m.y, m.z); d <= bestD {
				target, bestD = o, d
			}
		})
		if target != nil {
			m.axTarget = target.eid
		}
	}
	if target == nil {
		return false
	}
	dx, dy, dz := target.x-m.x, target.y-m.y, target.z-m.z
	if d := math.Sqrt(dx*dx + dy*dy + dz*dz); d > 1.5 {
		sp := m.moveSpeed() * axChaseSpeed
		m.vx, m.vy, m.vz = dx/d*sp, dy/d*sp*0.5, dz/d*sp
		m.rest = 0
		return true
	}
	m.vx, m.vz = 0, 0
	if m.axBiteCD <= 0 {
		m.axBiteCD = axBiteTicks
		target.lastAttacker = m.eid
		target.hurtKind(float64(m.attackDamage()), dtMobAttack)
		h.toNearbyEv(players, m.dim, m.x, m.z, swingArm(m.eid))
		h.playSoundDim(players, m.dim, "minecraft:entity.axolotl.attack", sndNeutral, m.x, m.y, m.z, 1, 1)
		if target.health <= 0 {
			h.killMob(players, target)
		}
	}
	return true
}

// axolotlSupport is applySupportingEffects: regeneration topped up to
// forty seconds at most, and mining fatigue lifted.
func (h *hub) axolotlSupport(players map[int32]*tracked, t *tracked) {
	left := 0
	if e := t.effects[effRegen]; e != nil {
		left = e.left
	}
	if left <= axRegenCap-1 {
		n2 := min(axRegenCap, axRegenAdd+left)
		h.applyEffect(players, t, effRegen, 0, n2/20)
	}
	h.removeEffect(t, effMiningFatigue)
}
