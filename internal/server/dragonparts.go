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
// drags the tail through the flight HISTORY (DragonFlightHistory): each tail
// segment takes the yaw and height the dragon had 12, 14 and 16 ticks ago,
// against where it was 5 ticks ago, so the tail curls behind a turning
// dragon. The engine flies the dragon once per survival step, so the
// history is kept per step and read between samples.
//
// A client names the part it hits: EnderDragon.recreateFromPacket gives the
// eight parts the ids after the dragon's own, in the order head, neck, body,
// three tail segments, two wings — and the dragon itself is not pickable.
// So the dragon reserves those ids, and a blow on one lands on that part.

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
	tail := func(i int) dragonPart {
		// The segment hangs 1.5 behind on the current yaw, then its own
		// (i+1)×2 along the yaw the dragon had 12+2i ticks ago.
		y0, yaw0 := m.dragonLatency(12 + 2*i)
		y1, yaw1 := m.dragonLatency(5)
		rot := yaw + wrapDegrees(float64(yaw0-yaw1))*math.Pi/180
		tfx, tfz := -math.Sin(rot), math.Cos(rot)
		dd := float64(i+1) * 2
		return dragonPart{name: "tail",
			x: m.x - fx*1.5 - tfx*dd, y: m.y + (y0 - y1), z: m.z - fz*1.5 - tfz*dd, w: 2, h: 2}
	}
	return []dragonPart{
		at("head", 6.5, 0, 0, 1, 1),
		at("neck", 5.5, 0, 0, 3, 3),
		at("body", 0.5, 0, 0, 5, 3),
		at("wing", 0, 4.5, 2, 4, 2),
		at("wing", 0, -4.5, 2, 4, 2),
		tail(0),
		tail(1),
		tail(2),
	}
}

// dragonPartNames is the client's part order (EnderDragon.subEntities): the
// part with id dragon+1+i is dragonPartNames[i].
var dragonPartNames = [8]string{"head", "neck", "body", "tail", "tail", "tail", "wing", "wing"}

// dragonPartOf is the part with client index i (dragonPartNames order) in
// the layout dragonPartsOf returns.
func dragonPartOf(m *mob, i int) dragonPart {
	parts := dragonPartsOf(m)
	switch i {
	case 0, 1, 2:
		return parts[i]
	case 3, 4, 5:
		return parts[5+i-3]
	default:
		return parts[3+i-6]
	}
}

// dragonHistLen is how many flight samples the dragon keeps (one per
// survival step; the longest latency read is 17 ticks).
const dragonHistLen = 4

// recordDragonFlight is DragonFlightHistory.record, once per flight step.
func (m *mob) recordDragonFlight() {
	if m.dragonHistN == 0 {
		for i := range m.dragonHistY {
			m.dragonHistY[i], m.dragonHistYaw[i] = m.y, m.yaw
		}
	}
	copy(m.dragonHistY[1:], m.dragonHistY[:dragonHistLen-1])
	copy(m.dragonHistYaw[1:], m.dragonHistYaw[:dragonHistLen-1])
	m.dragonHistY[0], m.dragonHistYaw[0] = m.y, m.yaw
	m.dragonHistN++
}

// dragonLatency is DragonFlightHistory.get(delay) for a delay in ticks,
// read between the per-step samples; with no history it is the present.
func (m *mob) dragonLatency(ticks int) (float64, float32) {
	if m.dragonHistN == 0 {
		return m.y, m.yaw
	}
	f := float64(ticks) / survivalTickN
	i := int(f)
	if i >= dragonHistLen-1 {
		return m.dragonHistY[dragonHistLen-1], m.dragonHistYaw[dragonHistLen-1]
	}
	frac := f - float64(i)
	y := m.dragonHistY[i] + (m.dragonHistY[i+1]-m.dragonHistY[i])*frac
	yaw := m.dragonHistYaw[i] + float32(frac*wrapDegrees(float64(m.dragonHistYaw[i+1]-m.dragonHistYaw[i])))
	return y, yaw
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

// reserveDragonPartEIDs keeps the eight ids after the dragon's for its parts
// (the client gives them to the parts itself), so no other entity is ever
// minted onto them. Sharded pods mint in lanes, where the next ids belong
// to other pods' lanes already.
func (h *hub) reserveDragonPartEIDs() {
	if h.shardOf != nil {
		return
	}
	for i := 0; i < len(dragonPartNames); i++ {
		h.allocEID()
	}
}

// dragonPartTarget resolves an attack aimed at one of the dragon's part
// ids: the dragon, and which part.
func (h *hub) dragonPartTarget(target int32) (*mob, int, bool) {
	d := h.dragon
	if d == nil || target <= d.eid || target > d.eid+int32(len(dragonPartNames)) {
		return nil, 0, false
	}
	return d, int(target - d.eid - 1), true
}
