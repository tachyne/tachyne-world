package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/handover"
	"github.com/tachyne/tachyne-common/shard"
)

// Cross-seam shadows make a shard border a WINDOW instead of a wall: an entity
// near a seam is streamed (read-only) to the neighbouring shard(s), so their
// players SEE it and their mobs can react to it BEFORE it crosses.
//
// Data flow, both halves no-ops when unsharded:
//   - outbound: this pod PUSHES shadows of its own near-border players and mobs
//     to the neighbours that can see them (updateShadows → pushShadow), and
//     retracts them (ShadowGone) as they drift out of range or leave.
//   - inbound: this pod RENDERS shadows it receives from neighbours to its local
//     players as ordinary entities (applyShadow → entAdd/entMove/entGone). The
//     owner stays authoritative; a shadow is pure render state here.
//
// Because a shadow renders through the same entity events the gateways already
// understand, this is entirely engine-side — no gateway change.

// shadowRadiusChunks is how close (in chunks) an entity must be to a neighbour's
// territory before we shadow it there: the neighbour's view radius, so a player
// at their border sees the entity exactly as it comes into view.
const shadowRadiusChunks = viewRadius

// shadowEnt is one inbound shadow — an entity owned by a neighbour that we render
// to our players. Keyed by the owner's (session-stable) eid.
type shadowEnt struct {
	kind     handover.Kind
	etype    int
	uuid     [16]byte
	name     string
	dim      int
	x, y, z  float64
	yaw      float32
	pitch    float32
	headYaw  float32
	baby     bool
	gamemode int  // player shadows: huntable only when survival
	added    bool // entAdd already emitted (a re-send is then a move, not a spawn)
}

// updateShadows pushes this pod's near-border players and mobs to the neighbours
// that can see them, retracting any that drifted out of range. Called on the
// mob-movement cadence; a no-op when unsharded.
func (h *hub) updateShadows(players map[int32]*tracked) {
	if h.peers == nil || h.shardOf == nil {
		return
	}
	for _, t := range players {
		if t.migrating != "" {
			continue // mid-handover: the destination is taking ownership
		}
		if t.dead {
			h.shadowGoneAll(t.p.eid) // a corpse casts no shadow (nothing to see or hunt)
			continue
		}
		h.pushShadow(t.p.eid, t.dim, t.x, t.z, shadowOfPlayer(t))
	}
	for _, m := range h.mobs {
		if m == h.dragon {
			continue // boss: NoSync movement, never shadowed
		}
		h.pushShadow(m.eid, m.dim, m.x, m.z, shadowOfMob(m))
	}
}

// pushShadow sends a Shadow to every connected neighbour within awareness range
// of (x,z) and a ShadowGone to any neighbour that held this eid last time but is
// now out of range. h.shadowOut remembers which neighbours currently hold it.
func (h *hub) pushShadow(eid int32, dim int, x, z float64, s handover.Shadow) {
	prev := h.shadowOut[eid]
	var next map[int32]bool
	for _, sid := range h.shadowTargets(dim, x, z) {
		if h.peers.send(sid, handover.MsgShadow, s) != nil {
			continue // link down — treat as out of range (retracted below)
		}
		if next == nil {
			next = make(map[int32]bool, len(prev)+1)
		}
		next[sid] = true
	}
	for sid := range prev {
		if !next[sid] {
			h.peers.send(sid, handover.MsgShadowGone, handover.ShadowGone{EID: eid})
		}
	}
	if next == nil {
		delete(h.shadowOut, eid)
	} else {
		h.shadowOut[eid] = next
	}
}

// shadowTargets is the set of connected neighbour SIDs whose territory lies
// within shadowRadiusChunks of chunk (x,z) — the neighbours that should render
// this entity. Dim is not filtered: regions apply to every dimension in v1.
func (h *hub) shadowTargets(dim int, x, z float64) []int32 {
	cx, cz := int32(chunkFloor(x)), int32(chunkFloor(z))
	var out []int32
	seen := map[int32]bool{}
	for _, r := range h.topo.Regions {
		if r.SID == h.sid || seen[r.SID] {
			continue
		}
		if regionChunkDist(r, cx, cz) <= shadowRadiusChunks && h.peers.connected(r.SID) {
			out = append(out, r.SID)
			seen[r.SID] = true
		}
	}
	return out
}

// regionChunkDist is the Chebyshev gap (in chunks) from chunk (cx,cz) to region
// r's rectangle; 0 when inside it.
func regionChunkDist(r shard.Region, cx, cz int32) int32 {
	return max(axisChunkDist(cx, r.MinCX, r.MinCX+r.W), axisChunkDist(cz, r.MinCZ, r.MinCZ+r.H))
}

// axisChunkDist is the gap from p to the half-open interval [lo,hi).
func axisChunkDist(p, lo, hi int32) int32 {
	switch {
	case p < lo:
		return lo - p
	case p >= hi:
		return p - (hi - 1)
	default:
		return 0
	}
}

func shadowOfPlayer(t *tracked) handover.Shadow {
	return handover.Shadow{
		EID: t.p.eid, Kind: handover.KindPlayer, Name: t.p.name, UUID: t.p.uuid,
		Dim: int32(t.dim), X: t.x, Y: t.y, Z: t.z,
		Yaw: t.yaw, Pitch: t.pitch, HeadYaw: t.yaw,
		OnGround: t.onGround, Sprinting: t.sprinting,
		Gamemode: int32(t.gamemode), // the neighbour's mobs hunt survival shadows only
	}
}

