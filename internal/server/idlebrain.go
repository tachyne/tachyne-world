package server

import (
	"math"
	"sort"
)

// The idle pieces the brain-driven animals share: RunOne, which shuffles its
// options by weight and runs the first that starts, and the look behaviours
// (SetEntityLookTargetSometimes, LookAtTargetSink, RandomLookAround) that
// decide where the head points and what SetWalkTargetFromLookTarget walks to.

// runOne is RunOne's pick: ShufflingList orders the options by
// -nextFloat()^(1/weight), and the first one that starts wins. try reports
// whether option i started; runOne reports whether any did.
func (h *hub) runOne(weights []int, try func(i int) bool) bool {
	type entry struct {
		i   int
		key float64
	}
	es := make([]entry, len(weights))
	for i, w := range weights {
		es[i] = entry{i, -math.Pow(h.rng.Float64(), 1/float64(w))}
	}
	sort.SliceStable(es, func(a, b int) bool { return es[a].key < es[b].key })
	for _, e := range es {
		if try(e.i) {
			return true
		}
	}
	return false
}

const (
	lookSinkMin     = 45 // LookAtTargetSink(45, 90)
	lookSinkMax     = 90
	brainLookRange  = 6 // SetEntityLookTargetSometimes(PLAYER, 6, …)
	randomLookYaw   = 30
	randomLookGapLo = 150 // RandomLookAround(UniformInt.of(150, 250), 30, 0, 0)
	randomLookGapHi = 250
)

// brainLook runs a brain animal's look behaviours for one update: the
// LOOK_TARGET it holds for LookAtTargetSink's 45-90 ticks; with none, a
// player within six blocks on SetEntityLookTargetSometimes' lo-hi tick
// cadence; and, where the brain has it, RandomLookAround — a point thirty
// degrees either side of the body, then a gaze cooldown of 150-250 ticks.
func (h *hub) brainLook(players map[int32]*tracked, m *mob, lo, hi int, randomLook bool) {
	if m.gazeCD > 0 { // CountDownCooldownTicks(GAZE_COOLDOWN_TICKS)
		m.gazeCD = max(0, m.gazeCD-mobMoveInterval)
	}
	if m.lookTicks > 0 {
		m.lookTicks -= mobMoveInterval
		if m.lookEID == 0 {
			m.headYaw = yawToward(0, 0, m.lookDX, m.lookDZ)
			return
		}
		if t := players[m.lookEID]; t != nil && t.dim == m.dim && !t.dead {
			m.headYaw = yawToward(m.x, m.z, t.x, t.z)
			return
		}
		m.lookTicks = 0 // whoever it was watching is gone
	}
	m.lookTicks, m.lookEID = 0, 0
	m.headYaw = m.yaw
	if t := h.nearestPlayerIn(players, m, brainLookRange); t != nil {
		for i := 0; i < mobMoveInterval; i++ {
			if m.lookTicker == 0 { // Ticker.tickDownAndCheck
				m.lookTicker = int32(lo + h.rng.Intn(hi-lo+1) - 1)
				continue
			}
			if m.lookTicker--; m.lookTicker == 0 {
				m.lookEID = t.p.eid
				m.lookTicks = int32(lookSinkMin + h.rng.Intn(lookSinkMax-lookSinkMin+1))
				m.headYaw = yawToward(m.x, m.z, t.x, t.z)
				return
			}
		}
	}
	if randomLook && m.gazeCD == 0 {
		yaw := float64(m.yaw) + 2*h.rng.Float64()*randomLookYaw - randomLookYaw
		r := yaw * math.Pi / 180
		m.lookEID, m.lookDX, m.lookDZ = 0, -math.Sin(r), math.Cos(r)
		m.lookTicks = int32(lookSinkMin + h.rng.Intn(lookSinkMax-lookSinkMin+1))
		m.gazeCD = int32(randomLookGapLo + h.rng.Intn(randomLookGapHi-randomLookGapLo+1))
		m.headYaw = float32(yaw)
	}
}

// walkFromLookTarget is SetWalkTargetFromLookTarget(speed, 3): with a
// LOOK_TARGET, a walk to within three blocks of it. A target already that
// close is reached at once, so only a player farther off starts a walk.
// It reports whether the behaviour started (it needs a look target).
func (h *hub) walkFromLookTarget(players map[int32]*tracked, m *mob, speed float64) bool {
	if m.lookTicks <= 0 {
		return false
	}
	if t := players[m.lookEID]; t != nil && m.lookEID != 0 && math.Hypot(t.x-m.x, t.z-m.z) > 3 {
		m.idleWalk = &idleWalk{x: t.x, z: t.z, close: 3, speed: speed, left: idleWalkUpdates}
	}
	return true
}

// doNothing is DoNothing(30, 60): the RunOne holds still for 30-60 ticks.
func (h *hub) doNothing(m *mob) {
	m.idleWalk = &idleWalk{still: true, left: (30 + h.rng.Intn(31)) / mobMoveInterval}
}

// randomStroll is RandomStroll.stroll(speed, xz, y): a walk to a standable
// spot within xz sideways and y up or down. It always starts; with no spot
// found it just sets no walk.
func (h *hub) randomStroll(m *mob, speed float64, xz, y int) {
	if x, z, ok := h.landRandomPos(m, xz, y); ok {
		m.idleWalk = &idleWalk{x: x, z: z, close: 1, speed: speed, left: idleWalkUpdates}
	}
}
