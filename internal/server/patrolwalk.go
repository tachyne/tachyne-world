package server

import "math"

// LongDistancePatrolGoal (PatrollingMonster): a pillager patrol walks to a
// point up to five hundred blocks away, the captain leading and the rest
// following the waypoints it hands back. Patrols used to spawn and then amble
// on the wander goal, so they never crossed the country the way vanilla's do —
// and never arrived anywhere.

const (
	patrolCompanionRange = 16  // getBoundingBox().inflate(16)
	patrolLegLength      = 10  // the waypoint is ten blocks along the route
	patrolArrived        = 10  // closerToCenterThan(position, 10): pick a new target
	patrolWaypointReach  = 2.0 // navigation.isDone(): near enough to plot the next leg
	patrolFailCooldown   = 200 // NAVIGATION_FAILED_COOLDOWN
	patrolTargetSpan     = 1000
	patrolTargetOffset   = 500
)

// canJoinPatrol is Raider.canJoinPatrol: any raider not already in a raid —
// the illagers and the ravager. (A raider that belongs to a raid is walking
// to that raid instead, which is the higher-priority goal.)
func canJoinPatrol(m *mob) bool {
	return (isIllager(m.etype) || m.etype == entityRavager) &&
		m.dying == 0 && m.raidCenter == (blockPos{})
}

// findPatrolTarget picks a point up to five hundred blocks out and starts the
// mob patrolling toward it.
func (h *hub) findPatrolTarget(m *mob) {
	m.patrolTarget = blockPos{
		x: floorInt(m.x) - patrolTargetOffset + h.rng.Intn(patrolTargetSpan),
		y: floorInt(m.y),
		z: floorInt(m.z) - patrolTargetOffset + h.rng.Intn(patrolTargetSpan),
	}
	m.patrolling = true
}

// setPatrolTarget is the follower's side: being handed a waypoint is what
// starts a patrol member patrolling.
func setPatrolTarget(m *mob, p blockPos) {
	m.patrolTarget = p
	m.patrolling = true
}

// patrolStep steers a patrolling illager along its route. Reports whether it
// took the mob's movement this tick.
func (h *hub) patrolStep(players map[int32]*tracked, m *mob) bool {
	now := h.tick.Load()
	if !m.patrolling || m.hasTarget || m.mount != 0 || m.rider != 0 ||
		m.patrolTarget == (blockPos{}) || now < m.patrolCooldown {
		return false
	}
	// navigation.isDone(): the current leg is walked, so plot the next one.
	if m.patrolLeg == (blockPos{}) ||
		math.Hypot(float64(m.patrolLeg.x)+0.5-m.x, float64(m.patrolLeg.z)+0.5-m.z) <= patrolWaypointReach {
		if !h.plotPatrolLeg(m) {
			return false
		}
	}
	m.vx, m.vz = h.pathSteer(m, float64(m.patrolLeg.x)+0.5, float64(m.patrolLeg.z)+0.5)
	return true
}

// plotPatrolLeg is the body of LongDistancePatrolGoal.tick: disband if the
// patrol has scattered, hand the captain's next waypoint to its companions,
// and pick a fresh target once the captain has arrived. Reports whether there
// is a leg to walk.
func (h *hub) plotPatrolLeg(m *mob) bool {
	companions := h.patrolCompanions(m)
	if len(companions) == 0 {
		m.patrolling = false // the last of the patrol: it stops being one
		return false
	}
	tx, tz := float64(m.patrolTarget.x)+0.5, float64(m.patrolTarget.z)+0.5
	if m.patrolCaptain && math.Hypot(tx-m.x, tz-m.z) < patrolArrived {
		h.findPatrolTarget(m) // arrived: somewhere else, then
		m.patrolLeg = blockPos{}
		return false
	}
	// The route bends: the leg aims at the target displaced a little
	// sideways, which is what keeps a patrol from walking a dead-straight
	// line across the world.
	dx, dz := m.x-tx, m.z-tz
	// yRot(90°): (x, z) -> (z, -x), scaled by 0.4 and added back to the target.
	bx, bz := tx+dz*0.4, tz-dx*0.4
	ox, oz := bx-m.x, bz-m.z
	d := math.Hypot(ox, oz)
	if d < 1e-6 {
		h.findPatrolTarget(m)
		m.patrolLeg = blockPos{}
		return false
	}
	lx := floorInt(m.x + ox/d*patrolLegLength)
	lz := floorInt(m.z + oz/d*patrolLegLength)
	w := h.worldFor(m.dim)
	// getHeightmapPos + navigation.moveTo: vanilla always HAS a position and
	// only gives up when no path reaches it. The engine's pather falls back to
	// straight steering rather than reporting failure, so the one thing worth
	// checking is that there is ground to stand on at all — void, deep water
	// or a sheer face means no leg, and the goal rests for the same 200 ticks
	// vanilla waits after a failed moveTo.
	if !w.Walkable(lx, lz) {
		m.patrolCooldown = h.tick.Load() + patrolFailCooldown
		m.patrolLeg = blockPos{}
		return false
	}
	leg := blockPos{lx, w.MobFeet(lx, lz), lz}
	m.patrolLeg = leg
	if m.patrolCaptain {
		for _, c := range companions {
			setPatrolTarget(c, leg)
			c.patrolLeg = blockPos{}
		}
	}
	return true
}

// patrolCompanions is findPatrolCompanions: the other patrol-capable illagers
// within sixteen blocks.
func (h *hub) patrolCompanions(m *mob) []*mob {
	var out []*mob
	for _, o := range h.mobs {
		if o == m || o.dim != m.dim || !canJoinPatrol(o) {
			continue
		}
		if math.Abs(o.x-m.x) <= patrolCompanionRange && math.Abs(o.y-m.y) <= patrolCompanionRange &&
			math.Abs(o.z-m.z) <= patrolCompanionRange {
			out = append(out, o)
		}
	}
	return out
}