func shadowOfMob(m *mob) handover.Shadow {
	return handover.Shadow{
		EID: m.eid, Kind: handover.KindMob, EType: int32(m.etype), UUID: m.uuid,
		Dim: int32(m.dim), X: m.x, Y: m.y, Z: m.z,
		// Head = m.yaw, the mob's REAL facing — m.syaw is only the entHead
		// broadcast-dedup latch (up to 8° stale), not a pose to mirror.
		Yaw: m.yaw, HeadYaw: m.yaw, Baby: m.baby,
	}
}

// shadowGoneAll retracts an eid from every neighbour currently holding a shadow
// of it — called when the entity leaves this pod (despawn, death, disconnect, or
// a completed handover).
func (h *hub) shadowGoneAll(eid int32) {
	if h.peers == nil {
		return
	}
	for sid := range h.shadowOut[eid] {
		h.peers.send(sid, handover.MsgShadowGone, handover.ShadowGone{EID: eid})
	}
	delete(h.shadowOut, eid)
}

// ---- inbound: render a neighbour's shadow to our local players ----

// applyShadow registers or updates an inbound shadow and reflects it to nearby
// players (a spawn on first sight, else a move).
func (h *hub) applyShadow(players map[int32]*tracked, s handover.Shadow) {
	se := h.shadowIn[s.EID]
	if se == nil {
		se = &shadowEnt{}
		h.shadowIn[s.EID] = se
	}
	se.kind, se.etype, se.uuid, se.name = s.Kind, int(s.EType), s.UUID, s.Name
	se.dim = int(s.Dim)
	se.x, se.y, se.z = s.X, s.Y, s.Z
	se.yaw, se.pitch, se.headYaw, se.baby = s.Yaw, s.Pitch, s.HeadYaw, s.Baby
	se.gamemode = int(s.Gamemode)
	if !se.added {
		se.added = true
		h.spawnShadow(players, s.EID, se)
		return
	}
	h.toNearbyEv(players, se.dim, se.x, se.z, entMove(s.EID, se.x, se.y, se.z, se.yaw, se.pitch, true))
	h.toNearbyEv(players, se.dim, se.x, se.z, entHead(s.EID, se.headYaw))
}

// spawnShadow shows a first-seen inbound shadow to nearby players. A player needs
// a tab/profile entry before its entity renders; a mob just needs the add (plus a
// baby flag).
func (h *hub) spawnShadow(players map[int32]*tracked, eid int32, se *shadowEnt) {
	if se.kind == handover.KindPlayer {
		h.toNearbyEv(players, se.dim, se.x, se.z, attachproto.PlayerInfo{UUID: se.uuid, Name: se.name})
		h.toNearbyEv(players, se.dim, se.x, se.z, entAdd(eid, playerEntityType, se.uuid, se.x, se.y, se.z, se.yaw, se.pitch))
		return
	}
	h.toNearbyEv(players, se.dim, se.x, se.z, entAdd(eid, se.etype, se.uuid, se.x, se.y, se.z, se.yaw, 0))
	if se.baby {
		h.toNearbyEv(players, se.dim, se.x, se.z, metaEv(babyMeta(eid, true)))
	}
}

// applyShadowGone drops an inbound shadow. It emits an EntityRemove only if no
// REAL entity now holds that eid here — after a crossing the shadow's owner may
// already have become a real player on this pod (same session-stable eid), and
// removing it would despawn the real one.
func (h *hub) applyShadowGone(players map[int32]*tracked, eid int32) {
	se := h.shadowIn[eid]
	if se == nil {
		return
	}
	delete(h.shadowIn, eid)
	if players[eid] != nil || h.mobs[eid] != nil {
		return // superseded by a real entity — leave it be
	}
	h.toNearbyEv(players, se.dim, se.x, se.z, entGone(eid))
	if se.kind == handover.KindPlayer {
		h.toNearbyEv(players, se.dim, se.x, se.z, infoGone(se.uuid))
	}
}

// dropShadowSuperseded discards shadow bookkeeping for an eid that just became a
// REAL entity here (a completed crossing) WITHOUT an EntityRemove — the real
// entAdd takes over the same eid. Called from the resume path.
func (h *hub) dropShadowSuperseded(eid int32) { delete(h.shadowIn, eid) }

// showShadowsTo streams every current inbound shadow in the newcomer's dimension
// to them (mirrors onJoin's mob/item loops so a late joiner sees cross-seam life).
func (h *hub) showShadowsTo(nt *tracked) {
	for eid, se := range h.shadowIn {
		if se.dim != nt.dim {
			continue
		}
		if se.kind == handover.KindPlayer {
			nt.p.trySendEv(attachproto.PlayerInfo{UUID: se.uuid, Name: se.name})
			nt.p.trySendEv(entAdd(eid, playerEntityType, se.uuid, se.x, se.y, se.z, se.yaw, se.pitch))
			continue
		}
		nt.p.trySendEv(entAdd(eid, se.etype, se.uuid, se.x, se.y, se.z, se.yaw, 0))
		if se.baby {
			nt.p.trySendEv(metaEv(babyMeta(eid, true)))
		}
	}
}

// syncShadows re-asserts inbound shadow positions to nearby players — the 2 s
// absolute resync, mirroring broadcastSync for mobs (self-heals dropped moves).
func (h *hub) syncShadows(players map[int32]*tracked) {
	for eid, se := range h.shadowIn {
		h.toNearbyEv(players, se.dim, se.x, se.z, entMove(eid, se.x, se.y, se.z, se.yaw, se.pitch, true))
	}
}
