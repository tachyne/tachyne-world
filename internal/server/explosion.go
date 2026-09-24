package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// What a blast does to the things standing in it — ServerExplosion.
// hurtEntities. Everything within twice the power is a candidate; how much
// of it the blast can actually see (getSeenPercent: rays from a grid over
// its bounding box to the centre, through blocks) scales both the damage
// and the shove, so a wall shields and a corner half-shields. The damage
// is (impact² + impact)/2 × 7 × 2r + 1 for impact = (1 − d/2r) × exposure
// — a TNT blast at your feet is 57, at the edge 1 — and the shove is
// impact × (1 − explosion knockback resistance), from the eyes, in three
// dimensions. Before, damage fell off linearly to a per-cause cap and
// nothing shielded.

const explosionDamageScale = 7.0 // ExplosionDamageCalculator.getEntityDamageAmount

// explosionImpact is the (1 − dist) × exposure term for a thing at
// (x,y,z) inside a blast of the given power; 0 when out of reach.
func explosionImpact(power, cx, cy, cz, x, y, z, exposure float64) float64 {
	dr := power * 2
	if dr < 1e-5 {
		return 0
	}
	dist := dist3(x, y, z, cx, cy, cz) / dr
	if dist > 1 {
		return 0
	}
	return (1 - dist) * exposure
}

// explosionDamage is the damage for an impact inside a blast.
func explosionDamage(power, impact float64) float64 {
	return (impact*impact+impact)/2*explosionDamageScale*power*2 + 1
}

// seenPercent is ServerExplosion.getSeenPercent: the fraction of a grid of
// points over the box (a point every half block, centred) that has a
// clear line to the blast's centre.
func (h *hub) seenPercent(dim int, cx, cy, cz, minX, minY, minZ, maxX, maxY, maxZ float64) float64 {
	xs := 1 / ((maxX-minX)*2 + 1)
	ys := 1 / ((maxY-minY)*2 + 1)
	zs := 1 / ((maxZ-minZ)*2 + 1)
	xOff := (1 - math.Floor(1/xs)*xs) / 2
	zOff := (1 - math.Floor(1/zs)*zs) / 2
	hits, count := 0, 0
	for xx := 0.0; xx <= 1; xx += xs {
		for yy := 0.0; yy <= 1; yy += ys {
			for zz := 0.0; zz <= 1; zz += zs {
				x := minX + xx*(maxX-minX)
				y := minY + yy*(maxY-minY)
				z := minZ + zz*(maxZ-minZ)
				if h.sightClear(dim, x+xOff, y, z+zOff, cx, cy, cz) {
					hits++
				}
				count++
			}
		}
	}
	if count == 0 {
		return 0
	}
	return float64(hits) / float64(count)
}

// explodeHurt is the blast's second half: the TNT carts it lights and the
// damage and shove on everything within reach.
func (h *hub) explodeHurt(players map[int32]*tracked, dim int, cx, cy, cz, power float64, dt dmgType, cause deathCause) {
	dr := power * 2
	for _, v := range h.vehicles { // a blast lights the TNT carts it reaches
		if v.dim == dim && v.etype == entityTntMinecart && v.fuse < 0 && dist3(v.x, v.y, v.z, cx, cy, cz) < dr+1 {
			h.primeCart(players, v, h.rng.Intn(20)+h.rng.Intn(20))
		}
	}
	if power < 1e-5 {
		return
	}
	for _, t := range players {
		if t.dim != dim || t.dead {
			continue
		}
		if dist3(t.x, t.y, t.z, cx, cy, cz) > dr {
			continue
		}
		exposure := h.seenPercent(dim, cx, cy, cz, t.x-0.3, t.y, t.z-0.3, t.x+0.3, t.y+1.8, t.z+0.3)
		impact := explosionImpact(power, cx, cy, cz, t.x, t.y, t.z, exposure)
		h.hurtFrom(players, t, float32(explosionDamage(power, impact)), dt, cause, from(cx, cz))
		// The blast's own shove (explosion is tagged no_knockback so the hit
		// does not add the ordinary one): from the eyes, scaled by what
		// Blast Protection's resistance buys.
		kb := impact * t.explosionKnockScale()
		ex, ey, ez := t.x-cx, t.y+playerEyeHeightStand-cy, t.z-cz
		n := math.Sqrt(ex*ex + ey*ey + ez*ez)
		if kb <= 0 || n < 1e-9 || t.gamemode == gmSpectator {
			continue
		}
		t.p.trySendEv(attachproto.Velocity{EID: t.p.eid, VX: ex / n * kb, VY: ey / n * kb, VZ: ez / n * kb})
		t.spinUntil = h.tick.Load() + windBurstGrace // let the launch through the speed check
	}
	for _, om := range h.mobs {
		if om.dim != dim || om.dying > 0 {
			continue
		}
		if dist3(om.x, om.y, om.z, cx, cy, cz) > dr {
			continue
		}
		b := om.box()
		exposure := h.seenPercent(dim, cx, cy, cz, om.x-b.w/2, om.y, om.z-b.w/2, om.x+b.w/2, om.y+b.h, om.z+b.w/2)
		impact := explosionImpact(power, cx, cy, cz, om.x, om.y, om.z, exposure)
		if om.hasBody() {
			// A sulfur cube's block: the blast lights TNT short and does no
			// damage, and the shove is three-dimensional, as vanilla's is.
			om.hurtKind(explosionDamage(power, impact), dt)
			h.cubeBlastPush(om, cx, cy, cz, impact)
			continue
		}
		om.hurtKind(explosionDamage(power, impact), dt)
		if om.health <= 0 {
			h.killMob(players, om)
			if h.blastChargedCreeper {
				h.chargedHeadDrop(players, om) // charged_creeper/<victim>: its head
			}
			continue
		}
		if kb := impact * om.kbScale(); kb > 0 {
			ex, ez := om.x-cx, om.z-cz
			if n := math.Hypot(ex, ez); n > 1e-9 {
				om.vx, om.vz, om.kb, om.reroute = om.vx+ex/n*kb, om.vz+ez/n*kb, 3, 0
				h.mobKnockVelocity(players, om)
			}
		}
	}
	for _, pt := range h.tnt { // PrimedTnt is pushed from its position, not its eyes
		if pt.dim != dim || dist3(pt.x, pt.y, pt.z, cx, cy, cz) > dr {
			continue
		}
		exposure := h.seenPercent(dim, cx, cy, cz, pt.x-tntHalfWidth, pt.y, pt.z-tntHalfWidth, pt.x+tntHalfWidth, pt.y+tntHeight, pt.z+tntHalfWidth)
		impact := explosionImpact(power, cx, cy, cz, pt.x, pt.y, pt.z, exposure)
		ex, ey, ez := pt.x-cx, pt.y-cy, pt.z-cz
		if n := math.Sqrt(ex*ex + ey*ey + ez*ez); impact > 0 && n > 1e-9 {
			pt.vx, pt.vy, pt.vz = pt.vx+ex/n*impact, pt.vy+ey/n*impact, pt.vz+ez/n*impact
		}
	}
	h.bus.publish("explosion", map[string]any{"x": cx, "y": cy, "z": cz})
}
