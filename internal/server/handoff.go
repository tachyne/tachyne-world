package server

import (
	"encoding/json"
	"fmt"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-common/handover"
	"github.com/tachyne/tachyne-common/shard"
)

// The runtime player-handover state machine (release → ack → resume). It rides
// the warm peer mesh (peer.go) and moves authoritative state pod→pod; the
// gateway backend-swap that makes the crossing visible is PR4.

// peerSender is the slice of peerMesh the handover logic needs — an interface so
// tests can route frames in-process without real sockets.
type peerSender interface {
	send(peer int32, typ byte, v any) error
	connected(peer int32) bool
}

// handoff tracks a player release in flight, awaiting the destination's ack.
type handoff struct {
	t    *tracked
	dest int32
}

// checkSeamCrossing runs after a validated move: if the player stepped into a
// neighbour shard's territory (and we hold a warm link to it), begin an instant
// handover. Stepping into VOID (Unowned) is not a handover — the movement clamp
// keeps players inside the world there.
func (h *hub) checkSeamCrossing(players map[int32]*tracked, t *tracked) {
	if h.shardOf == nil || h.peers == nil || t.migrating != "" {
		return
	}
	dest := h.shardAt(t.x, t.z)
	if dest == h.sid || dest == shard.Unowned {
		return
	}
	if !h.peers.connected(dest) {
		return // no warm link — cannot hand over; the movement clamp holds the player
	}
	h.beginHandover(t, dest)
}

// beginHandover snapshots the player and sends it to the destination pod. The
// player is frozen locally (t.migrating) but NOT removed until the ack lands —
// make-before-break: if the destination never acks, the player stays here.
// nextMigID mints a per-pod-unique migration id (hub goroutine only).
func (h *hub) nextMigID() string {
	h.migSeq++
	return fmt.Sprintf("%d.%d", h.sid, h.migSeq)
}

func (h *hub) beginHandover(t *tracked, dest int32) {
	migID := h.nextMigID()
	snap := playerStateOf(t, t.p.name, t.p.uuid)
	// Register BEFORE sending: the ack may arrive (and in tests always does)
	// before send returns, and finishHandover must find the pending handoff.
	t.migrating = migID
	h.handoffs[migID] = &handoff{t: t, dest: dest}
	if err := h.peers.send(dest, handover.MsgMigrate, handover.MigrateEntity{
		Kind: handover.KindPlayer, MigID: migID, Player: &snap,
	}); err != nil {
		delete(h.handoffs, migID) // link dropped after connected() — roll back, keep the player
		t.migrating = ""
	}
}

// applyMigration receives an entity from a neighbour: it becomes ours. For a
// player we recreate it from the snapshot (same eid), announce it to everyone
// already here, and ack. The player's gateway session binds later, on resume
// (Hello{Purpose:"resume", token}) — PR4.
func (h *hub) applyMigration(players map[int32]*tracked, from int32, me handover.MigrateEntity) {
	switch me.Kind {
	case handover.KindPlayer:
		if me.Player == nil {
			return
		}
		// Hold the state pending until the player's gateway reconnects here with
		// Hello{Purpose:"resume", token} (ResumeRemote claims it). The player goes
		// live on this pod only when its session binds — no orphaned entity.
		h.setPending(me.MigID, *me.Player)
		h.peers.send(from, handover.MsgAck, handover.Ack{MigID: me.MigID, OK: true})
	case handover.KindMob:
		if me.Mob == nil {
			return
		}
		ms := *me.Mob
		eid := h.allocEID() // re-mint in THIS pod's lane (§9.6: mobs re-mint on migration)
		m := &mob{
			eid: eid, etype: int(ms.EType), dim: int(ms.Dim),
			x: ms.X, y: ms.Y, z: ms.Z, yaw: ms.Yaw, syaw: ms.HeadYaw,
			vx: ms.VX, vy: ms.VY, vz: ms.VZ, sx: ms.X, sy: ms.Y, sz: ms.Z,
			health: int(ms.Health), hostile: ms.Hostile, baby: ms.Baby, tamed: ms.Tamed,
			sitting: ms.Sitting, owner: ms.Owner, saddled: ms.Saddled, rider: ms.Rider,
			riders: ms.Riders, harness: ms.Harness, speed: speedFor(int(ms.EType)),
			uuid: ms.UUID,
		}
		m.behavior = migratedBehavior(m)
		h.mobs[eid] = m
		h.toNearbyEv(players, m.dim, m.x, m.z, entAdd(eid, m.etype, m.uuid, m.x, m.y, m.z, m.yaw, 0))
		// No ack: mob migration is fire-and-forget (a remove+add flicker is fine).
	default:
		h.peers.send(from, handover.MsgAck, handover.Ack{MigID: me.MigID, OK: false, Err: "kind not yet supported"})
	}
}

