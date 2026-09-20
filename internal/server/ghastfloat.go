package server

import "math"

// The ghast's flight. Vanilla gives a ghast no chase goal at all: it drifts
// to a random point within sixteen blocks (RandomFloatAroundGoal), turns to
// face whatever it has targeted (GhastLookGoal) and shoots from wherever it
// happens to be. That is why a ghast never comes down on you — it hangs
// about the same patch of air and lobs fireballs. It also only targets
// somebody within four blocks of its own height.

const (
	ghastFloatRange  = 16.0   // chooseRandomPosition: ±16 on each axis
	ghastFloatArrive = 1.0    // canUse: within one block of the wanted spot
	ghastFloatFar    = 3600.0 // …or more than sixty blocks from it (squared)
	ghastTargetDY    = 4.0    // the target selector's |Δy| ≤ 4
)

// floatAroundBehavior is RandomFloatAroundGoal: pick a spot, drift to it,
// pick another. Targets are shot at, never chased.
type floatAroundBehavior struct{}

func (floatAroundBehavior) name() string { return "float-around" }

func (floatAroundBehavior) steer(h *hub, m *mob) (float64, float64) {
	dx, dy, dz := m.floatX-m.x, m.floatY-m.y, m.floatZ-m.z
	d2 := dx*dx + dy*dy + dz*dz
	if !m.floatSet || d2 < ghastFloatArrive*ghastFloatArrive || d2 > ghastFloatFar {
		m.floatX = m.x + (h.rng.Float64()*2-1)*ghastFloatRange
		m.floatY = m.y + (h.rng.Float64()*2-1)*ghastFloatRange
		m.floatZ = m.z + (h.rng.Float64()*2-1)*ghastFloatRange
		m.floatSet = true
		dx, dy, dz = m.floatX-m.x, m.floatY-m.y, m.floatZ-m.z
		d2 = dx*dx + dy*dy + dz*dz
	}
	// A flyer's vertical drift rides on hover/step elsewhere; the steering
	// pair is horizontal, as it is for every other behaviour.
	d := math.Sqrt(d2)
	if d < 1e-6 {
		return 0, 0
	}
	_ = dy
	sp := m.moveSpeed()
	return dx / d * sp, dz / d * sp
}

// ghastCanTarget is the ghast's target selector: only somebody at roughly
// its own height is worth shooting at.
func ghastCanTarget(m *mob, y float64) bool {
	return math.Abs(y-m.y) <= ghastTargetDY
}
