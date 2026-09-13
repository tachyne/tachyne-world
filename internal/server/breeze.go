package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The breeze's fight (BreezeAi: Slide, LongJump, ShootWhenStuck, Shoot):
// it never walks at you. It slides to a spot behind you (or away, if you
// are within four blocks), draws breath and long-jumps to a point four to
// eight blocks behind you on a forty-to-eighty-degree arc, lands, and in
// the hundred ticks after a landing inhales for fifteen and fires a wind
// charge at you (four to recover, ten before the next); if it cannot jump
// it shoots from where it stands. Poses drive the client's animation.

const (
	poseLongJumping = 6
	poseShooting    = 16
	poseInhaling    = 17

	brzStanding = iota
	brzInhaling
	brzJumping
	brzShooting

	breezeShootRangeSq = 256.0 // Shoot: within 16
	breezeShootCharge  = 15    // SHOOT_INITIAL_DELAY_TICKS
	breezeShootRecover = 4     // SHOOT_RECOVER_DELAY_TICKS
	breezeShootCD      = 10    // SHOOT_COOLDOWN_TICKS
	breezeShootWindow  = 100   // BREEZE_SHOOT memory after a landing
	breezeInhale       = 10    // INHALING_DURATION_TICKS
	breezeJumpCD       = 10    // JUMP_COOLDOWN_TICKS (2 when hurt)
	breezeJumpCDHurt   = 2
	breezeTooClose     = 4.0 // tooCloseForJump
	breezeJumpVelMul   = 0.058333334
	breezeSlideSpeed   = 0.6
	breezeInnerCircle  = 4.0
	mobGravity         = 0.08
)

var breezeJumpAngles = []int{40, 55, 60, 75, 80} // ALLOWED_ANGLES

func (h *hub) breezePose(players map[int32]*tracked, m *mob, pose int32) {
	h.toNearbyEv(players, m.dim, m.x, m.z, metaEv(poseMeta(m.eid, pose)))
}

