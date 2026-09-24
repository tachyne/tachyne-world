package server

import attachproto "github.com/tachyne/tachyne-common/attach"

// The cracks other players see on a block someone is mining. Vanilla's
// ServerPlayerGameMode adds the block's destroy progress every tick of a dig
// and, whenever the stage (progress × 10) changes, sends it to the players
// around (Level.destroyBlockProgress); an abort, a finish or a dig moved
// elsewhere clears it. The digger's own client draws its own cracks. Until
// 2026-09-24 nobody else ever saw a player mine.

// digCrack is one player's dig in progress.
type digCrack struct {
	dim      int
	pos      blockPos
	state    uint32 // the block when the dig began
	progress float64
	stage    int8
}

// evDigStart and evDigStop come from the session's player_action handling.
type evDigStart struct {
	eid     int32
	dim     int
	x, y, z int
}

type evDigStop struct{ eid int32 }

func (evDigStart) isHubEvent() {}
func (evDigStop) isHubEvent()  {}

// startDig begins (or moves) a player's dig.
func (h *hub) startDig(players map[int32]*tracked, e evDigStart) {
	t := players[e.eid]
	if t == nil {
		return
	}
	h.stopDig(players, e.eid)
	w := h.worldFor(e.dim)
	if w == nil {
		return
	}
	pos := blockPos{e.x, e.y, e.z}
	h.digs[e.eid] = &digCrack{dim: e.dim, pos: pos, state: w.At(pos.x, pos.y, pos.z), stage: -1}
}

// stopDig ends a player's dig and clears its cracks.
func (h *hub) stopDig(players map[int32]*tracked, eid int32) {
	d := h.digs[eid]
	if d == nil {
		return
	}
	delete(h.digs, eid)
	if d.stage >= 0 {
		h.sendDigStage(players, eid, d, -1)
	}
}

// tickDigCracks adds a tick of progress to every dig and sends stage changes.
func (h *hub) tickDigCracks(players map[int32]*tracked) {
	for eid, d := range h.digs {
		t := players[eid]
		w := h.worldFor(d.dim)
		if t == nil || t.dead || t.dim != d.dim || w == nil || w.At(d.pos.x, d.pos.y, d.pos.z) != d.state {
			h.stopDig(players, eid) // gone, moved on, or the block changed under it
			continue
		}
		d.progress += h.destroyProgress(t, d.state)
		stage := int8(min(d.progress*10, 10))
		if stage != d.stage {
			d.stage = stage
			h.sendDigStage(players, eid, d, stage)
		}
	}
}

// sendDigStage shows one stage to the players watching the digger (0-9; any
// other value clears the cracks).
func (h *hub) sendDigStage(players map[int32]*tracked, eid int32, d *digCrack, stage int8) {
	t := players[eid]
	x, z := float64(d.pos.x), float64(d.pos.z)
	if t != nil {
		x, z = t.x, t.z
	}
	h.toTracking(players, eid, d.dim, x, z, attachproto.BlockBreakProgress{
		EID: eid, X: int32(d.pos.x), Y: int32(d.pos.y), Z: int32(d.pos.z), Progress: stage})
}
