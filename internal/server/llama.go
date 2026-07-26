package server

import "math"

// The llama's spit.
//
// Llamas were rideable, formed caravans and joined wandering traders, but the
// one thing everybody knows a llama for was missing: it never spat. A provoked
// llama simply bit, which is doubly wrong — vanilla llamas carry no attack
// damage attribute at all, so the single point of damage a llama can do IS the
// spit, delivered from up to twenty blocks away.
//
// The species table's melee is set to zero to match, which the table already
// documents as "never melees".

const (
	llamaSpitRange = 20.0 // RangedAttackGoal's attack radius
	llamaSpitDmg   = 1
	llamaSpitSpeed = 1.5 // the projectile's launch velocity
	// RangedAttackGoal's 40-tick interval, counted in mob-updates (2 ticks
	// each) and including the update that fires — the same accounting the
	// skeleton's bow cadence uses.
	llamaSpitCooldown = 19
)

var entityLlamaSpit = entityID("llama_spit")

// llamaSpit hurls a gob at the nearest player the llama is angry with. The lob
// mirrors vanilla's: aim a third of the way up the target and add a rise
// proportional to the horizontal distance, so the arc drops onto them rather
// than flying flat.
func (h *hub) llamaSpit(players map[int32]*tracked, m *mob) {
	if m.attackCD > 0 {
		m.attackCD--
		return
	}
	t := h.nearestHuntable(players, m.dim, m.x, m.z, llamaSpitRange)
	if t == nil {
		return
	}
	m.attackCD = llamaSpitCooldown
	ox, oy, oz := m.x, m.y+1.4, m.z
	dx, dy, dz := t.x-ox, (t.y+0.6)-oy, t.z-oz
	dy += math.Hypot(dx, dz) * 0.2 // the lob
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return
	}
	m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi) // face the shot
	a := h.launchProjectileIn(players, entityLlamaSpit, m.dim, ox, oy, oz,
		dx/d*llamaSpitSpeed, dy/d*llamaSpitSpeed, dz/d*llamaSpitSpeed)
	a.shooter, a.dmg, a.breaks = m.eid, llamaSpitDmg, true
	h.playSoundDim(players, m.dim, "minecraft:entity.llama.spit", sndNeutral, m.x, m.y, m.z, 1, 1)
}
