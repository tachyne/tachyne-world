package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Knockback as motion. LivingEntity.knockback does not move a body: it sets
// its deltaMovement — half of what it had, less the shove, and on the ground
// a hop of min(0.4, dy/2 + power) — and LivingEntity.travel carries it from
// there, tick by tick: gravity and the 0.98 vertical drag, 0.91 of the
// horizontal speed kept each tick in the air, and the floor's friction × 0.91
// once it is down. A zombie punched on flat ground leaves the ground, comes
// down a second later some three blocks off, and skids a little.
//
// The engine used to tell the client about the hop (mobKnockVelocity) while
// keeping the body on the floor and walking it back under a fast decay, so
// the server's mob went under a block for the client's two or three. A
// launched walker now flies the arc on the server too: the vertical
// integrator (mobairborne.go) carries the hop, knockFlightStep the
// horizontal speed, and knockStepOK the collision — a wall stops it, a ledge
// does not, water does not.

const (
	knockAirFriction = 0.91 // travelInAir: the air's horizontal friction
	knockFlightMax   = 100  // updates a flight may last at most (a long fall off a cliff)
	knockRestSpeed   = 0.003
)

// knockLaunches reports whether a blow sets this mob flying on the server:
// a walker on land, doing nothing that already moves it on its own terms,
// and not held where it is by something that skips its movement (a sitting
// pet, a villager asleep in its bed).
func (h *hub) knockLaunches(m *mob) bool {
	switch {
	case m == h.dragon, m.dying > 0, m.flies, m.swims, m.statik, m.geyserFly,
		m.leaping, m.goatJumping, m.noAI, m.frozen, m.spawnInvuln > 0,
		m.mount != 0, m.cart != 0, m.rider != 0, len(m.riders) > 0, m.hasBody(),
		m.sitting, m.sleeping, isCamelKind(m.etype) && m.camelSitting(),
		m.etype == entityVex, m.etype == entityBreeze && m.brzState == brzJumping:
		return false
	}
	return !h.inWater(m.dim, m.x, m.y, m.z)
}

// knockFriction is travelInAir's horizontal friction for one tick: 0.91 in
// the air, the floor's own friction × 0.91 on it.
func (h *hub) knockFriction(m *mob) float64 {
	if m.airborne {
		return knockAirFriction
	}
	w := h.worldFor(m.dim)
	return blockFriction(w.At(floorInt(m.x), floorInt(m.y)-1, floorInt(m.z))) * knockAirFriction
}

// knockFlightStep sets this update's step from the flight's per-tick speed —
// the mobMoveInterval ticks it covers, each slowed by the friction — and
// spends that friction on the speed. The flight ends once the body is down
// and has skidded to a stop.
func (h *hub) knockFlightStep(m *mob) {
	f := h.knockFriction(m)
	step, keep := 0.0, 1.0
	for i := 0; i < mobMoveInterval; i++ {
		step += keep
		keep *= f
	}
	m.vx, m.vz = m.kvx*step, m.kvz*step
	m.kvx, m.kvz = m.kvx*keep, m.kvz*keep
	if m.kb == 0 || (!m.airborne && math.Hypot(m.kvx, m.kvz) < knockRestSpeed) {
		m.endKnockFlight()
	}
}

// endKnockFlight hands the body back to its goals.
func (m *mob) endKnockFlight() {
	m.kbFlight, m.kb = false, 0
	m.kvx, m.kvz = 0, 0
}

// knockStepOK is the collision a knocked body meets: the cells its box
// would fill in the next column must be free (a ledge, water or a hazard
// does not stop it — only something solid). On the ground it steps up
// nothing; in the air it cannot climb at all.
func (h *hub) knockStepOK(m *mob, nx, nz float64) bool {
	fnx, fnz := floorInt(nx), floorInt(nz)
	if fnx == floorInt(m.x) && fnz == floorInt(m.z) {
		return true
	}
	w := h.worldFor(m.dim)
	fy := floorInt(m.y)
	if !h.bodyFits(m, fnx, fy, fnz) || worldgen.IsTallCollision(w.At(fnx, fy, fnz)) {
		return false
	}
	// A fence, wall or gate below reaches half a block into the feet's cell.
	if m.y-float64(fy) < 0.5 && worldgen.IsTallCollision(w.At(fnx, fy-1, fnz)) {
		return false
	}
	return true
}

// knockHitsWall is Entity.collide for a knocked body that met a wall: the
// axis that hits it loses its speed, and the other carries on along it.
func (h *hub) knockHitsWall(m *mob, nx, nz float64) {
	switch {
	case h.knockStepOK(m, nx, m.z) && h.ownedAt(nx, m.z):
		m.x = nx
		m.vz, m.kvz = 0, 0
	case h.knockStepOK(m, m.x, nz) && h.ownedAt(m.x, nz):
		m.z = nz
		m.vx, m.kvx = 0, 0
	default:
		m.vx, m.vz, m.kvx, m.kvz = 0, 0, 0, 0
	}
}
