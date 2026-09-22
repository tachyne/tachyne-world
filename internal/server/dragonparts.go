package server

import "math"

// The dragon's hitboxes. Vanilla's EnderDragon is not one box: it carries
// eight parts — head, neck, body, two wings and three tail segments — each
// with its own box, and EnderDragon.hurt quarters anything that lands
// anywhere but the head:
//
//	if (part != this.head) f = f / 4.0f + Math.min(f, 1.0f);
//
// That one line is the shape of the whole fight. An arrow into the head is
// worth four into the tail, which is why the dragon is fought from the middle
// of the island looking up rather than by peppering whatever drifts past.
//
// Vanilla places the head and neck along the flight path's look vector and
// drags the tail through the flight HISTORY, so the tail curls behind a
// turning dragon. These parts sit on the dragon's current yaw instead: the
// shape is right, the lag is not.

type dragonPart struct {
	name    string
	x, y, z float64
	w, h    float64 // box width (centred) and height (from y upward)
}

// dragonPartsOf lays the eight boxes out around a dragon.
func dragonPartsOf(m *mob) []dragonPart {
	yaw := float64(m.yaw) * math.Pi / 180
	// The facing the dragon's head points along, and the wing axis across it.
	fx, fz := -math.Sin(yaw), math.Cos(yaw)
	wx, wz := math.Cos(yaw), math.Sin(yaw)
	at := func(name string, ahead, side, up, w, h float64) dragonPart {
		return dragonPart{name: name,
			x: m.x + fx*ahead + wx*side, y: m.y + up, z: m.z + fz*ahead + wz*side, w: w, h: h}
	}
	return []dragonPart{
		at("head", 6.5, 0, 0, 1, 1),
		at("neck", 5.5, 0, 0, 3, 3),
		at("body", 0.5, 0, 0, 5, 3),
		at("wing", 0, 4.5, 2, 4, 2),
		at("wing", 0, -4.5, 2, 4, 2),
		at("tail", -3.5, 0, 0, 2, 2),
		at("tail", -5.5, 0, 0, 2, 2),
		at("tail", -7.5, 0, 0, 2, 2),
	}
}

// dragonPartAt names the part a point lands in, if any.
func dragonPartAt(m *mob, px, py, pz float64) (string, bool) {
	for _, p := range dragonPartsOf(m) {
		half := p.w / 2
		if math.Abs(px-p.x) <= half && math.Abs(pz-p.z) <= half &&
			py >= p.y-p.h/2 && py <= p.y+p.h/2 {
			return p.name, true
		}
	}
	return "", false
}

// dragonPartDamage is EnderDragon.hurt's part rule: the head takes a blow
// whole, everything else takes a quarter of it plus a point.
func dragonPartDamage(part string, dmg float64) float64 {
	if part == "head" {
		return dmg
	}
	return dmg/4 + math.Min(dmg, 1)
}
