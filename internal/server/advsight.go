package server

import "math"

// mobInSightName returns the species name of the first mob a player's gaze
// crosses within reach, or "" — the spyglass advancements' looking_at
// predicate. A slab test against each mob's vanilla hitbox along the view ray
// from the eyes (yaw/pitch in vanilla's convention: yaw 0 = south, pitch
// positive = down).
func (h *hub) mobInSightName(t *tracked, reach float64) string {
	yaw := float64(t.yaw) * math.Pi / 180
	pitch := float64(t.pitch) * math.Pi / 180
	dx := -math.Sin(yaw) * math.Cos(pitch)
	dy := -math.Sin(pitch)
	dz := math.Cos(yaw) * math.Cos(pitch)
	ex, ey, ez := t.x, t.y+1.62, t.z
	best, bestName := reach, ""
	for _, m := range h.mobs {
		if m.dim != t.dim || m.dying > 0 {
			continue
		}
		b := m.box()
		half := b.w / 2
		if d, ok := rayBox(ex, ey, ez, dx, dy, dz, m.x-half, m.y, m.z-half, m.x+half, m.y+b.h, m.z+half); ok && d < best {
			best, bestName = d, advEntityName[m.etype]
		}
	}
	return bestName
}

// rayBox is the slab method: the distance along (dx,dy,dz) from the origin at
// which the ray enters the box, or false if it misses.
func rayBox(ox, oy, oz, dx, dy, dz, x0, y0, z0, x1, y1, z1 float64) (float64, bool) {
	tmin, tmax := 0.0, math.Inf(1)
	for _, ax := range [3][3]float64{{ox, dx, 0}, {oy, dy, 1}, {oz, dz, 2}} {
		o, d := ax[0], ax[1]
		var lo, hi float64
		switch int(ax[2]) {
		case 0:
			lo, hi = x0, x1
		case 1:
			lo, hi = y0, y1
		default:
			lo, hi = z0, z1
		}
		if math.Abs(d) < 1e-9 {
			if o < lo || o > hi {
				return 0, false
			}
			continue
		}
		t0, t1 := (lo-o)/d, (hi-o)/d
		if t0 > t1 {
			t0, t1 = t1, t0
		}
		if t0 > tmin {
			tmin = t0
		}
		if t1 < tmax {
			tmax = t1
		}
		if tmin > tmax {
			return 0, false
		}
	}
	return tmin, true
}
