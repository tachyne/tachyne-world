package server

// Knockback as motion. LivingEntity.knockback does not move a body: it sets
// its deltaMovement — half of what it had, less the shove, and on the ground
// a hop of min(0.4, dy/2 + power) — and LivingEntity.travel carries it from
// there, tick by tick: gravity and the 0.98 vertical drag, 0.91 of the
// horizontal speed kept each tick in the air, and the floor's friction × 0.91
// once it is down. A zombie punched on flat ground leaves the ground, comes
// down a second later some three blocks off, and skids a little.
//
// A walker's blow lands in its deltaMovement (mobShove, launchKnock) and its
// travel flies the arc (mobtravel.go): a wall stops it, a ledge does not,
// water does not. While it is in the air its goals hold off; once it is
// down they take over again, and the skid runs out under the ground's
// friction while it walks.

const knockFlightMax = 100 // goal updates a flight may last at most (a long fall off a cliff)

// knockLaunches reports whether a blow sets this mob flying on the server:
// a walker, doing nothing that already moves it on its own terms, and not
// held where it is by something that skips its movement (a sitting pet, a
// villager asleep in its bed).
func (h *hub) knockLaunches(m *mob) bool {
	switch {
	case m == h.dragon, m.dying > 0, m.noAI, m.frozen, m.spawnInvuln > 0,
		m.mount != 0, m.cart != 0, m.rider != 0, len(m.riders) > 0, m.hasBody(),
		m.sitting, m.sleeping, isCamelKind(m.etype) && m.camelSitting():
		return false
	}
	return h.travels(m)
}

// knockFlightStep is a goal update of a knocked walker: the goals stay off
// while it is in the air, and it is theirs again once it is down — or in
// water, where the blow is only a push and it swims on.
func (h *hub) knockFlightStep(m *mob) {
	if m.kb == 0 || (m.onGround && m.vy <= 0) || h.inWater(m.dim, m.x, m.y, m.z) {
		m.endKnockFlight()
	}
}

// endKnockFlight hands the body back to its goals.
func (m *mob) endKnockFlight() {
	m.kbFlight, m.kb = false, 0
}