// mobStateOf snapshots a mob for migration. The behavior INTERFACE is not
// carried — it is re-resolved from the type on arrival (migratedBehavior).
func mobStateOf(m *mob) handover.MobState {
	return handover.MobState{
		EID: m.eid, EType: int32(m.etype), UUID: m.uuid, Dim: int32(m.dim),
		X: m.x, Y: m.y, Z: m.z, Yaw: m.yaw, HeadYaw: m.syaw,
		VX: m.vx, VY: m.vy, VZ: m.vz,
		Health: int32(m.health), Hostile: m.hostile,
		Baby: m.baby, Tamed: m.tamed, Sitting: m.sitting, Owner: m.owner,
		Saddled: m.saddled, Rider: m.rider, Riders: m.riders, Harness: m.harness,
	}
}

// migratedBehavior re-resolves a migrated mob's steering from its type. v1 is
// coarse (hostiles hunt, everything else wanders) — herd/villager/npc/pet nuance
// is lost across a seam for now (TODO: carry a behavior tag).
func migratedBehavior(m *mob) Behavior {
	if m.hostile {
		return hostileBehavior{}
	}
	return wanderBehavior{}
}

// migrateMobAcross hands a mob to the neighbour owning (nx,nz) if that is a
// connected neighbour shard. Returns true if it migrated (the mob is removed
// here). Returns false for void, no link, or a send failure — the caller then
// clamps (reroutes) the mob at the edge as before.
func (h *hub) migrateMobAcross(players map[int32]*tracked, m *mob, nx, nz float64) bool {
	if h.peers == nil {
		return false
	}
	dest := h.shardAt(nx, nz)
	if dest == h.sid || dest == shard.Unowned || !h.peers.connected(dest) {
		return false
	}
	ms := mobStateOf(m)
	ms.X, ms.Z = nx, nz // hand it over at the crossing point
	if h.peers.send(dest, handover.MsgMigrate, handover.MigrateEntity{Kind: handover.KindMob, MigID: h.nextMigID(), Mob: &ms}) != nil {
		return false // link dropped — keep the mob, clamp instead
	}
	h.removeMob(players, m) // vanish here; the dest re-adds under a fresh eid
	return true
}

// finishHandover completes a release once the destination acks: stop simulating
// the player here and tell its gateway to re-point to the destination pod (which
// keeps the client socket and swaps backends). Our own players see it despawn
// here and — since it is now real on and near the neighbour — reappear moments
// later as an inbound shadow the destination pushes back (shadow.go), so it stays
// continuously visible without a lingering ghost.
func (h *hub) finishHandover(players map[int32]*tracked, migID string, ok bool) {
	ho := h.handoffs[migID]
	if ho == nil {
		return
	}
	delete(h.handoffs, migID)
	if !ok {
		ho.t.migrating = "" // failed — the player stays here
		return
	}
	delete(players, ho.t.p.eid)
	h.toNearbyEv(players, ho.t.dim, ho.t.x, ho.t.z, entGone(ho.t.p.eid))
	h.toNearbyEv(players, ho.t.dim, ho.t.x, ho.t.z, infoGone(ho.t.p.uuid))
	h.shadowGoneAll(ho.t.p.eid) // it's real on the destination now — retract our shadow of it
	ho.t.p.trySendEv(attachproto.Rehome{DestSID: ho.dest, Token: migID})
}

// setPending stores a migrated player's snapshot for its gateway to claim on
// resume (hub goroutine). TODO: a deadline/TTL so an abandoned handover (client
// vanished mid-crossing) doesn't leak — SHARDING.md §9.3's 10s window.
func (h *hub) setPending(token string, ps handover.PlayerState) {
	h.pendingMu.Lock()
	h.pendingResume[token] = ps
	h.pendingMu.Unlock()
}

// claimPending pops a pending migrated snapshot by token (attach-session
// goroutine). ok is false if there is no such pending resume (stale/duplicate).
func (h *hub) claimPending(token string) (handover.PlayerState, bool) {
	h.pendingMu.Lock()
	defer h.pendingMu.Unlock()
	ps, ok := h.pendingResume[token]
	if ok {
		delete(h.pendingResume, token)
	}
	return ps, ok
}

// handlePeerFrame decodes and dispatches a peer frame on the hub goroutine.
func (h *hub) handlePeerFrame(players map[int32]*tracked, from int32, typ byte, payload []byte) {
	switch typ {
	case handover.MsgMigrate:
		var me handover.MigrateEntity
		if json.Unmarshal(payload, &me) == nil {
			h.applyMigration(players, from, me)
		}
	case handover.MsgAck:
		var ack handover.Ack
		if json.Unmarshal(payload, &ack) == nil {
			h.finishHandover(players, ack.MigID, ack.OK)
		}
	case handover.MsgShadow:
		var s handover.Shadow
		if json.Unmarshal(payload, &s) == nil {
			h.applyShadow(players, s)
		}
	case handover.MsgShadowGone:
		var sg handover.ShadowGone
		if json.Unmarshal(payload, &sg) == nil {
			h.applyShadowGone(players, sg.EID)
		}
	}
}
