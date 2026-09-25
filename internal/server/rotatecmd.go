package server

import "math"

// /rotate (RotateCommand): turn one entity to a rotation — plain degrees or
// `~` relative to whoever runs it — or to face a position or another entity
// (its feet, or its eyes).

type evRotate struct {
	eid  int32
	args []string
}

func (evRotate) isHubEvent() {}

const rotateUsage = "Usage: /rotate <target> <yaw> <pitch> | /rotate <target> facing <x> <y> <z> | /rotate <target> facing entity <entity> [eyes|feet]"

func (h *hub) cmdRotate(players map[int32]*tracked, e evRotate) {
	me := players[e.eid]
	if me == nil {
		return
	}
	a := e.args
	if len(a) < 3 {
		me.p.tell(rotateUsage)
		return
	}
	// The one entity to turn: a player first, then a mob.
	var (
		tp   *tracked
		m    *mob
		x, y float64
		z    float64
		dim  int
		name string
	)
	if ts := h.commandTargets(players, e.eid, a[0]); len(ts) > 0 {
		tp, x, y, z, dim, name = ts[0], ts[0].x, ts[0].y, ts[0].z, ts[0].dim, ts[0].p.name
	} else if ms := h.commandMobs(players, e.eid, a[0]); len(ms) > 0 {
		m, x, y, z, dim, name = ms[0], ms[0].x, ms[0].y, ms[0].z, ms[0].dim, mobDisplayName(ms[0].etype)
	} else {
		me.p.tell("No entity was found")
		return
	}

	var yaw, pitch float64
	switch {
	case a[1] != "facing" && len(a) == 3:
		var ok1, ok2 bool
		yaw, ok1 = parseCoord(a[1], float64(me.yaw))
		pitch, ok2 = parseCoord(a[2], float64(me.pitch))
		if !ok1 || !ok2 {
			me.p.tell(rotateUsage)
			return
		}
	case a[1] == "facing" && a[2] == "entity" && (len(a) == 4 || len(a) == 5):
		var tx, ty, tz float64
		if ts := h.commandTargets(players, e.eid, a[3]); len(ts) > 0 {
			tx, ty, tz = ts[0].x, ts[0].y, ts[0].z
			if len(a) == 5 && a[4] == "eyes" {
				ty += playerEyeStand
			}
		} else if ms := h.commandMobs(players, e.eid, a[3]); len(ms) > 0 {
			tx, ty, tz = ms[0].x, ms[0].y, ms[0].z
			if len(a) == 5 && a[4] == "eyes" {
				ty += mobEyeHeight(ms[0])
			}
		} else {
			me.p.tell("No entity was found")
			return
		}
		yaw, pitch = lookAngles(x, y, z, tx, ty, tz)
	case a[1] == "facing" && len(a) == 5:
		tx, ty, tz, ok := parsePosition(a[2:5], me.x, me.y, me.z, me.yaw, me.pitch)
		if !ok {
			me.p.tell(rotateUsage)
			return
		}
		yaw, pitch = lookAngles(x, y, z, tx, ty, tz)
	default:
		me.p.tell(rotateUsage)
		return
	}
	pitch = math.Max(-90, math.Min(90, pitch))
	yaw = math.Remainder(yaw, 360)

	if tp != nil {
		tp.yaw, tp.pitch = float32(yaw), float32(pitch)
		tp.p.sendEv(teleportEv(tp.x, tp.y, tp.z, tp.yaw, tp.pitch))
	} else {
		m.yaw = float32(yaw)
		h.toTracking(players, m.eid, dim, m.x, m.z, entMove(m.eid, m.x, m.y, m.z, m.yaw, float32(pitch), false))
		h.toTracking(players, m.eid, dim, m.x, m.z, entHead(m.eid, m.yaw))
	}
	h.cmdSuccess(players, me.p, "Rotated "+name, true)
}

// lookAngles is Entity.lookAt: the yaw and pitch from one point toward
// another, in Minecraft's degrees (yaw 0 = +z, pitch up negative).
func lookAngles(fx, fy, fz, tx, ty, tz float64) (yaw, pitch float64) {
	dx, dy, dz := tx-fx, ty-fy, tz-fz
	yaw = math.Atan2(dz, dx)*180/math.Pi - 90
	pitch = -math.Atan2(dy, math.Hypot(dx, dz)) * 180 / math.Pi
	return yaw, pitch
}
