package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

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

// waypointHideHeads are the items Waypoint.addHideAttribute marks: worn on
// the head, they take the wearer off everyone's locator bar.
var waypointHideHeads = func() map[int32]bool {
	m := map[int32]bool{}
	for _, n := range []string{"carved_pumpkin", "skeleton_skull", "wither_skeleton_skull", "player_head",
		"zombie_head", "creeper_head", "dragon_head", "piglin_head"} {
		if id, ok := itemByName[n]; ok {
			m[int32(id)] = true
		}
	}
	return m
}()

// waypointTransmits is isTransmittingWaypoint: WAYPOINT_TRANSMIT_RANGE above
// zero. Invisibility, a hiding head and crouching each multiply it by zero,
// and a spectator never transmits (doesSourceIgnoreReceiver).
func waypointTransmits(t *tracked) bool {
	return !t.dead && t.gamemode != gmSpectator && t.hasEffect(effInvisibility) == 0 &&
		!waypointHideHeads[t.armor[0].item] && t.playerAttrs().Value(attr.WaypointTransmitRange) > 0
}

// waypointReaches is doesSourceIgnoreReceiver's range half: a receiver sees
// a transmitter nearer than the lesser of its transmit range and the
// receiver's receive range; a spectator sees every one, and a receiver whose
// receive range is zero is not receiving at all (isReceivingWaypoints).
func waypointReaches(t, o *tracked) bool {
	if o.gamemode == gmSpectator {
		return true
	}
	recv := o.playerAttrs().Value(attr.WaypointReceiveRange)
	if recv <= 0 {
		return false
	}
	r := math.Min(t.playerAttrs().Value(attr.WaypointTransmitRange), recv)
	return dist3(t.x, t.y, t.z, o.x, o.y, o.z) < r
}

// waypointSync brings every receiver's view of transmitter t up to date:
// a track when t shows (always re-sent when it moved), an untrack when it
// stopped showing or left the receiver's dimension.
func (h *hub) waypointSync(players map[int32]*tracked, t *tracked, moved bool) {
	shows := h.rules.LocatorBar && waypointTransmits(t)
	for _, o := range players {
		if o.p.eid == t.p.eid {
			continue
		}
		had := o.wpTracked[t.p.eid]
		switch want := shows && o.dim == t.dim && waypointReaches(t, o); {
		case want && (moved || !had):
			if o.wpTracked == nil {
				o.wpTracked = map[int32]bool{}
			}
			o.wpTracked[t.p.eid] = true
			o.p.trySendEv(waypointFor(t, waypointTrack))
		case !want && had:
			delete(o.wpTracked, t.p.eid)
			o.p.trySendEv(attachproto.Waypoint{Op: waypointUntrack, UUID: t.p.uuid})
		}
	}
}

// waypointOnJoin cross-registers the joiner with everyone already present.
func (h *hub) waypointOnJoin(players map[int32]*tracked, nt *tracked) {
	h.waypointSync(players, nt, true)
	for _, o := range players {
		if o != nt {
			h.waypointSync(map[int32]*tracked{nt.p.eid: nt}, o, true)
		}
	}
}

// waypointOnMove re-tracks a mover whose block position changed.
func (h *hub) waypointOnMove(players map[int32]*tracked, t *tracked, moved bool) {
	if moved {
		h.waypointSync(players, t, true)
	}
}

// waypointTick re-checks every transmitter once a second, catching what
// changes without a move: an effect, a head put on, a game mode, the rule.
func (h *hub) waypointTick(players map[int32]*tracked) {
	for _, t := range players {
		h.waypointSync(players, t, false)
	}
}

// waypointOnLeave untracks a departing player for everyone.
func (h *hub) waypointOnLeave(players map[int32]*tracked, p *player) {
	f := attachproto.Waypoint{Op: waypointUntrack, UUID: p.uuid}
	for _, o := range players {
		delete(o.wpTracked, p.eid)
		o.p.trySendEv(f)
	}
}
