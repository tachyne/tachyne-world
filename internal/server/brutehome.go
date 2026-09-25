package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A piglin brute's idle life (PiglinBruteAi's IDLE activity). It remembers
// where it spawned — its HOME, the bastion it guards — and whenever it has
// nothing to do it picks one of: a random stroll, walking up to a piglin or
// another brute within eight blocks, walking back home when it is within a
// hundred blocks of it (at most every four seconds), a stroll about when it
// is within five blocks of home (at most every nine seconds), or a pause of
// a second and a half to three. All of it at 0.6 of its pace. So a brute
// keeps to its bastion instead of drifting off across the Nether.

const (
	bruteIdleSpeed      = 0.6 // SPEED_MULTIPLIER_WHEN_IDLING
	bruteHomeClose      = 2   // HOME_CLOSE_ENOUGH_DISTANCE
	bruteHomeTooFar     = 100 // HOME_TOO_FAR_DISTANCE
	bruteHomeStrollDist = 5   // HOME_STROLL_AROUND_DISTANCE
	bruteHomeToEvery    = 80  // StrollToPoi's nextOkStartTime step
	bruteHomeAroundGap  = 180 // StrollAroundPoi.MIN_TIME_BETWEEN_STROLLS
	bruteInteractRange  = 8   // INTERACTION_RANGE
	bruteInteractStop   = 2   // InteractWith's stop distance
	idleWalkUpdates     = 300 // a walk that has not arrived in thirty seconds is stuck: given up
)

// idleWalk is a WALK_TARGET an idle behaviour set: a spot, or a mob to walk
// up to; still is a DoNothing pause.
type idleWalk struct {
	x, z  float64
	eid   int32
	close float64
	speed float64
	left  int // mob updates before it is given up
	still bool
}

// idleWalkStep walks the mob to its idle walk target. Returns whether the
// walk holds it this update (false once it has arrived or given up).
func (h *hub) idleWalkStep(m *mob) bool {
	w := m.idleWalk
	if w == nil {
		return false
	}
	if w.left--; w.left < 0 {
		m.idleWalk = nil
		return false
	}
	if w.still {
		m.vx, m.vz = m.vx*0.6, m.vz*0.6
		return true
	}
	tx, tz := w.x, w.z
	if w.eid != 0 {
		o := h.mobs[w.eid]
		if o == nil || o.dying > 0 || o.dim != m.dim {
			m.idleWalk = nil
			return false
		}
		tx, tz = o.x, o.z
	}
	if d := math.Hypot(tx-m.x, tz-m.z); d <= w.close || d < standoffDist+0.05 { // the steering stops short at the standoff
		m.idleWalk = nil
		m.vx, m.vz = 0, 0
		return true
	}
	vx, vz := h.pathSteer(m, tx, tz)
	m.vx, m.vz = vx*w.speed, vz*w.speed
	m.rest = 0
	m.headYaw = float32(math.Atan2(-(tx-m.x), tz-m.z) * 180 / math.Pi)
	return true
}

// landRandomPos is LandRandomPos.getPos: a standable spot within h blocks
// sideways and v up or down.
func (h *hub) landRandomPos(m *mob, hr, vr int) (float64, float64, bool) {
	w := h.worldFor(m.dim)
	if w == nil {
		return 0, 0, false
	}
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	for i := 0; i < 10; i++ {
		x := bx + h.rng.Intn(2*hr+1) - hr
		y := by + h.rng.Intn(2*vr+1) - vr
		z := bz + h.rng.Intn(2*hr+1) - hr
		if !w.Loaded(int32(x>>4), int32(z>>4)) {
			continue
		}
		for dy := 0; dy < 2*vr && y > by-vr && !worldgen.Collides(w.At(x, y-1, z)); dy++ {
			y--
		}
		if !worldgen.Collides(w.At(x, y-1, z)) || worldgen.Collides(w.At(x, y, z)) ||
			worldgen.Collides(w.At(x, y+1, z)) || worldgen.IsLava(w.At(x, y, z)) {
			continue
		}
		return float64(x) + 0.5, float64(z) + 0.5, true
	}
	return 0, 0, false
}

