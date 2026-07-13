package server

import attachproto "github.com/tachyne/tachyne-common/attach"

// Locator bar: players broadcast their block position to every other player
// in the same dimension so the 26.2 HUD shows a direction marker per player.
// The engine emits domain waypoint frames unconditionally (gated only by the
// locator_bar gamerule); the gateways drop them for pre-26.2 clients, which
// lack the feature. Repeated TRACK is idempotent on the client (the waypoint
// map is keyed by the transmitter UUID), so a plain re-track on block change
// is all the update path needs.

const (
	waypointTrack   = 0
	waypointUntrack = 1
)

// waypointFor builds a track/untrack frame for a transmitter.
func waypointFor(t *tracked, op int8) attachproto.Waypoint {
	return attachproto.Waypoint{Op: op, UUID: t.p.uuid,
		X: int32(t.x), Y: int32(t.y), Z: int32(t.z)}
}

// waypointOnJoin cross-registers the joiner with everyone already present.
func (h *hub) waypointOnJoin(players map[int32]*tracked, nt *tracked) {
	if !h.rules.LocatorBar {
		return
	}
	mine := waypointFor(nt, waypointTrack)
	for _, o := range players {
		if o.p.eid == nt.p.eid || o.dim != nt.dim {
			continue
		}
		o.p.trySendEv(mine)
		nt.p.trySendEv(waypointFor(o, waypointTrack))
	}
}

// waypointOnMove re-tracks a mover whose block position changed.
func (h *hub) waypointOnMove(players map[int32]*tracked, t *tracked, moved bool) {
	if !h.rules.LocatorBar || !moved {
		return
	}
	f := waypointFor(t, waypointTrack)
	for _, o := range players {
		if o.p.eid == t.p.eid || o.dim != t.dim {
			continue
		}
		o.p.trySendEv(f)
	}
}

// waypointOnLeave untracks a departing player for everyone.
func (h *hub) waypointOnLeave(players map[int32]*tracked, p *player) {
	f := attachproto.Waypoint{Op: waypointUntrack, UUID: p.uuid}
	for _, o := range players {
		o.p.trySendEv(f)
	}
}
