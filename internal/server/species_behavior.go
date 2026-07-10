package server

import (
	"math"

	"tachyne/internal/worldgen"
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
	if worldgen.IsWater(w.At(fnx, int(math.Floor(ny)), fnz)) {
		m.x, m.y, m.z = nx, ny, nz
	} else {
		m.vx, m.vy, m.vz = -m.vx*0.5, -m.vy, -m.vz*0.5
	}
	// Small vertical wander so schools don't sit on one plane.
	if h.rng.Intn(20) == 0 {
		m.vy = (h.rng.Float64() - 0.5) * m.speed
	}
	m.vy *= 0.8
}

// flyMove floats a flying mob toward its hover altitude above the terrain,
// with free horizontal movement (no step collision) and a gentle vertical
// spring so it neither sinks into the ground nor drifts to the sky.
func (h *hub) flyMove(m *mob, nx, nz float64, fnx, fnz int) {
	w := h.worldFor(m.dim)
	// Don't fly through solid walls: only take the horizontal step if the
	// destination column's air is clear at flight height.
	ground := float64(w.SurfaceFeet(fnx, fnz))
	want := ground + m.hover
	if m.hasTarget && m.ty != 0 { // diving on prey: aim at the target's level
		want = m.ty + m.hover*0.3
	}
	if !worldgen.Collides(w.At(fnx, int(math.Floor(m.y)), fnz)) {
		m.x, m.z = nx, nz
	} else {
		ang := h.rng.Float64() * 2 * math.Pi
		m.vx, m.vz = math.Cos(ang)*m.speed, math.Sin(ang)*m.speed
	}
	// Vertical spring toward the desired altitude.
	m.y += math.Max(-m.speed, math.Min(m.speed, (want-m.y)*0.1))
}

// mobRanged is the shared ranged-attack gate: cools down, finds a huntable
// player in range, faces it, and returns it (nil = hold fire). period is in
// mob-updates (2 ticks each).
func (h *hub) mobRanged(players map[int32]*tracked, m *mob, rng, period int) *tracked {
	if m.attackCD > 0 {
		m.attackCD--
		return nil
	}
	t := h.nearestHuntable(players, m.dim, m.x, m.z, float64(rng))
	if t == nil {
		return nil
	}
	m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi)
	m.attackCD = period
	return t
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

// ghastShoot lobs an explosive fireball (vanilla Ghast: explosionPower 1).
func (h *hub) ghastShoot(players map[int32]*tracked, m *mob) {
	t := h.mobRanged(players, m, 64, 30) // fires every ~3 s from far off
	if t == nil {
		return
	}
	ux, uy, uz := aimAt(m.x, m.y+2, m.z, t.x, t.y+0.5, t.z)
	a := h.launchProjectileIn(players, entityLargeFireball, m.dim, m.x, m.y+2, m.z, ux, uy, uz)
	a.shooter, a.dmg, a.explode, a.fire = m.eid, 6, 1, true
	h.playSoundDim(players, m.dim, "minecraft:entity.ghast.shoot", sndHostile, m.x, m.y, m.z, 3, 1)
}

// breezeShoot fires a wind charge — knockback, no damage (vanilla Breeze).
func (h *hub) breezeShoot(players map[int32]*tracked, m *mob) {
	t := h.mobRanged(players, m, 24, 15)
	if t == nil {
		return
	}
	ux, uy, uz := aimAt(m.x, m.y+1, m.z, t.x, t.y+0.5, t.z)
	v := 1.4
	a := h.launchProjectileIn(players, entityWindCharge, m.dim, m.x, m.y+1, m.z, ux*v, uy*v, uz*v)
	a.shooter, a.knock, a.breaks = m.eid, 1.5, true
	h.playSound(players, "minecraft:entity.breeze.shoot", sndHostile, m.x, m.y, m.z, 1, 1)
}

// witherShoot fires a wither skull (dark damage + the wither effect).
func (h *hub) witherShoot(players map[int32]*tracked, m *mob) {
	t := h.mobRanged(players, m, 40, 8)
	if t == nil {
		return
	}
	ux, uy, uz := aimAt(m.x, m.y+2, m.z, t.x, t.y+0.5, t.z)
	v := 1.2
	a := h.launchProjectileIn(players, entityWitherSkull, m.dim, m.x, m.y+2, m.z, ux*v, uy*v, uz*v)
	a.shooter, a.dmg, a.wither, a.breaks = m.eid, 8, 10, true
	h.playSoundDim(players, m.dim, "minecraft:entity.wither.shoot", sndHostile, m.x, m.y, m.z, 2, 1)
}

// shulkerShoot fires a bullet (vanilla homes; ours flies straight and gives
// its target levitation on a hit isn't modeled — it just stings).
func (h *hub) shulkerShoot(players map[int32]*tracked, m *mob) {
	t := h.mobRanged(players, m, 16, 10)
	if t == nil {
		return
	}
	ux, uy, uz := aimAt(m.x, m.y+0.5, m.z, t.x, t.y+1, t.z)
	v := 0.8
	a := h.launchProjectileIn(players, entityShulkerBullet, m.dim, m.x, m.y+0.5, m.z, ux*v, uy*v, uz*v)
	a.shooter, a.dmg, a.breaks = m.eid, 4, true
	h.playSound(players, "minecraft:entity.shulker.shoot", sndHostile, m.x, m.y, m.z, 1, 1)
}

// guardianBeam is the guardian's charge-up laser: it locks on, and after a
// short wind-up deals the hit directly (vanilla's beam has no travelling
// projectile — the damage is applied when the beam completes).
func (h *hub) guardianBeam(players map[int32]*tracked, m *mob) {
	if m.attackCD > 0 {
		m.attackCD--
		if m.attackCD == 0 && m.hasTarget { // beam completes — apply the hit
			if t := h.nearestHuntable(players, m.dim, m.x, m.z, 16); t != nil {
				dmg := hostileMelee(m) * h.diffMult()
				h.damage(players, t, t.armorReduce(dmg))
				h.wearArmor(players, t, dmg)
			}
		}
		return
	}
	t := h.nearestHuntable(players, m.dim, m.x, m.z, 16)
	if t == nil {
		return
	}
	m.yaw = float32(math.Atan2(-(t.x-m.x), t.z-m.z) * 180 / math.Pi)
	m.attackCD = 40 // ~4 s charge (vanilla ATTACK_TIME 80 ticks; ours in updates)
	h.playSound(players, "minecraft:entity.guardian.attack", sndHostile, m.x, m.y, m.z, 1, 1)
}