// bruteIdleStep is the brute's IDLE movement RunOne. Returns whether it
// holds the brute this update; a fight (the attack target) is left to the
// ordinary hunt.
func (h *hub) bruteIdleStep(m *mob) bool {
	if m.dying > 0 || m.hasTarget {
		m.idleWalk = nil
		return false
	}
	if m.home == (blockPos{}) {
		m.home = blockPos{floorInt(m.x), floorInt(m.y), floorInt(m.z)} // initMemories: where it was made
	}
	if h.idleWalkStep(m) {
		return true
	}
	// RunOne: the options shuffled by weight, the first that runs wins.
	opts := []int{0, 1, 2, 3, 4, 5}
	weights := [6]int{2, 2, 2, 2, 2, 1}
	for n := len(opts); n > 0; n-- {
		total := 0
		for _, o := range opts[:n] {
			total += weights[o]
		}
		r := h.rng.Intn(total)
		pick := 0
		for i, o := range opts[:n] {
			if r -= weights[o]; r < 0 {
				pick = i
				break
			}
		}
		o := opts[pick]
		opts[pick], opts[n-1] = opts[n-1], opts[pick]
		if h.bruteIdleOption(m, o) {
			h.idleWalkStep(m)
			return true
		}
	}
	return false
}

// bruteIdleOption tries one of the RunOne's behaviours; true = it ran.
func (h *hub) bruteIdleOption(m *mob, o int) bool {
	now := h.tick.Load()
	hx, hy, hz := float64(m.home.x)+0.5, float64(m.home.y)+0.5, float64(m.home.z)+0.5
	homeD := dist3(hx, hy, hz, m.x, m.y, m.z)
	switch o {
	case 0: // RandomStroll.stroll(0.6): within ten sideways, seven up or down
		x, z, ok := h.landRandomPos(m, 10, 7)
		if !ok {
			return false
		}
		m.idleWalk = &idleWalk{x: x, z: z, close: 1, speed: bruteIdleSpeed, left: idleWalkUpdates}
	case 1, 2: // InteractWith piglin / piglin brute, within eight
		want := entityPiglin
		if o == 2 {
			want = entityPiglinBrute
		}
		var best *mob
		bestD := float64(bruteInteractRange)
		h.grid().nearby(m.dim, m.x, m.z, bestD, func(c *mob) {
			if c == m || c.etype != want || c.dying > 0 {
				return
			}
			if d := dist3(c.x, c.y, c.z, m.x, m.y, m.z); d <= bestD && h.mobSeesMob(m, c) {
				best, bestD = c, d
			}
		})
		if best == nil {
			return false
		}
		m.idleWalk = &idleWalk{eid: best.eid, close: bruteInteractStop, speed: bruteIdleSpeed, left: idleWalkUpdates}
		m.headYaw = float32(math.Atan2(-(best.x-m.x), best.z-m.z) * 180 / math.Pi)
	case 3: // StrollToPoi(HOME, 0.6, 2, 100)
		if homeD >= bruteHomeTooFar {
			return false
		}
		if now <= m.homeToNext {
			return true // ran, but too soon to set off again
		}
		m.idleWalk = &idleWalk{x: hx, z: hz, close: bruteHomeClose, speed: bruteIdleSpeed, left: idleWalkUpdates}
		m.homeToNext = now + bruteHomeToEvery
	case 4: // StrollAroundPoi(HOME, 0.6, 5): a stroll from where it stands
		if homeD >= bruteHomeStrollDist {
			return false
		}
		if now <= m.homeAroundNext {
			return true
		}
		m.homeAroundNext = now + bruteHomeAroundGap
		x, z, ok := h.landRandomPos(m, 8, 6)
		if !ok {
			return true // setOrErase: no spot, no walk
		}
		m.idleWalk = &idleWalk{x: x, z: z, close: 1, speed: bruteIdleSpeed, left: idleWalkUpdates}
	case 5: // DoNothing(30, 60)
		m.idleWalk = &idleWalk{still: true, left: (30 + h.rng.Intn(31)) / mobMoveInterval}
	}
	return true
}
