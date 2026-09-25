package server

import "math"

// Home restrictions for the tamed nautilus and the happy ghast
// (AbstractNautilus.checkRestriction, HappyGhast.checkRestriction): each
// update, one that is neither leashed nor carrying anyone re-anchors its
// home where it is whenever it has none, has drifted beyond the radius
// plus a margin (eight for a nautilus, sixteen for a ghast) — having been
// ridden or led away — or its radius changed. Their random wandering then
// stays inside it (GoalUtils.mobRestricted rejects spots outside); here a
// mob outside its home steers back in. The radius is the adult bare one's
// 32 (nautilus) or 64 (ghast), halved for a baby or once saddled or
// harnessed.

// restrictionRadius is the species' home radius and re-anchor margin;
// ok=false when the species (or this one, untamed) keeps no home.
func restrictionRadius(m *mob) (r, margin int, ok bool) {
	switch {
	case nautilusKind(m.etype):
		if !m.tamed {
			return 0, 0, false
		}
		if m.baby || m.saddled {
			return 16, 8, true
		}
		return 32, 8, true
	case m.etype == entityHappyGhast:
		if m.baby || m.harness != 0 {
			return 32, 16, true
		}
		return 64, 16, true
	}
	return 0, 0, false
}

// restrictionHomeStep runs checkRestriction and walks the mob back inside
// its home. Reports whether it took the movement.
func (h *hub) restrictionHomeStep(m *mob) bool {
	r, margin, ok := restrictionRadius(m)
	if !ok || m.leash != 0 || m.rider != 0 || m.mobRider != 0 || len(m.riders) > 0 {
		return false
	}
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	d2 := func() float64 {
		dx, dy, dz := float64(bx-m.homePos.x), float64(by-m.homePos.y), float64(bz-m.homePos.z)
		return dx*dx + dy*dy + dz*dz
	}
	if far := float64(r + margin); m.homeR == 0 || d2() >= far*far || m.homeR != r {
		m.homePos, m.homeR = blockPos{bx, by, bz}, r // setHomeTo(blockPosition, radius)
	}
	if d2() < float64(r*r) { // isWithinHome
		return false
	}
	hx, hy, hz := float64(m.homePos.x)+0.5, float64(m.homePos.y), float64(m.homePos.z)+0.5
	m.vx, m.vz = straightSteer(m, hx, hz, 0.5)
	m.vy = math.Max(-m.moveSpeed(), math.Min(m.moveSpeed(), (hy-m.y)*0.1))
	m.rest = 0
	return true
}