// breezeStep is the fight's tick: returns whether it holds the breeze.
func (h *hub) breezeStep(players map[int32]*tracked, m *mob) bool {
	if m.brzJumpCD > 0 {
		m.brzJumpCD -= mobMoveInterval
	}
	if m.brzShootCD > 0 {
		m.brzShootCD -= mobMoveInterval
	}
	if m.brzShootWindow > 0 {
		m.brzShootWindow -= mobMoveInterval
	}
	switch m.brzState {
	case brzJumping:
		return true // in the air: breezeFlight moves it
	case brzInhaling:
		m.vx, m.vz = 0, 0
		m.brzTicks += mobMoveInterval
		if m.brzTicks < breezeInhale {
			return true
		}
		vx, vy, vz, ok := h.breezeJumpVector(m, m.brzJumpX, m.brzJumpY, m.brzJumpZ)
		if !ok {
			m.brzState = brzStanding
			h.breezePose(players, m, poseStanding)
			return true
		}
		h.playSoundDim(players, m.dim, "minecraft:entity.breeze.jump", sndHostile, m.x, m.y, m.z, 1, 1)
		m.brzState, m.brzVX, m.brzVY, m.brzVZ = brzJumping, vx, vy, vz
		m.yaw = float32(math.Atan2(-vx, vz) * 180 / math.Pi)
		h.breezePose(players, m, poseLongJumping)
		return true
	case brzShooting:
		m.vx, m.vz = 0, 0
		t := h.nearestHuntable(players, m.dim, m.x, m.z, float64(m.followRange()))
		if t == nil {
			h.breezeStopShooting(players, m)
			return false
		}
		m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi)
		was := m.brzTicks
		m.brzTicks += mobMoveInterval
		if was < breezeShootCharge && m.brzTicks >= breezeShootCharge {
			h.breezeFire(players, m, t)
		}
		if m.brzTicks >= breezeShootCharge+breezeShootRecover {
			h.breezeStopShooting(players, m)
		}
		return true
	}
	// Standing: a target within follow range, or nothing to do.
	t := h.nearestHuntable(players, m.dim, m.x, m.z, float64(m.followRange()))
	if t == nil {
		m.brzSlide = false
		return false
	}
	m.hasTarget, m.tx, m.tz = true, t.x, t.z
	d2 := (t.x-m.x)*(t.x-m.x) + (t.z-m.z)*(t.z-m.z)
	// Shoot: after a landing (or stuck), from standing, within sixteen.
	if m.brzShootWindow > 0 && m.brzShootCD <= 0 && !m.brzSlide {
		if d2 < breezeShootRangeSq {
			m.brzState, m.brzTicks = brzShooting, 0
			h.breezePose(players, m, poseShooting)
			h.playSoundDim(players, m.dim, "minecraft:entity.breeze.inhale", sndHostile, m.x, m.y, m.z, 1, 1)
			m.vx, m.vz = 0, 0
			return true
		}
		m.brzShootWindow = 0 // out of range: the memory is erased
	}
	// Slide: walk to the chosen spot, then reconsider.
	if m.brzSlide {
		dx, dz := m.brzSlideX-m.x, m.brzSlideZ-m.z
		if math.Hypot(dx, dz) > 1 && m.brzSlideTicks > 0 {
			m.brzSlideTicks -= mobMoveInterval
			h.steerTo(m, m.brzSlideX, m.brzSlideZ, breezeSlideSpeed)
			return true
		}
		m.brzSlide = false
	}
	// LongJump: not too close, the jump cooldown over, four clear blocks
	// above, a landing spot behind the target.
	if m.brzJumpCD <= 0 && m.brzShootWindow <= 0 && math.Sqrt(d2) > breezeTooClose && h.breezeCanJumpFrom(m) {
		if jx, jy, jz, ok := h.breezeJumpTarget(m, t); ok {
			m.brzState, m.brzTicks = brzInhaling, 0
			m.brzJumpX, m.brzJumpY, m.brzJumpZ = jx, jy, jz
			m.yaw = float32(math.Atan2(-(jx-m.x), jz-m.z) * 180 / math.Pi)
			h.breezePose(players, m, poseInhaling)
			h.playSoundDim(players, m.dim, "minecraft:entity.breeze.charge", sndHostile, m.x, m.y, m.z, 1, 1)
			m.vx, m.vz = 0, 0
			return true
		}
		m.brzShootWindow = breezeShootWindow // ShootWhenStuck
		return true
	}
	if m.brzShootWindow > 0 {
		m.vx, m.vz = 0, 0 // waiting on the shoot cooldown
		return true
	}
	// Slide.start: away if within the inner circle, else behind the target
	// or into the middle circle.
	if d2 < breezeInnerCircle*breezeInnerCircle {
		if fx, fz, ok := h.posAwayFrom(m, t.x, t.z); ok {
			m.brzSlide, m.brzSlideX, m.brzSlideZ, m.brzSlideTicks = true, fx, fz, 100
			return true
		}
	}
	if h.rng.Intn(2) == 0 {
		m.brzSlideX, m.brzSlideZ = breezePointBehind(h, t)
	} else {
		d := math.Sqrt(d2) - (8 - h.rng.Float64()*4) // randomPointInMiddleCircle: lerp(rand, 8, 4) short of the target
		if hd := math.Sqrt(d2); hd > 1e-6 {
			m.brzSlideX, m.brzSlideZ = m.x+(t.x-m.x)/hd*d, m.z+(t.z-m.z)/hd*d
		}
	}
	m.brzSlide, m.brzSlideTicks = true, 100
	return true
}

// breezePointBehind is BreezeUtil.randomPointBehindTarget: four to eight
// blocks behind the target's head, spread by a gaussian of forty-five degrees.
func breezePointBehind(h *hub, t *tracked) (float64, float64) {
	yaw := float64(t.yaw) + 180 + h.rng.NormFloat64()*45
	dist := 4 + h.rng.Float64()*4
	rad := yaw * math.Pi / 180
	return t.x - math.Sin(rad)*dist, t.z + math.Cos(rad)*dist
}

// breezeCanJumpFrom is canJumpFromCurrentPosition: on the ground, four
// clear blocks above.
func (h *hub) breezeCanJumpFrom(m *mob) bool {
	w := h.worldFor(m.dim)
	fx, fy, fz := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
	for i := 1; i <= 4; i++ {
		if s := w.At(fx, fy+i, fz); s != worldgen.Air && !worldgen.IsWater(s) {
			return false
		}
	}
	return true
}

// breezeJumpTarget picks a landing spot behind the target snapped to the
// ground (snapToSurface), refusing a dangerous floor.
func (h *hub) breezeJumpTarget(m *mob, t *tracked) (float64, float64, float64, bool) {
	w := h.worldFor(m.dim)
	px, pz := breezePointBehind(h, t)
	bx, bz := int(math.Floor(px)), int(math.Floor(pz))
	feet := w.MobFeetFrom(bx, bz, int(math.Floor(t.y)))
	if d := feet - int(math.Floor(t.y)); d > 10 || d < -10 {
		return 0, 0, 0, false
	}
	below := w.At(bx, feet-1, bz)
	if !worldgen.Collides(below) || worldgen.IsLava(below) || below == worldgen.Cactus || isFire(below) { // isBlockDangerous
		return 0, 0, 0, false
	}
	return float64(bx) + 0.5, float64(feet), float64(bz) + 0.5, true
}

