package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Goats long-jump (GoatAi LongJumpToRandomPos + LongJumpMidJump): every
// thirty to sixty seconds an idle goat looks, twenty tries, for a spot up
// to five blocks out and five up or down that it cannot simply walk to —
// a gap or a ledge in the way — crouches for forty ticks, and jumps it on
// a steep arc (angles sixty-five to eighty, its long-jump bleat), landing
// with a hoof-step and a fresh cooldown.

const (
	goatJumpCDMin    = 600 // TIME_BETWEEN_LONG_JUMPS
	goatJumpCDMax    = 1200
	goatJumpWidth    = 5  // MAX_LONG_JUMP_WIDTH
	goatJumpHeight   = 5  // MAX_LONG_JUMP_HEIGHT
	goatJumpTries    = 20 // FIND_JUMP_TRIES
	goatJumpPrepare  = 40 // PREPARE_JUMP_DURATION
	goatJumpMaxVel   = 3.5714288
	goatJumpMinReach = 8 // MIN_PATHFIND_DISTANCE_TO_VALID_JUMP (ours: any gap in the line)
)

var goatJumpAngles = []int{65, 70, 75, 80} // LongJumpToRandomPos.ALLOWED_ANGLES

// walkableLine reports whether a goat could just walk to (tx, tz): every
// column along the line is standable within a block of the last.
func (h *hub) walkableLine(m *mob, tx, ty, tz float64) bool {
	w := h.worldFor(m.dim)
	dx, dz := tx-m.x, tz-m.z
	d := math.Hypot(dx, dz)
	if d < 1e-6 {
		return true
	}
	last := int(math.Floor(m.y))
	steps := int(math.Ceil(d))
	for i := 1; i <= steps; i++ {
		f := float64(i) / float64(steps)
		x, z := int(math.Floor(m.x+dx*f)), int(math.Floor(m.z+dz*f))
		feet := w.MobFeetFrom(x, z, last)
		if feet-last > 1 || last-feet > 1 || !worldgen.Collides(w.At(x, feet-1, z)) || worldgen.IsWater(w.At(x, feet, z)) {
			return false
		}
		last = feet
	}
	return int(math.Floor(ty)) == last
}

// goatJumpCandidate is one of the twenty tries: a spot with a solid floor
// and standing room the goat cannot walk to.
func (h *hub) goatJumpCandidate(m *mob) (float64, float64, float64, bool) {
	w := h.worldFor(m.dim)
	bx, by, bz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	for i := 0; i < goatJumpTries; i++ {
		x := bx + h.rng.Intn(2*goatJumpWidth+1) - goatJumpWidth
		y := by + h.rng.Intn(2*goatJumpHeight+1) - goatJumpHeight
		z := bz + h.rng.Intn(2*goatJumpWidth+1) - goatJumpWidth
		if (x == bx && z == bz) || math.Hypot(float64(x-bx), float64(z-bz)) < 2 {
			continue
		}
		if !worldgen.Collides(w.At(x, y-1, z)) || worldgen.Collides(w.At(x, y, z)) || worldgen.Collides(w.At(x, y+1, z)) {
			continue // isAcceptableLandingPosition: a solid floor, room to stand
		}
		tx, ty, tz := float64(x)+0.5, float64(y), float64(z)+0.5
		if h.walkableLine(m, tx, ty, tz) {
			continue // it could just walk there
		}
		if _, _, _, ok := jumpVectorFor(m, tx, ty, tz, goatJumpAngles, goatJumpMaxVel, h.rng.Intn); ok {
			return tx, ty, tz, true
		}
	}
	return 0, 0, 0, false
}

