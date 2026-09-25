package server

import "math"

// The dragon's hitboxes. Vanilla's EnderDragon is not one box: it carries
// eight parts — head, neck, body, two wings and three tail segments — each
// with its own box, and EnderDragon.hurt quarters anything that lands
// anywhere but the head and neck:
//
//	if (part != this.head && part != this.neck) f = f / 4.0f + Math.min(f, 1.0f);
//
// That one line is the shape of the whole fight. An arrow into the head is
// worth four into the tail, which is why the dragon is fought from the middle
// of the island looking up rather than by peppering whatever drifts past.
//
// Vanilla places the head and neck along the flight path's look vector and
// drags the tail through the flight HISTORY (DragonFlightHistory): each tail
// segment takes the yaw and height the dragon had 12, 14 and 16 ticks ago,
// against where it was 5 ticks ago, so the tail curls behind a turning
// dragon. The dragon flies every tick, and the history is vanilla's
// sixty-four samples, one a tick.
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

// dragonPartsOf lays the eight boxes out around a dragon exactly as aiStep's
// tickPart calls do, in the client's order (EnderDragon.subEntities): head,
// neck, body, three tail segments, two wings. The dragon's yaw is vanilla's
// yRot: it flies along (sin, −cos), the reverse of other mobs' facing.
func dragonPartsOf(m *mob) []dragonPart {
	d := m.dragon()
	s5, s10 := m.dragonLatency(5), m.dragonLatency(10)
	tilt := (s5.y - s10.y) * 10 * math.Pi / 180
	ccT, ssT := math.Cos(tilt), math.Sin(tilt)
	rot1 := float64(m.yaw) * math.Pi / 180
	ss1, cc1 := math.Sin(rot1), math.Cos(rot1)
	rot2 := rot1 - d.yRotA*0.01
	ss2, cc2 := math.Sin(rot2), math.Cos(rot2)
	yOff := s5.y - m.dragonLatency(0).y // getHeadYOffset
	if dragonSitting(d.phase) {
		yOff = -1
	}
	at := func(name string, dx, dy, dz, w, h float64) dragonPart {
		return dragonPart{name: name, x: m.x + dx, y: m.y + dy, z: m.z + dz, w: w, h: h}
	}
	parts := []dragonPart{
		at("head", ss2*6.5*ccT, yOff+ssT*6.5, -cc2*6.5*ccT, 1, 1),
		at("neck", ss2*5.5*ccT, yOff+ssT*5.5, -cc2*5.5*ccT, 3, 3),
		at("body", ss1*0.5, 0, -cc1*0.5, 5, 3),
	}
	for i := 0; i < 3; i++ {
		p0 := m.dragonLatency(12 + i*2)
		rot := rot1 + wrapDegrees(float64(p0.yaw-s5.yaw))*math.Pi/180
		ss, cc := math.Sin(rot), math.Cos(rot)
		dd := float64(i+1) * 2
		parts = append(parts, at("tail", -(ss1*1.5+ss*dd)*ccT, p0.y-s5.y-(dd+1.5)*ssT+1.5, (cc1*1.5+cc*dd)*ccT, 2, 2))
	}
	return append(parts,
		at("wing", cc1*4.5, 2, ss1*4.5, 4, 2),
		at("wing", -cc1*4.5, 2, -ss1*4.5, 4, 2))
}

// dragonPartNames is the client's part order (EnderDragon.subEntities): the
// part with id dragon+1+i is dragonPartNames[i].
var dragonPartNames = [8]string{"head", "neck", "body", "tail", "tail", "tail", "wing", "wing"}

// dragonPartOf is the part with client index i (dragonPartNames order).
func dragonPartOf(m *mob, i int) dragonPart { return dragonPartsOf(m)[i] }

// recordDragonFlight is DragonFlightHistory.record, once a tick. The first
// record fills the history with the present so an early read is level.
func (m *mob) recordDragonFlight() {
	d := m.dragon()
	s := dragonFlightSample{y: m.y, yaw: m.yaw}
	if d.histN == 0 {
		for i := range d.hist {
			d.hist[i] = s
		}
	}
	d.histHead = (d.histHead + 1) & 63
	d.hist[d.histHead] = s
	d.histN++
}

// dragonLatency is DragonFlightHistory.get(delay): where it was delay ticks
// ago; with no history it is the present.
func (m *mob) dragonLatency(delay int) dragonFlightSample {
	d := m.dragon()
	if d.histN == 0 {
		return dragonFlightSample{y: m.y, yaw: m.yaw}
	}
	return d.hist[(d.histHead-delay)&63]
}

// dragonPartAt names the part a point lands in, if any (a part's box is
// its width about it and its height up from it).
func dragonPartAt(m *mob, px, py, pz float64) (string, bool) {
	for _, p := range dragonPartsOf(m) {
		half := p.w / 2
		if math.Abs(px-p.x) <= half && math.Abs(pz-p.z) <= half && py >= p.y && py <= p.y+p.h {
			return p.name, true
		}
	}
	return "", false
}

// dragonPartDamage is EnderDragon.hurt's part rule: the head and neck take
// a blow whole, everything else a quarter of it plus a point.
func dragonPartDamage(part string, dmg float64) float64 {
	if part == "head" || part == "neck" {
		return dmg
	}
	return dmg/4 + math.Min(dmg, 1)
}

// dragonHurtFilter is EnderDragon.hurt ahead of the damage: nothing lands on
// a dying (or dead) dragon; a sitting one turns arrows and wind charges
// away untouched; the part rule; and only a player's blow, or one of
// #always_hurts_ender_dragons (explosions), counts at all.
func (m *mob) dragonHurtFilter(dmg float64, dt dmgType) (float64, bool) {
	d := m.dragon()
	part, byPlayer, arrow := m.dragonMeleePart, m.dragonHitByPlayer, m.dragonHitArrow
	m.dragonMeleePart, m.dragonHitByPlayer, m.dragonHitArrow = "", false, false // one blow's context
	if d.dead || d.phase == phaseDying || m.health <= 0 {
		return 0, false
	}
	if dragonSitting(d.phase) && arrow {
		return 0, false
	}
	if part == "" {
		part = "body" // hurtServer routes to the body
	}
	dmg = dragonPartDamage(part, dmg)
	if dmg < 0.01 || !(byPlayer || dt.has(tagAlwaysHurtsEnderDragons)) {
		return 0, false
	}
	return dmg, true
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
