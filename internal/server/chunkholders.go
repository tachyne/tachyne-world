package server

// Who holds chunks loaded, for the chunk stream's gate on a player who may
// load none of their own (a spectator while spectators_generate_chunks is
// off: ChunkMap.skipPlayer). Vanilla sends such a player only the chunks
// other tickets keep loaded — other players' views and forced chunks. The
// hub publishes that set as view windows a few times a second; the attach
// goroutines read the snapshot without touching hub state.

// chunkHolder is one window of loaded chunks: a player's view (radius r) or
// a forced chunk (r 0).
type chunkHolder struct {
	dim    int
	cx, cz int32
	r      int32
}

// publishChunkHolders snapshots the windows. Hub goroutine.
func (h *hub) publishChunkHolders(players map[int32]*tracked) {
	out := make([]chunkHolder, 0, len(players)+len(h.rules.Forced))
	for _, t := range players {
		if t.p.loadedOnly.Load() {
			continue // a skipped player holds nothing
		}
		out = append(out, chunkHolder{dim: t.dim, cx: int32(floorInt(t.x) >> 4), cz: int32(floorInt(t.z) >> 4), r: t.p.radius()})
	}
	for _, f := range h.rules.Forced {
		out = append(out, chunkHolder{dim: f.Dim, cx: f.X, cz: f.Z})
	}
	h.chunkHolders.Store(&out)
}

// heldLoaded reports whether a chunk is inside some holder's window. Safe
// from any goroutine.
func (h *hub) heldLoaded(dim, cx, cz int32) bool {
	hs := h.chunkHolders.Load()
	if hs == nil {
		return false
	}
	for _, c := range *hs {
		if int32(c.dim) == dim && abs32(cx-c.cx) <= c.r && abs32(cz-c.cz) <= c.r {
			return true
		}
	}
	return false
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
