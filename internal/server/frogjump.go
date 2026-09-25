package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// FrogAi's LONG_JUMP activity (LongJumpToPreferredBlock + LongJumpMidJump):
// every five to seven seconds a frog on dry ground, not tempted and not
// courting, looks over every cell within four across and two up or down,
// taking the farther ones first by weight, for somewhere to land that it
// cannot simply walk to — half the time holding out for a lily pad or big
// dripleaf (#frog_prefer_jump_to). It crouches for forty ticks, leaps on a
// steep arc with its long-jump croak, and lands with a step and a fresh
// cooldown. Where it cannot start (in water or lava, on honey) the cooldown
// is halved and rolled again.

const (
	frogJumpCDMin    = 100 // TIME_BETWEEN_LONG_JUMPS
	frogJumpCDMax    = 140
	frogJumpWidth    = 4 // MAX_LONG_JUMP_WIDTH
	frogJumpHeight   = 2 // MAX_LONG_JUMP_HEIGHT
	frogJumpPrefer   = 0.5
	frogJumpPrepare  = 40
	frogJumpMaxSpeed = 3.5714288
)

// frogPrefersJumpTo is #frog_prefer_jump_to.
func frogPrefersJumpTo(s uint32) bool {
	return isLilyPad(s) || (s >= bigDripleafMin && s <= bigDripleafMax)
}

// frogLandingOK is FrogAi.isAcceptableLandingSpot: no fluid in or about the
// cell, and either a preferred block there or under it, or ordinary
// standing room on a solid floor.
func (h *hub) frogLandingOK(m *mob, p blockPos) bool {
	w := h.worldFor(m.dim)
	at, below, above := w.At(p.x, p.y, p.z), w.At(p.x, p.y-1, p.z), w.At(p.x, p.y+1, p.z)
	if worldgen.HoldsWater(at) || worldgen.HoldsWater(below) || worldgen.HoldsWater(above) ||
		worldgen.IsLava(at) || worldgen.IsLava(below) || worldgen.IsLava(above) {
		return false
	}
	if frogPrefersJumpTo(at) || frogPrefersJumpTo(below) {
		return true
	}
	return worldgen.Collides(below) && !worldgen.Collides(at) && !worldgen.Collides(above)
}

// frogJumpCandidate is start + pickCandidate: the cells weighted by their
// squared distance, drawn without replacement until one will do.
func (h *hub) frogJumpCandidate(m *mob) (float64, float64, float64, bool) {
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	type cand struct {
		p blockPos
		w int
	}
	var cands []cand
	total := 0
	for x := bx - frogJumpWidth; x <= bx+frogJumpWidth; x++ {
		for y := by - frogJumpHeight; y <= by+frogJumpHeight; y++ {
			for z := bz - frogJumpWidth; z <= bz+frogJumpWidth; z++ {
				if x == bx && y == by && z == bz {
					continue
				}
				wt := sqI(x-bx) + sqI(y-by) + sqI(z-bz)
				cands = append(cands, cand{blockPos{x, y, z}, wt})
				total += wt
			}
		}
	}
	wantPreferred := h.rng.Float32() < frogJumpPrefer
	w := h.worldFor(m.dim)
	var fallback []cand
	try := func(c cand) (float64, float64, float64, bool) {
		if c.p.x == bx && c.p.z == bz || !h.frogLandingOK(m, c.p) {
			return 0, 0, 0, false
		}
		tx, ty, tz := float64(c.p.x)+0.5, float64(c.p.y)+0.5, float64(c.p.z)+0.5 // Vec3.atCenterOf
		if _, _, _, ok := jumpVectorFor(m, tx, ty, tz, goatJumpAngles, frogJumpMaxSpeed, h.rng.Intn); !ok {
			return 0, 0, 0, false
		}
		if h.walkableLine(m, tx, float64(c.p.y), tz) {
			return 0, 0, 0, false // a path reaches it: no need to jump
		}
		return tx, ty, tz, true
	}
	for len(cands) > 0 && total > 0 {
		r := h.rng.Intn(total)
		i := 0
		for ; i < len(cands)-1 && r >= cands[i].w; i++ {
			r -= cands[i].w
		}
		c := cands[i]
		cands[i] = cands[len(cands)-1]
		cands = cands[:len(cands)-1]
		total -= c.w
		if wantPreferred && !frogPrefersJumpTo(w.At(c.p.x, c.p.y-1, c.p.z)) {
			fallback = append(fallback, c)
			continue
		}
		if tx, ty, tz, ok := try(c); ok {
			return tx, ty, tz, true
		}
	}
	for _, c := range fallback {
		if tx, ty, tz, ok := try(c); ok {
			return tx, ty, tz, true
		}
	}
	return 0, 0, 0, false
}

// frogJumpStep runs each mob update. Returns whether it holds the frog.
func (h *hub) frogJumpStep(players map[int32]*tracked, m *mob) bool {
	if m.goatJumping {
		return true // the arc (goatFlight) carries it
	}
	if !m.goatJumpSet { // initMemories: a first cooldown
		m.goatJumpCD, m.goatJumpSet = frogJumpCDMin+h.rng.Intn(frogJumpCDMax-frogJumpCDMin+1), true
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
		vx, vy, vz, ok := jumpVectorFor(m, m.goatJumpX, m.goatJumpY, m.goatJumpZ, goatJumpAngles, frogJumpMaxSpeed, h.rng.Intn)
		if !ok {
			m.goatJumpCD = frogJumpCDMin + h.rng.Intn(frogJumpCDMax-frogJumpCDMin+1)
			return false
		}
		h.playSoundDim(players, m.dim, "minecraft:entity.frog.long_jump", sndNeutral, m.x, m.y, m.z, 1, 1)
		m.goatJumping, m.goatVX, m.goatVY, m.goatVZ = true, vx, vy, vz
		m.yaw = float32(math.Atan2(-vx, vz) * 180 / math.Pi)
		h.breezePose(players, m, poseLongJumping)
		return true
	}
	if m.goatJumpCD > 0 || m.tempted || m.loveTicks > 0 || m.panic > 0 || m.hasTarget || m.baby {
		return false
	}
	w := h.worldFor(m.dim)
	if h.inWater(m.dim, m.x, m.y, m.z) || worldgen.IsLava(w.At(floorInt(m.x), floorInt(m.y), floorInt(m.z))) ||
		w.At(floorInt(m.x), floorInt(m.y), floorInt(m.z)) == honeyBlockState {
		m.goatJumpCD = (frogJumpCDMin + h.rng.Intn(frogJumpCDMax-frogJumpCDMin+1)) / 2 // checkExtraStartConditions
		return false
	}
	tx, ty, tz, ok := h.frogJumpCandidate(m)
	if !ok {
		m.goatJumpCD = frogJumpCDMin + h.rng.Intn(frogJumpCDMax-frogJumpCDMin+1)
		return false
	}
	m.goatJumpX, m.goatJumpY, m.goatJumpZ = tx, ty, tz
	m.goatPrep = frogJumpPrepare
	m.yaw = float32(math.Atan2(-(tx-m.x), tz-m.z) * 180 / math.Pi)
	m.vx, m.vz = 0, 0
	return true
}