// breezeJumpVector is LongJumpUtil.calculateJumpVectorForAngle over the
// shuffled allowed angles, capped at 0.0583 × FOLLOW_RANGE (1.4).
func (h *hub) breezeJumpVector(m *mob, tx, ty, tz float64) (float64, float64, float64, bool) {
	maxV := breezeJumpVelMul * float64(m.followRange())
	angles := append([]int(nil), breezeJumpAngles...)
	h.rng.Shuffle(len(angles), func(i, j int) { angles[i], angles[j] = angles[j], angles[i] })
	for _, n := range angles {
		// The target is pulled half a block toward the mob.
		hx, hz := tx-m.x, tz-m.z
		if hd := math.Hypot(hx, hz); hd > 1e-6 {
			hx, hz = tx-hx/hd*0.5-m.x, tz-hz/hd*0.5-m.z
		}
		f2 := float64(n) * math.Pi / 180
		dir := math.Atan2(hz, hx)
		d2 := hx*hx + hz*hz
		d3 := math.Sqrt(d2)
		d4 := ty - m.y
		d12 := d2 * mobGravity / (d3*math.Sin(2*f2) - 2*d4*math.Pow(math.Cos(f2), 2))
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

// breezeFlight is the jump's arc, in place of the walk: gravity per tick,
// landing when it comes down on the floor.
func (h *hub) breezeFlight(players map[int32]*tracked, m *mob) {
	w := h.worldFor(m.dim)
	for i := 0; i < mobMoveInterval; i++ {
		m.brzVY -= mobGravity
		nx, nz := m.x+m.brzVX, m.z+m.brzVZ
		if h.ownedAt(nx, nz) && !worldgen.Collides(w.At(int(math.Floor(nx)), int(math.Floor(m.y)), int(math.Floor(nz)))) {
			m.x, m.z = nx, nz
		} else {
			m.brzVX, m.brzVZ = 0, 0
		}
		m.y += m.brzVY
		feet := float64(w.MobFeetFrom(int(math.Floor(m.x)), int(math.Floor(m.z)), int(math.Floor(m.y))))
		if m.brzVY < 0 && m.y <= feet {
			m.y = feet
			h.breezeLand(players, m)
			return
		}
	}
	m.vx, m.vz = 0, 0
}

// breezeLand is isFinishedJumping: the landing sound, a jump cooldown
// (short if hurt), and a hundred-tick window to shoot.
func (h *hub) breezeLand(players map[int32]*tracked, m *mob) {
	m.brzState, m.brzVX, m.brzVY, m.brzVZ = brzStanding, 0, 0, 0
	h.playSoundDim(players, m.dim, "minecraft:entity.breeze.land", sndHostile, m.x, m.y, m.z, 1, 1)
	h.breezePose(players, m, poseStanding)
	m.brzJumpCD = breezeJumpCD
	if m.kb > 0 {
		m.brzJumpCD = breezeJumpCDHurt
	}
	m.brzShootWindow = breezeShootWindow
	m.vx, m.vz = 0, 0
}

// breezeFire is Shoot.tick past the charge: a wind charge at 0.7 toward a
// point a third of the way up the target, with vanilla's spread.
func (h *hub) breezeFire(players map[int32]*tracked, m *mob, t *tracked) {
	ux, uy, uz := aimAt(m.x, m.y+1, m.z, t.x, t.y+0.3, t.z)
	v := 1.4                                                // 0.7 per tick, two ticks an update
	spread := float64(5-int(h.rules.Difficulty)*4) * 0.0075 // Projectile.shoot inaccuracy × 0.0075 per unit
	if spread < 0 {
		spread = 0
	}
	ux += h.rng.NormFloat64() * spread
	uy += h.rng.NormFloat64() * spread
	uz += h.rng.NormFloat64() * spread
	a := h.launchProjectileIn(players, entityWindCharge, m.dim, m.x, m.y+1, m.z, ux*v, uy*v, uz*v)
	a.shooter = m.eid
	h.playSoundDim(players, m.dim, "minecraft:entity.breeze.shoot", sndHostile, m.x, m.y, m.z, 1.5, 1)
}

func (h *hub) breezeStopShooting(players map[int32]*tracked, m *mob) {
	m.brzState, m.brzTicks = brzStanding, 0
	h.breezePose(players, m, poseStanding)
	m.brzShootCD = breezeShootCD
	m.brzShootWindow = 0
}
