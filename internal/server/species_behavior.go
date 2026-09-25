package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Locomotion modes + signature ranged attacks for the roster species. Walkers
// use updateMobs' default terrain collision; the modes here handle the mobs
// that don't walk — swimmers stay in their water column, fliers float free,
// and the various shooters lob their species' projectile.

// swimMove keeps a water mob inside water: it moves freely in 3D but is pulled
// back toward the water body if it would leave it. Vanilla fish/squid drift and
// dart; ours wander within the sea and bob to stay submerged.
func (h *hub) swimMove(m *mob, nx, nz float64, fnx, fnz int) {
	w := h.worldFor(m.dim)
	ny := m.y + m.vy
	// Only advance into cells that are still water — otherwise bounce off the
	// bank/surface and pick a new heading, so the fish never beaches itself.
	if worldgen.HoldsWater(w.At(fnx, int(math.Floor(ny)), fnz)) {
		m.x, m.y, m.z = nx, ny, nz
	} else {
		m.vx, m.vy, m.vz = -m.vx*0.5, -m.vy, -m.vz*0.5
	}
	// Small vertical wander so schools don't sit on one plane.
	if h.rng.Intn(20) == 0 {
		m.vy = (h.rng.Float64() - 0.5) * m.moveSpeed()
	}
	m.vy *= 0.8
	// Entity.onInsideBubbleColumn: a whirlpool pulls a swimmer down, an
	// updraft carries it up — the clamps are vanilla's per-tick figures.
	switch w.At(int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))) {
	case worldgen.BubbleColumnDrag:
		m.vy = math.Max(-0.3, m.vy-0.03)
	case worldgen.BubbleColumnUp:
		m.vy = math.Min(0.7, m.vy+0.06)
	}
}

// flyMove floats a flying mob toward its hover altitude above the terrain,
// with free horizontal movement (no step collision) and a gentle vertical
// spring so it neither sinks into the ground nor drifts to the sky.
func (h *hub) flyMove(m *mob, nx, nz float64, fnx, fnz int) {
	w := h.worldFor(m.dim)
	// Hover relative to the floor at the mob's OWN level (a cave bat hovers
	// over the cave floor, not the mountain top far above it).
	ground := float64(w.MobFeetFrom(fnx, fnz, int(math.Floor(m.y))))
	want := ground + m.hover
	if y, ok := m.phantomAltitude(); ok { // circling high, or diving at the target
		want = y
	} else if y, ok := m.flyAimed(h.tick.Load()); ok { // led somewhere: a tempting player, a ghastling's player
		want = y
	} else if _, floats := m.behavior.(floatAroundBehavior); floats && m.floatSet {
		// RandomFloatAroundGoal wants a point in three dimensions: the ghast
		// rises and sinks to it rather than holding one height (and never
		// dives at what it shoots).
		want = math.Max(m.floatY, ground+1)
	} else if m.hasTarget && m.ty != 0 { // diving on prey: aim at the target's level
		want = m.ty + m.hover*0.3
	}
	// On a route, the current waypoint's own height IS the altitude wanted.
	// Without this the hover spring pulls the mob back to its cruising height
	// every tick while the errand pushes it up, and the two cancel: the mob
	// bobs in place and never climbs to the hive in the tree.
	if node, ok := m.flyWaypoint(); ok {
		want = float64(node.y) + 0.5
	}
	if !worldgen.Collides(w.At(fnx, int(math.Floor(m.y)), fnz)) {
		m.x, m.z = nx, nz
	} else {
		ang := h.rng.Float64() * 2 * math.Pi
		m.vx, m.vz = math.Cos(ang)*m.moveSpeed(), math.Sin(ang)*m.moveSpeed()
	}
	// Vertical spring toward the desired altitude — but never into a ceiling
	// (the unchecked spring carried cave bats up through solid rock).
	climb := m.moveSpeed() * m.flyingFactor()
	ny := m.y + math.Max(-climb, math.Min(climb, (want-m.y)*0.1))
	if !worldgen.Collides(w.At(int(math.Floor(m.x)), int(math.Floor(ny)), int(math.Floor(m.z)))) {
		m.y = ny
	}
}

// mobRanged is the shared ranged-attack gate: cools down, finds its target
// in range (a player, or the prey its target goal picked), faces it, and
// returns it (false = hold fire). period is in mob-updates (2 ticks each).
func (h *hub) mobRanged(players map[int32]*tracked, m *mob, rng, period int) (quarry, bool) {
	if m.attackCD > 0 {
		m.attackCD--
		return quarry{}, false
	}
	t, ok := h.rangedQuarry(players, m, float64(rng))
	if !ok {
		return quarry{}, false
	}
	if !h.seesQuarry(m, t, false) {
		return quarry{}, false // RangedAttackGoal: no shot without line of sight
	}
	m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi)
	m.attackCD = period
	return t, true
}

// aimAt returns a unit vector from (ox,oy,oz) to a target point (0,0,0 on a
// degenerate aim).
func aimAt(ox, oy, oz, tx, ty, tz float64) (float64, float64, float64) {
	dx, dy, dz := tx-ox, ty-oy, tz-oz
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	if d < 1e-6 {
		return 0, 0, 0
	}
	return dx / d, dy / d, dz / d
}

// witherShoot fires a wither skull (dark damage + the wither effect). The
// centre head's goal is RangedAttackGoal(this, 1.0, 40, 20): a skull every 40
// ticks at a target within 20 blocks (19 updates of cooldown plus the
// shooting one).
func (h *hub) witherShoot(players map[int32]*tracked, m *mob) {
	t, ok := h.mobRanged(players, m, 20, 19)
	if !ok {
		return
	}
	// performRangedAttack(0, target): the centre head's aimed shot is blue
	// once in a thousand, which is the rare nasty surprise in the fight.
	h.witherSkullAt(players, m, t.x, t.y+0.5, t.z, h.rng.Float64() < witherBlueOdds)
}
