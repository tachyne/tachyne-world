package server

// PiglinAi's RIDE activity: a baby piglin that has seen a baby hoglin for
// a while (babySometimesRideBabyHoglin — a ticker of ten to forty seconds
// that only counts while one is in sight) takes it as its RIDE_TARGET for
// ten to thirty seconds. Mount walks it over at 0.8 and climbs on within a
// block; a baby piglin climbing a hoglin climbs to the top of whatever baby
// piglins already ride it, two deep (Piglin.startRiding's getTopPassenger),
// so the stacks grow. DismountOrSkipMounting(8) lets go of a vehicle that
// died, went eight or more away or to another world, is not a baby, was
// hurt lately (or the rider was), or is a piglin riding nothing; and with
// the RIDE_TARGET gone, a baby riding a baby gets off (updateActivity).

const (
	piglinRideStartMin  = 200 // RIDE_START_INTERVAL: 10–40 s
	piglinRideStartSpan = 601
	piglinRideMin       = 200 // RIDE_DURATION: 10–30 s
	piglinRideSpan      = 401
	piglinRideSpeed     = 0.8 // Mount.create(0.8)
	piglinRideReach     = 1.0 // CLOSE_ENOUGH_TO_START_RIDING_DIST
	piglinRideKeep      = 8.0 // DismountOrSkipMounting(8, …)
)

// piglinRideTick runs every update for a baby piglin, riding or not: the
// memory's expiry, the dismount checks, and the IDLE behaviour that picks a
// hoglin to ride.
func (h *hub) piglinRideTick(players map[int32]*tracked, m *mob) {
	if m.etype != entityPiglin || !m.baby || m.dying > 0 {
		return
	}
	now := h.tick.Load()
	if m.rideTarget != 0 && now >= m.rideUntil {
		m.rideTarget = 0
	}
	if m.rideTarget == 0 {
		if h.piglinRidingBaby(m) {
			h.piglinDismount(players, m) // updateActivity: stopRiding
		}
		h.piglinMaybePickRide(m, now)
		return
	}
	// DismountOrSkipMounting.
	var v *mob
	if m.mount != 0 {
		v = h.mobs[m.mount]
	} else {
		v = h.mobs[m.rideTarget]
	}
	if v == nil || v.dying > 0 || v.dim != m.dim || dist3sq(v.x, v.y, v.z, m.x, m.y, m.z) >= piglinRideKeep*piglinRideKeep ||
		h.piglinWantsToStopRiding(m, v) {
		h.piglinDismount(players, m)
		m.rideTarget = 0
	}
}

// piglinMaybePickRide is babySometimesRideBabyHoglin, part of the IDLE
// activity: nothing else on its mind, a baby hoglin in sight, and the
// ticker run down.
func (h *hub) piglinMaybePickRide(m *mob, now uint64) {
	if m.hasTarget || m.admireUntil != 0 || m.piglinFlee > 0 || m.celebrateUntil != 0 {
		return
	}
	hog := h.nearestVisibleBabyHoglin(m)
	if hog == nil {
		return // the behaviour's memory condition: the ticker does not run
	}
	// SetEntityLookTargetSometimes.Ticker.tickDownAndCheck, per update.
	if m.rideTicker <= 0 {
		m.rideTicker = piglinRideStartMin + h.rng.Intn(piglinRideStartSpan) - 1
		return
	}
	if m.rideTicker -= mobMoveInterval; m.rideTicker > 0 {
		return
	}
	m.rideTicker = 0
	m.rideTarget = hog.eid
	m.rideUntil = now + uint64(piglinRideMin+h.rng.Intn(piglinRideSpan))
}

// nearestVisibleBabyHoglin is PiglinSpecificSensor's NEAREST_VISIBLE_BABY_HOGLIN.
func (h *hub) nearestVisibleBabyHoglin(m *mob) *mob {
	var best *mob
	bestD := piglinSightRange * piglinSightRange
	h.grid().nearby(m.dim, m.x, m.z, piglinSightRange, func(o *mob) {
		if o.etype != entityHoglin || !o.baby || o.dying > 0 {
			return
		}
		if d := dist3sq(o.x, o.y, o.z, m.x, m.y, m.z); d < bestD && h.mobSeesMob(m, o) {
			best, bestD = o, d
		}
	})
	return best
}

// piglinRideStep is the RIDE activity's Mount(0.8): walk to the ride
// target and climb on once within a block. It reports whether it holds the
// piglin.
func (h *hub) piglinRideStep(players map[int32]*tracked, m *mob) bool {
	if m.etype != entityPiglin || !m.baby || m.rideTarget == 0 || m.mount != 0 ||
		m.hasTarget || m.admireUntil != 0 || m.piglinFlee > 0 || m.celebrateUntil != 0 {
		return false
	}
	v := h.mobs[m.rideTarget]
	if v == nil {
		return false
	}
	if dist3sq(v.x, v.y, v.z, m.x, m.y, m.z) < piglinRideReach*piglinRideReach {
		top := v
		if v.etype == entityHoglin { // Piglin.startRiding: getTopPassenger(vehicle, 3)
			for i := 0; i < 2 && top.mobRider != 0; i++ {
				next := h.mobs[top.mobRider]
				if next == nil {
					break
				}
				top = next
			}
		}
		if top.mobRider == 0 && top != m { // canAddPassenger: nobody on it yet
			h.mountMobOn(players, m, top, false)
		}
		m.vx, m.vz = 0, 0
		return true
	}
	vx, vz := h.pathSteer(m, v.x, v.z)
	m.vx, m.vz = vx*piglinRideSpeed, vz*piglinRideSpeed
	m.yaw = yawToward(m.x, m.z, v.x, v.z)
	m.headYaw = m.yaw
	m.rest = 0
	return true
}

// piglinWantsToStopRiding is PiglinAi.wantsToStopRiding.
func (h *hub) piglinWantsToStopRiding(m, v *mob) bool {
	if !v.baby || v.dying > 0 || m.invulnTicks > 0 || v.invulnTicks > 0 {
		return true
	}
	return v.etype == entityPiglin && v.mount == 0
}

// piglinRidingBaby is PiglinAi.isBabyRidingBaby.
func (h *hub) piglinRidingBaby(m *mob) bool {
	if !m.baby || m.mount == 0 {
		return false
	}
	v := h.mobs[m.mount]
	return v != nil && v.baby && (v.etype == entityPiglin || v.etype == entityHoglin)
}

// piglinDismount is stopRiding.
func (h *hub) piglinDismount(players map[int32]*tracked, m *mob) {
	if m.mount == 0 {
		return
	}
	if v := h.mobs[m.mount]; v != nil {
		h.freeMobSeat(players, v, m.eid)
	}
	m.mount, m.mountDrives, m.navMount = 0, false, nil
}
