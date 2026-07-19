package server

import "math"

// Thrown potions: splash and lingering. A splash potion shatters on any impact
// (block or entity) and applies its effects to every survival player in range,
// with duration scaled by proximity — vanilla ThrownPotion.applySplash. A
// lingering potion instead leaves an area-effect cloud that re-applies its
// (weaker) effects to whoever stands in it as it shrinks away.
//
// Effects are player-only, matching the rest of the engine's status-effect
// system (mobs have no effect map). The potion's effect table lives with the
// brewing code (potionEffects) so drink/splash/lingering never drift.

const (
	splashRadius = 4.0  // vanilla applySplash inflate(4,2,4) → a 4-block reach
	splashFactor = 0.75 // splash potions carry 3/4 of the drink duration
	lingerFactor = 0.25 // a lingering cloud's effects are 1/4 duration
	cloudTicks   = 600  // ~30 s cloud lifetime (vanilla AreaEffectCloud)
	cloudRadius0 = 3.0  // starting cloud radius
	cloudReapply = 20   // re-apply the effect to occupants once a second
	cloudPuff    = 5    // emit the visible cloud particles every N ticks
)

// effectCloud is one lingering-potion cloud resting on the ground.
type effectCloud struct {
	eid       int32
	dim       int
	x, y, z   float64
	kind      int8
	radius    float64
	ttl       int    // ticks of life left
	reapplyAt uint64 // next tick it doses whoever stands in it
}

// splashPotion resolves a thrown potion at its impact point.
func (h *hub) splashPotion(players map[int32]*tracked, dim int, x, y, z float64, kind int8, lingering bool) {
	h.playSound(players, "minecraft:entity.splash_potion.break", sndNeutral, x, y, z, 1, 1)
	h.spawnParticles(players, particleSplash, x, y, z, 0.4, 0.2, 8)
	if lingering {
		h.spawnPotionCloud(dim, x, y, z, kind)
		return
	}
	effs := potionEffects(kind)
	if len(effs) == 0 {
		return
	}
	for _, t := range players {
		if t.dim != dim || t.gamemode != gmSurvival || t.dead {
			continue
		}
		d := dist3(t.x, t.y+1, t.z, x, y, z)
		if d > splashRadius {
			continue
		}
		h.applyPotionAoE(players, t, effs, 1-d/splashRadius, splashFactor)
	}
}

// applyPotionAoE doses one player with a potion's effects, scaling timed effects
// by proximity×factor and instant effects by proximity (vanilla applySplash).
func (h *hub) applyPotionAoE(players map[int32]*tracked, t *tracked, effs []potEffect, prox, factor float64) {
	if prox < 0 {
		prox = 0
	}
	for _, e := range effs {
		if e.secs == 0 { // instant (Healing): magnitude scales with proximity
			if e.id == effInstantHealth {
				heal := float32(prox) * 4 * float32(int(1)<<e.amp)
				t.health = float32(math.Min(maxHealth, float64(t.health)+float64(heal)))
				h.sendHealth(t)
			} else {
				h.applyEffect(players, t, e.id, e.amp, 0)
			}
			continue
		}
		if secs := int(float64(e.secs) * factor * prox); secs >= 1 {
			h.applyEffect(players, t, e.id, e.amp, secs)
		}
	}
}

// spawnPotionCloud drops a lingering-potion cloud at the impact.
func (h *hub) spawnPotionCloud(dim int, x, y, z float64, kind int8) {
	if len(potionEffects(kind)) == 0 {
		return // water/awkward: nothing to linger
	}
	eid := h.allocEID()
	h.clouds[eid] = &effectCloud{eid: eid, dim: dim, x: x, y: y, z: z, kind: kind,
		radius: cloudRadius0, ttl: cloudTicks, reapplyAt: h.tick.Load()}
}

// updateClouds shrinks every lingering cloud, doses occupants on its cadence,
// and puffs its particles; a cloud expires when its life or radius runs out.
func (h *hub) updateClouds(players map[int32]*tracked) {
	if len(h.clouds) == 0 {
		return
	}
	now := h.tick.Load()
	for eid, c := range h.clouds {
		c.ttl--
		c.radius -= cloudRadius0 / float64(cloudTicks) // linear shrink to nothing
		if c.ttl <= 0 || c.radius <= 0.3 {
			delete(h.clouds, eid)
			continue
		}
		if now%cloudPuff == 0 {
			h.spawnParticles(players, particleSplash, c.x, c.y+0.1, c.z, float32(c.radius), 0, int32(c.radius*6))
		}
		if now < c.reapplyAt {
			continue
		}
		c.reapplyAt = now + cloudReapply
		effs := potionEffects(c.kind)
		for _, t := range players {
			if t.dim != c.dim || t.gamemode != gmSurvival || t.dead {
				continue
			}
			if dist3(t.x, t.y+1, t.z, c.x, c.y, c.z) <= c.radius {
				h.applyPotionAoE(players, t, effs, 1, lingerFactor)
			}
		}
	}
}

// throwSplashPotion launches a thrown-potion projectile from a player (use_item
// on a splash/lingering potion), consuming one from the slot.
func (h *hub) throwSplashPotion(players map[int32]*tracked, t *tracked, slot int) {
	if t.inv == nil || slot < 0 || slot >= 9 {
		return
	}
	s := &t.inv.slots[slot]
	if s.item != itemSplashPotion && s.item != itemLingerPotion {
		return
	}
	lingering := s.item == itemLingerPotion
	kind := s.potion
	// Aim from the eyes along the look vector (vanilla throw power 0.5, downward
	// -20° pitch offset folded into the client's aim — we approximate with the
	// player's own pitch).
	yaw, pitch := float64(t.yaw)*math.Pi/180, float64(t.pitch)*math.Pi/180
	dx := -math.Sin(yaw) * math.Cos(pitch)
	dy := -math.Sin(pitch)
	dz := math.Cos(yaw) * math.Cos(pitch)
	const v = 0.5
	a := h.launchProjectileIn(players, entitySplashProj, t.dim, t.x, t.y+1.4, t.z, dx*v, dy*v, dz*v)
	a.shooter, a.splash, a.breaks, a.potion, a.lingering = t.p.eid, true, true, kind, lingering
	a.playerShot, a.noHitUntil = true, h.tick.Load()+2 // don't shatter on the thrower at launch
	h.playSound(players, "minecraft:entity.splash_potion.throw", sndPlayer, t.x, t.y, t.z, 0.5, 1)
	if t.gamemode != gmCreative {
		if s.count--; s.count <= 0 {
			*s = invStack{}
		}
		h.sendSlot(t, slot)
	}
}
