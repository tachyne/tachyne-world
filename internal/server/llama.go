package server

import "math"

// The llama's spit.
//
// Llamas were rideable, formed caravans (caravan.go) and joined wandering traders, but the
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
	if !h.seeTimeTick(m, t, false) {
		return // RangedAttackGoal: no spit without line of sight
	}
	m.attackCD = llamaSpitCooldown
	h.spitAt(players, m, t.x, t.y+0.6, t.z)
}

// spitAt is Llama.spit: the gob aimed a third of the way up the target, with
// the lob. It strikes whatever living thing it meets but its own llama.
func (h *hub) spitAt(players map[int32]*tracked, m *mob, tx, ty, tz float64) {
	ox, oy, oz := m.x, m.y+1.4, m.z
	dx, dy, dz := tx-ox, ty-oy, tz-oz
	dy += math.Hypot(dx, dz) * 0.2 // the lob
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return
	}
	m.yaw = float32(math.Atan2(-(tx-m.x), tz-m.z) * 180 / math.Pi) // face the shot
	a := h.launchProjectileIn(players, entityLlamaSpit, m.dim, ox, oy, oz,
		dx/d*llamaSpitSpeed, dy/d*llamaSpitSpeed, dz/d*llamaSpitSpeed)
	a.shooter, a.dmg, a.breaks, a.mobShot = m.eid, llamaSpitDmg, true, true
	h.playSoundDim(players, m.dim, "minecraft:entity.llama.spit", sndNeutral, m.x, m.y, m.z, 1, 1)
}

// llamaWolfRange is LlamaAttackWolfGoal's follow distance: a quarter of the
// llama's forty.
const llamaWolfRange = 10.0

// llamaWolfTick is LlamaAttackWolfGoal driving the llama's RangedAttackGoal:
// now and then (one in sixteen ticks) a llama picks the nearest wild wolf
// within ten blocks and spits at it on the ranged cadence while it stays in
// reach and in sight.
func (h *hub) llamaWolfTick(players map[int32]*tracked, m *mob) {
	if m.hostile || m.dying > 0 || m.rider != 0 {
		return
	}
	if m.attackCD > 0 {
		m.attackCD--
		return
	}
	w := h.mobs[m.llamaWolf]
	if w == nil || w.dying > 0 || w.tamed || w.dim != m.dim || dist3(w.x, w.y, w.z, m.x, m.y, m.z) > llamaWolfRange {
		m.llamaWolf, w = 0, nil
		if h.rng.Intn(16/mobMoveInterval) != 0 {
			return
		}
		best := llamaWolfRange
		h.grid().nearby(m.dim, m.x, m.z, llamaWolfRange, func(o *mob) {
			if o.etype == entityWolf && !o.tamed && o.dying == 0 {
				if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < best {
					w, best = o, d
				}
			}
		})
		if w == nil {
			return
		}
		m.llamaWolf = w.eid
	}
	if !h.mobSeesMob(m, w) {
		return
	}
	m.attackCD = llamaSpitCooldown
	h.spitAt(players, m, w.x, w.y+0.3, w.z)
}
