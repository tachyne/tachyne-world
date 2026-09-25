package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Villagers panic (VillagerPanicTrigger + the PANIC package). A villager is
// panicking while it was hurt in the last two seconds (HURT_BY), while it
// sees one of the hostiles it fears inside that hostile's range
// (NEAREST_HOSTILE: zombies, husks, drowned and zombie villagers at eight,
// vexes at eight, vindicators and zoglins at ten, evokers, illusioners and
// ravagers at twelve, pillagers at fifteen), or while whatever hurt it is
// still within six blocks (VillagerCalmDown). In the panic it walks at one
// and a half times its pace: to a random spot up to sixteen blocks away from
// a threat that has come within six (SetWalkTargetAwayFrom), otherwise on
// short scurrying hops (VillageBoundRandomStroll 2, 2).

const (
	villagerFleeSpeed = 1.5 // getPanicPackage: f × 1.5
	villagerFleeTo    = 6.0 // SetWalkTargetAwayFrom(…, 6)
	villagerHurtTicks = 40  // HURT_BY: getLastDamageSource forgets after forty ticks
	villagerWalkGive  = 100 // updates before an unreached walk target is dropped
)

// villagerPanicStep runs each mob update. Returns whether it holds the
// villager.
func (h *hub) villagerPanicStep(players map[int32]*tracked, m *mob) bool {
	if m.sleeping {
		return false
	}
	if m.villagerHurt { // HurtBySensor
		m.villagerHurt = false
		m.villagerHurtLeft = villagerHurtTicks
		m.vHurtBy = m.lastAttacker
	} else if m.villagerHurtLeft > 0 {
		m.villagerHurtLeft -= mobMoveInterval
	}
	// HURT_BY_ENTITY: whoever struck last, while it is about.
	var ax, az float64
	attacker := false
	if m.vHurtBy != 0 {
		if t := players[m.vHurtBy]; t != nil && t.dim == m.dim && !t.dead {
			ax, az, attacker = t.x, t.z, true
		} else if o := h.mobs[m.vHurtBy]; o != nil && o.dim == m.dim && o.dying == 0 {
			ax, az, attacker = o.x, o.z, true
		}
	}
	threat := h.villagerNearestHostile(m)
	attackerNear := attacker && sq(ax-m.x)+sq(az-m.z) <= villagerFleeTo*villagerFleeTo
	if m.villagerHurtLeft <= 0 && threat == nil && !attackerNear {
		m.panicHasT, m.vHurtBy = false, 0 // VillagerCalmDown: forget it, back to the schedule
		return false
	}
	// MoveToTargetSink: a walk target holds until it is reached (or given up).
	if m.panicHasT {
		if m.vPanicLeft--; m.vPanicLeft <= 0 || math.Hypot(m.panicTX-m.x, m.panicTZ-m.z) < 1 {
			m.panicHasT = false
		}
	}
	if !m.panicHasT {
		switch {
		case threat != nil && sq(threat.x-m.x)+sq(threat.z-m.z) < villagerFleeTo*villagerFleeTo:
			m.panicTX, m.panicTZ, m.panicHasT = h.posAwayFrom(m, threat.x, threat.z)
		case attackerNear:
			m.panicTX, m.panicTZ, m.panicHasT = h.posAwayFrom(m, ax, az)
		default:
			m.panicTX, m.panicTZ, m.panicHasT = h.villagerHopTarget(m)
		}
		m.vPanicLeft = villagerWalkGive
	}
	m.rest = 0
	if !m.panicHasT {
		m.vx, m.vz = 0, 0
		return true
	}
	vx, vz := h.pathSteer(m, m.panicTX, m.panicTZ)
	m.vx, m.vz = vx*villagerFleeSpeed, vz*villagerFleeSpeed
	return true
}

// villagerHopTarget is VillageBoundRandomStroll(…, 2, 2)'s LandRandomPos:
// a standable spot within two blocks each way.
func (h *hub) villagerHopTarget(m *mob) (float64, float64, bool) {
	w := h.worldFor(m.dim)
	if w == nil {
		return 0, 0, false
	}
	by := floorInt(m.y)
	for i := 0; i < 10; i++ {
		x := floorInt(m.x) + h.rng.Intn(5) - 2
		z := floorInt(m.z) + h.rng.Intn(5) - 2
		if !w.Loaded(int32(x>>4), int32(z>>4)) {
			continue
		}
		feet := w.MobFeetFrom(x, z, by)
		if feet-by > 2 || by-feet > 2 || !worldgen.Collides(w.At(x, feet-1, z)) ||
			worldgen.Collides(w.At(x, feet, z)) || worldgen.Collides(w.At(x, feet+1, z)) {
			continue
		}
		return float64(x) + 0.5, float64(z) + 0.5, true
	}
	return 0, 0, false
}