// goatJumpStep runs each mob update. Returns whether it holds the goat.
func (h *hub) goatJumpStep(players map[int32]*tracked, m *mob) bool {
	if m.goatJumping {
		return true // goatFlight moves it
	}
	if m.goatJumpCD > 0 {
		m.goatJumpCD -= mobMoveInterval
	}
	if m.goatPrep > 0 {
		m.goatPrep -= mobMoveInterval
		m.vx, m.vz = 0, 0
		if m.goatPrep > 0 {
			return true
		}
		vx, vy, vz, ok := jumpVectorFor(m, m.goatJumpX, m.goatJumpY, m.goatJumpZ, goatJumpAngles, goatJumpMaxVel, h.rng.Intn)
		if !ok {
			m.goatJumpCD = goatJumpCDMin
			return false
		}
		sound := "minecraft:entity.goat.long_jump"
		if m.screaming {
			sound = "minecraft:entity.goat.screaming.long_jump"
		}
		h.playSoundDim(players, m.dim, sound, sndNeutral, m.x, m.y, m.z, 1, 1)
		m.goatJumping, m.goatVX, m.goatVY, m.goatVZ = true, vx, vy, vz
		m.yaw = float32(math.Atan2(-vx, vz) * 180 / math.Pi)
		h.breezePose(players, m, poseLongJumping)
		return true
	}
	if m.goatJumpCD > 0 || m.ramPhase != 0 || m.tempted || m.loveTicks > 0 || m.panic > 0 || m.hasTarget ||
		h.inWater(m.dim, m.x, m.y, m.z) {
		return false
	}
	if m.goatJumpCD == 0 && !m.goatJumpSet { // initMemories: the first cooldown is rolled, not zero
		m.goatJumpCD, m.goatJumpSet = goatJumpCDMin+h.rng.Intn(goatJumpCDMax-goatJumpCDMin+1), true
		return false
	}
	tx, ty, tz, ok := h.goatJumpCandidate(m)
	if !ok {
		m.goatJumpCD = goatJumpCDMin + h.rng.Intn(goatJumpCDMax-goatJumpCDMin+1) // nothing to jump: wait for the next look
		return false
	}
	m.goatJumpX, m.goatJumpY, m.goatJumpZ = tx, ty, tz
	m.goatPrep = goatJumpPrepare
	m.yaw = float32(math.Atan2(-(tx-m.x), tz-m.z) * 180 / math.Pi)
	m.vx, m.vz = 0, 0
	return true
}

// goatFlight is the arc, in place of the walk.
func (h *hub) goatFlight(players map[int32]*tracked, m *mob) {
	w := h.worldFor(m.dim)
	for i := 0; i < mobMoveInterval; i++ {
		m.goatVY -= m.effectiveGravity(m.goatVY)
		nx, nz := m.x+m.goatVX, m.z+m.goatVZ
		if h.ownedAt(nx, nz) && !worldgen.Collides(w.At(int(math.Floor(nx)), int(math.Floor(m.y)), int(math.Floor(nz)))) {
			m.x, m.z = nx, nz
		} else {
			m.goatVX, m.goatVZ = 0, 0
		}
		m.y += m.goatVY
		// Landing: the goat's box is 0.9 wide, so a ledge under any corner
		// catches it, as vanilla's collision does.
		feet, landed := -1e9, false
		if m.goatVY < 0 {
			for _, c := range [5][2]float64{{0, 0}, {0.45, 0.45}, {-0.45, 0.45}, {0.45, -0.45}, {-0.45, -0.45}} {
				f := float64(w.MobFeetFrom(int(math.Floor(m.x+c[0])), int(math.Floor(m.z+c[1])), int(math.Floor(m.y))))
				if f > feet && m.y <= f && f <= m.y-m.goatVY {
					feet, landed = f, true
				}
			}
		}
		if landed {
			m.y = feet
			m.goatJumping, m.goatVX, m.goatVY, m.goatVZ = false, 0, 0, 0
			m.goatJumpCD = goatJumpCDMin + h.rng.Intn(goatJumpCDMax-goatJumpCDMin+1) // LongJumpMidJump: landed
			h.playSoundDim(players, m.dim, "minecraft:entity.goat.step", sndNeutral, m.x, m.y, m.z, 1, 1)
			h.breezePose(players, m, poseStanding)
			return
		}
	}
	m.vx, m.vz = 0, 0
}

// jumpVectorFor is LongJumpUtil.calculateJumpVectorForAngle over the
// shuffled allowed angles, capped at maxV (the breeze and the goat share it).
func jumpVectorFor(m *mob, tx, ty, tz float64, angles []int, maxV float64, intn func(int) int) (float64, float64, float64, bool) {
	order := append([]int(nil), angles...)
	for i := len(order) - 1; i > 0; i-- {
		j := intn(i + 1)
		order[i], order[j] = order[j], order[i]
	}
	for _, n := range order {
		hx, hz := tx-m.x, tz-m.z
		if hd := math.Hypot(hx, hz); hd > 1e-6 {
			hx, hz = tx-hx/hd*0.5-m.x, tz-hz/hd*0.5-m.z
		}
		f2 := float64(n) * math.Pi / 180
		dir := math.Atan2(hz, hx)
		d2 := hx*hx + hz*hz
		d3 := math.Sqrt(d2)
		d4 := ty - m.y
		d12 := d2 * m.gravity() / (d3*math.Sin(2*f2) - 2*d4*math.Pow(math.Cos(f2), 2))
		if d12 < 0 {
			continue
		}
		d13 := math.Sqrt(d12)
		if d13 > maxV {
			continue
		}
		d14, d15 := d13*math.Cos(f2), d13*math.Sin(f2)
		return d14 * math.Cos(dir) * 0.95, d15 * 0.95, d14 * math.Sin(dir) * 0.95, true
	}
	return 0, 0, 0, false
}
