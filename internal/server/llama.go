package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

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
	// Llama.rangedAttackUncertainty: the spit's aim scatter.
	llamaSpitUncertainty = 10.0
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
	// Its target: the attacker it holds (universal anger: the nearest).
	t := players[m.targetEID]
	if t == nil || t.dim != m.dim || dist3sq(t.x, t.y, t.z, m.x, m.y, m.z) > llamaSpitRange*llamaSpitRange {
		if m.targetEID != 0 {
			return
		}
		if t = h.nearestHuntable(players, m.dim, m.x, m.z, llamaSpitRange); t == nil {
			return
		}
	}
	if !h.seeTimeTick(m, t, false) {
		return // RangedAttackGoal: no spit without line of sight
	}
	m.attackCD = llamaSpitCooldown
	h.spitAt(players, m, t.x, t.y+0.6, t.z)
	if !m.llamaDefending {
		// LlamaHurtByTargetGoal: didSpit ends the goal — one gob per
		// provocation, then the llama goes back to its business.
		h.calmDown(m)
	}
}

// spitAt is Llama.spit: the gob leaves from in front of the llama's mouth
// (LlamaSpit's constructor) and is shot at a third of the way up the target
// with a lob that grows with distance, power 1.5 and uncertainty 10. It
// strikes whatever living thing it meets but its own llama.
func (h *hub) spitAt(players map[int32]*tracked, m *mob, tx, ty, tz float64) {
	if math.Hypot(tx-m.x, tz-m.z) > 1e-6 {
		m.yaw = float32(math.Atan2(-(tx-m.x), tz-m.z) * 180 / math.Pi) // face the shot
	}
	r := float64(m.yaw) * math.Pi / 180
	reach := (m.box().w + 1) * 0.5
	ox, oy, oz := m.x-reach*math.Sin(r), m.y+mobEyeHeight(m)-0.1, m.z+reach*math.Cos(r)
	dx, dy, dz := tx-m.x, ty-oy, tz-m.z
	dy += math.Hypot(dx, dz) * float64(float32(0.2)) // the lob
	vx, vy, vz := h.shootVector(dx, dy, dz, llamaSpitSpeed, llamaSpitUncertainty)
	if vx == 0 && vy == 0 && vz == 0 {
		return
	}
	a := h.launchProjectileIn(players, entityLlamaSpit, m.dim, ox, oy, oz, vx, vy, vz)
	a.shooter, a.dmg, a.breaks, a.mobShot = m.eid, llamaSpitDmg, true, true
	h.playSoundDim(players, m.dim, "minecraft:entity.llama.spit", sndNeutral, m.x, m.y, m.z, 1, 1+(h.rng.Float32()-h.rng.Float32())*0.2)
}

// llamaWolfRange is LlamaAttackWolfGoal's follow distance: a quarter of the
// llama's forty.
const llamaWolfRange = 10.0

// llamaWolfTick is the llama's mob targets driving its RangedAttackGoal:
// LlamaAttackWolfGoal now and then (one in sixteen ticks) picks the nearest
// wild wolf within ten blocks; a trader llama adds TraderLlama's
// NearestAttackableTargetGoals for zombies (not zombified piglins) and
// illagers, in sight within its follow range, and whatever hurt the trader
// it is leashed to (traderLlamasDefendMob). It spits on the ranged cadence
// while the target stays in reach and in sight.
func (h *hub) llamaWolfTick(players map[int32]*tracked, m *mob) {
	if m.hostile || m.dying > 0 || m.rider != 0 {
		return
	}
	if m.attackCD > 0 {
		m.attackCD--
		return
	}
	w := h.mobs[m.llamaWolf]
	keep := llamaWolfRange
	if w != nil && w.etype != entityWolf {
		keep = m.followRange() // a TargetGoal holds on out to the follow range
	}
	if w == nil || w.dying > 0 || (w.etype == entityWolf && w.tamed) || w.dim != m.dim || dist3(w.x, w.y, w.z, m.x, m.y, m.z) > keep {
		m.llamaWolf, w = 0, nil
		if m.etype == entityTraderLlama && h.rng.Intn(10/mobMoveInterval) == 0 {
			w = h.traderLlamaFoe(m)
		}
		if w == nil && h.rng.Intn(16/mobMoveInterval) == 0 {
			best := llamaWolfRange
			h.grid().nearby(m.dim, m.x, m.z, llamaWolfRange, func(o *mob) {
				if o.etype == entityWolf && !o.tamed && o.dying == 0 {
					if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < best {
						w, best = o, d
					}
				}
			})
		}
		if w == nil {
			return
		}
		m.llamaWolf = w.eid
	}
	if !h.mobSeesMob(m, w) || dist3(w.x, w.y, w.z, m.x, m.y, m.z) > llamaSpitRange {
		return
	}
	m.attackCD = llamaSpitCooldown
	h.spitAt(players, m, w.x, w.y+0.3, w.z)
}

// traderLlamaFoe is TraderLlama's two NearestAttackableTargetGoals (mustSee):
// the nearest Zombie that is not a zombified piglin, or AbstractIllager,
// within the llama's follow range.
func (h *hub) traderLlamaFoe(m *mob) *mob {
	var foe *mob
	best := m.followRange()
	h.grid().nearby(m.dim, m.x, m.z, best, func(o *mob) {
		if o.dying > 0 || !(zombieKind(o.etype) || isIllager(o.etype)) { // zombieKind already leaves out the zombified piglin
			return
		}
		if d := dist3(o.x, o.y, o.z, m.x, m.y, m.z); d < best && h.mobSeesMob(m, o) {
			foe, best = o, d
		}
	})
	return foe
}

// traderLlamasDefendMob is TraderLlamaDefendWanderingTraderGoal for a mob
// attacker: the trader's getLastHurtByMob is whatever hurt it, not only a
// player, and every trader llama on its leads turns on it.
func (h *hub) traderLlamasDefendMob(trader, a *mob) {
	if trader.etype != entityWanderingTrader || a == nil || a.dying > 0 {
		return
	}
	for _, l := range h.mobs {
		if l.etype == entityTraderLlama && l.leash == trader.eid && l.dying == 0 && l.eid != a.eid {
			l.llamaWolf = a.eid
		}
	}
}

// spitTouchesNonAir is LlamaSpit.tick's touchesNoAir test: does the gob's
// 0.25 box at (x, y, z) overlap any cell that is not air.
func (h *hub) spitTouchesNonAir(dim int, x, y, z float64) bool {
	const half, height = 0.125, 0.25
	w := h.worldFor(dim)
	for bx := int(math.Floor(x - half)); bx <= int(math.Floor(x+half)); bx++ {
		for by := int(math.Floor(y)); by <= int(math.Floor(y+height)); by++ {
			for bz := int(math.Floor(z - half)); bz <= int(math.Floor(z+half)); bz++ {
				if st := w.At(bx, by, bz); st != worldgen.Air && st != caveAirState && st != voidAirState {
					return true
				}
			}
		}
	}
	return false
}
