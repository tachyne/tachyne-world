package server

// Per-chunk mob load/unload — the vanilla chunk-entity model. Each tick (from
// naturalSpawn, which already computes the loaded-chunk set) chunks that entered
// range reload their saved mobs, and chunks that have been out of range past a
// grace window write their live mobs back to the store and drop them from the
// ticking set. The live set therefore stays bounded by the loaded area rather
// than growing with everything ever explored. Applies to both spawner modes.

const (
	mobReloadBudget = 8   // chunks reloaded from the store per tick (bounds join/teleport bursts)
	mobUnloadGrace  = 100 // ticks (5 s) a chunk must be out of range before its mobs unload
)

// mobChunkOf is the chunk a mob stands in, with its dimension: {dim, cx, cz}.
func mobChunkOf(m *mob) [3]int32 {
	return [3]int32{int32(m.dim), int32(chunkFloor(m.x)), int32(chunkFloor(m.z))}
}

// reconcileMobChunks reloads mobs for chunks that entered range and unloads mobs
// for chunks that have left it past the grace window.
func (h *hub) reconcileMobChunks(players map[int32]*tracked, chunkSet map[[3]int32]bool) {
	if h.mobstore == nil {
		return
	}
	if h.activeChunks == nil {
		h.activeChunks = map[[3]int32]bool{}
	}
	if h.chunkOutAt == nil {
		h.chunkOutAt = map[[3]int32]uint64{}
	}
	if h.seededChunks == nil {
		h.seededChunks = map[[2]int32]bool{} // so reloaded herds mark their chunk seeded
	}
	now := h.tick.Load()

	// Reload chunks that just entered range (budgeted so a fresh join / teleport
	// does not restore a whole view window in one tick).
	budget := mobReloadBudget
	h.reloading = true
	for c := range chunkSet {
		if h.activeChunks[c] {
			delete(h.chunkOutAt, c) // back in range before it could unload
			continue
		}
		if budget <= 0 {
			continue // remaining chunks reload on later ticks (map order is random)
		}
		budget--
		h.activateMobChunk(players, c)
	}
	// A live mob outside every active chunk has walked or teleported out of
	// the loaded area. Its chunk becomes active here — its saved mobs load
	// beside it — and, out of every player's range, starts the unload clock
	// below like any chunk left behind, so it is saved with them. Left
	// inactive, the autosave would file the mob over that chunk's saved mobs
	// and the chunk's next load would bring back a copy beside the original.
	var strays [][3]int32
	for _, m := range h.mobs {
		if c := mobChunkOf(m); !h.activeChunks[c] && h.persistMob(m) {
			strays = append(strays, c)
		}
	}
	for _, c := range strays {
		if !h.activeChunks[c] {
			h.activateMobChunk(players, c)
		}
	}
	h.reloading = false

	h.unloadMobChunks(players, chunkSet, now)
}

// activateMobChunk makes a chunk's mobs live: its saved mobs reload, riders
// back on their mounts.
func (h *hub) activateMobChunk(players map[int32]*tracked, c [3]int32) {
	h.activeChunks[c] = true
	h.settleChunkEdits(players, int(c[0]), c[1], c[2]) // edits the regenerated ground no longer holds
	byOld := map[int32]*mob{}
	var riders []*mob
	for _, sm := range h.mobstore.take(c[0], c[1], c[2]) {
		sm := sm
		m := h.reloadMob(players, &sm) // players in range get the EntityAdd for the reloaded mob
		if m == nil {
			continue
		}
		if sm.EID != 0 {
			byOld[sm.EID] = m
		}
		if m.savedMount != 0 {
			riders = append(riders, m)
		}
	}
	h.relinkMounts(players, riders, byOld)
}

// unloadMobChunks runs the unload clock and saves the mobs of chunks past it.
func (h *hub) unloadMobChunks(players map[int32]*tracked, chunkSet map[[3]int32]bool, now uint64) {
	// Start the unload clock for active chunks that left range; cancel it for any
	// that came back.
	for c := range h.activeChunks {
		if chunkSet[c] {
			delete(h.chunkOutAt, c)
		} else if h.chunkOutAt[c] == 0 {
			h.chunkOutAt[c] = now
		}
	}

	// Evict chunks whose grace window has elapsed: save their live mobs and drop
	// them from the ticking set.
	var evict [][3]int32
	for c, out := range h.chunkOutAt {
		if now-out >= mobUnloadGrace {
			evict = append(evict, c)
		}
	}
	if len(evict) == 0 {
		return
	}
	evicting := make(map[[3]int32]bool, len(evict))
	for _, c := range evict {
		evicting[c] = true
	}
	byChunk := map[[3]int32][]savedMob{}
	var drop []*mob
	for _, m := range h.mobs {
		if !h.persistMob(m) {
			continue
		}
		if c := mobChunkOf(m); evicting[c] {
			byChunk[c] = append(byChunk[c], toSavedMob(m))
			drop = append(drop, m)
		}
	}
	for _, c := range evict {
		// The live mobs ARE the authoritative state of the evicting chunk —
		// stash them directly (overwrite). Do NOT merge with the existing
		// bucket: the 600-tick autosave (bucketLive) writes active chunks' live
		// mobs into the store, so merging that snapshot back in doubles the herd
		// on every autosave-then-unload cycle, growing exponentially as players
		// roam. An active chunk emptied its bucket on reload, so anything there
		// now is only that stale autosave copy of these same mobs.
		h.mobstore.stash(c[0], c[1], c[2], byChunk[c])
		delete(h.activeChunks, c)
		delete(h.chunkOutAt, c)
	}
	for _, m := range drop {
		h.removeMob(players, m)
	}
}

// reconcileEntityChunks computes every dimension's entity-ticking chunks and
// loads or unloads the mobs with them: every player's view window in the
// dimension they are in, and every forced chunk (ForceLoadCommand's FORCED
// ticket is entity ticking), so mobs in a /forceload'ed area stay loaded and
// keep moving with nobody online.
func (h *hub) reconcileEntityChunks(players map[int32]*tracked) {
	if h.mobstore == nil {
		return
	}
	if h.scratchEntityChunks == nil {
		h.scratchEntityChunks = map[[3]int32]bool{}
	}
	set := h.scratchEntityChunks
	clear(set)
	for _, t := range players {
		r := t.p.radius()
		d, cx, cz := int32(t.dim), int32(chunkFloor(t.x)), int32(chunkFloor(t.z))
		for x := cx - r; x <= cx+r; x++ {
			for z := cz - r; z <= cz+r; z++ {
				set[[3]int32{d, x, z}] = true
			}
		}
	}
	for dim := 0; dim <= 2; dim++ {
		w := h.worldFor(dim)
		if w == nil || (dim != 0 && w == h.world) { // a test hub serves one world for all three
			continue
		}
		for _, c := range w.ForcedChunks() {
			set[[3]int32{int32(dim), c[0], c[1]}] = true
		}
	}
	h.reconcileMobChunks(players, set)
}

// anyForced reports whether any dimension has a forced chunk.
func (h *hub) anyForced() bool {
	for dim := 0; dim <= 2; dim++ {
		if w := h.worldFor(dim); w != nil && (dim == 0 || w != h.world) && w.ForcedCount() > 0 {
			return true
		}
	}
	return false
}
