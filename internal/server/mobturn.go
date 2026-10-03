package server

import "math"

// How a mob turns, and how its viewers hear about it.
//
//   - The body (yRot) turns to the way the goals want to go through the move
//     control, at most 90° a tick (MoveControl.MAX_TURN, rotlerp) — ten for
//     the smooth swimmers (SmoothSwimmingMoveControl's maxTurnY: a frog, a
//     tadpole, a dolphin, an axolotl, a nautilus). A walker walks the way it
//     faces (moveRelative turns zza by yRot), so turning round takes it two
//     ticks, not none.
//   - The head (yHeadRot) turns to what it watches through the look control,
//     getHeadRotSpeed a tick (10; an attack goal looks at its target at
//     30), back to the body at 10 when it watches nothing, and while the mob
//     is on its way somewhere it stays within getMaxHeadYRot (75) of the body
//     (LookControl.clampHeadRotationToBody).
//   - The tracker (ServerEntity.sendChanges) tells the viewers every
//     updateInterval ticks — 3 for a mob, 2 for an allay — counted from when
//     tracking began: the position when it moved at least the codec's step
//     (7.6293945E-6 squared) or every 60 ticks regardless, the body's facing
//     when its packed byte changed, and the head's the same way.

// rotlerp is MoveControl.rotlerp: from a toward b by at most max degrees.
func rotlerp(a, b, max float32) float32 {
	d := wrapDeg(b - a)
	if d > max {
		d = max
	} else if d < -max {
		d = -max
	}
	return wrapDeg(a + d)
}

// wrapDeg is Mth.wrapDegrees: an angle in [-180, 180).
func wrapDeg(v float32) float32 {
	f := math.Mod(float64(v), 360)
	if f >= 180 {
		f -= 360
	} else if f < -180 {
		f += 360
	}
	return float32(f)
}

// packDeg is Mth.packDegrees: the byte an angle goes out as.
func packDeg(v float32) int8 {
	return int8(int32(math.Floor(float64(v) * 256 / 360)))
}

// moveTurnFor is the move control's turn a tick for a species.
func moveTurnFor(etype int) float32 {
	switch etype {
	case entityFrog, entityTadpole, entityDolphin, entityAxolotl, entityNautilus, entityZombieNautilus:
		return 10 // SmoothSwimmingMoveControl(…, 85, 10, …)
	}
	return 90 // MoveControl.MAX_TURN
}

// headRotSpeed is how far the head turns a tick toward what the mob watches
// (Mob.getHeadRotSpeed, or the 30 an attack goal's setLookAt passes).
func headRotSpeed(m *mob) float32 {
	switch m.etype {
	case entityFrog:
		return 35
	case entityBreeze:
		return 25
	}
	if m.hostile && m.hasTarget {
		return 30 // MeleeAttackGoal / RangedBowAttackGoal: setLookAt(target, 30, 30)
	}
	return 10
}

// maxHeadYRot is Mob.getMaxHeadYRot: how far the head may sit off the body.
func maxHeadYRot(m *mob) float32 {
	switch m.etype {
	case entityGoat:
		return 15
	case entitySniffer:
		return 50
	case entityDolphin, entityAxolotl:
		return 1
	case entityArmadillo:
		if m.armState != 0 {
			return 0
		}
		return 32
	case entityFrog:
		return 5
	case entityShulker:
		return 180
	case entityRavager:
		return 45
	case entityCamel, entityCamelHusk, entityBreeze:
		return 30
	}
	return 75
}

// turnBody is the move control's turn: the body toward the goals' heading,
// by at most the species' turn a tick. Nothing turns it while it stands,
// rides a blow or a spring, or is anchored.
func (m *mob) turnBody() {
	if m.statik || m.kbFlight || m.springing() || m.etype == entityVex || (m.vx == 0 && m.vz == 0) {
		return
	}
	want := float32(math.Atan2(-m.vx, m.vz) * 180 / math.Pi)
	m.yaw = rotlerp(m.yaw, want, moveTurnFor(m.etype))
}

// facing is the unit vector the body faces (yRot's forward).
func (m *mob) facing() (float64, float64) {
	r := float64(m.yaw) * math.Pi / 180
	return -math.Sin(r), math.Cos(r)
}

// lookTick is LookControl.tick: the head toward what the goals have it
// watch (m.headYaw, the body when nothing), at the look speed, held near
// the body while the mob is going somewhere.
func (m *mob) lookTick() {
	if !m.lookInit {
		m.lookYaw, m.lookInit = m.headYaw, true
		return
	}
	sp := headRotSpeed(m)
	d := wrapDeg(m.headYaw - m.lookYaw)
	if d > sp {
		d = sp
	} else if d < -sp {
		d = -sp
	}
	m.lookYaw = wrapDeg(m.lookYaw + d)
	if m.vx != 0 || m.vz != 0 { // clampHeadRotationToBody: the navigation is not done
		lim := maxHeadYRot(m)
		if off := wrapDeg(m.lookYaw - m.yaw); off > lim {
			m.lookYaw = wrapDeg(m.yaw + lim)
		} else if off < -lim {
			m.lookYaw = wrapDeg(m.yaw - lim)
		}
	}
}

// snapLook sets the head where it is wanted at once (a command, a
// teleport): no turn for the viewers to watch.
func (m *mob) snapLook() {
	m.lookYaw, m.lookInit = m.headYaw, true
	m.sheadYaw = m.headYaw
}

// mobUpdateInterval is the tracker's updateInterval for a mob's type.
func mobUpdateInterval(etype int) int32 {
	if etype == entityAllay {
		return 2
	}
	return 3
}

// mobTracked reports whether the tracker pass sends this mob's movement:
// not a mob something else moves and tells of (the dragon, a mount a
// player rides, a passenger carried on another mob or a cart, a cube
// carrying a block) nor one playing out its death.
func (h *hub) mobTracked(m *mob) bool {
	switch {
	case m == h.dragon, m.dying > 0, m.health <= 0:
		return false
	case m.rider != 0, len(m.riders) > 0:
		return false
	case m.mount != 0 && !m.mountDrives, m.cart != 0:
		return false
	case m.hasBody():
		return false
	}
	if m.mobRider != 0 {
		if r := h.mobs[m.mobRider]; r != nil && r.mountDrives {
			return false
		}
	}
	return true
}

// mobTrack is one tick of a mob's ServerEntity.sendChanges.
func (h *hub) mobTrack(players map[int32]*tracked, m *mob) {
	if !h.mobTracked(m) {
		return
	}
	m.lookTick()
	n := m.trTicks
	m.trTicks++
	if n%mobUpdateInterval(m.etype) != 0 {
		return
	}
	dx, dy, dz := m.x-m.sx, m.y-m.sy, m.z-m.sz
	moved := dx*dx+dy*dy+dz*dz >= 7.6293945e-6 || n%60 == 0
	if moved || packDeg(m.yaw) != packDeg(m.syaw) {
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, 0, m.grounded()))
		m.sx, m.sy, m.sz, m.syaw = m.x, m.y, m.z, m.yaw
	}
	if packDeg(m.lookYaw) != packDeg(m.sheadYaw) {
		m.sheadYaw = m.lookYaw
		h.toTracking(players, m.eid, m.dim, m.x, m.z, entHead(m.eid, m.lookYaw))
	}
}
