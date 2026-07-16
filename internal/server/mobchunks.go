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

func mobChunkOf(m *mob) [2]int32 {
	return [2]int32{int32(chunkFloor(m.x)), int32(chunkFloor(m.z))}
}

// reconcileMobChunks reloads mobs for chunks that entered range and unloads mobs
// for chunks that have left it past the grace window.
func (h *hub) reconcileMobChunks(players map[int32]*tracked, chunkSet map[[2]int32]bool) {
	if h.mobstore == nil {
		return
	}
	if h.activeChunks == nil {
		h.activeChunks = map[[2]int32]bool{}
	}
	if h.chunkOutAt == nil {
		h.chunkOutAt = map[[2]int32]uint64{}
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
		h.activeChunks[c] = true
		for _, sm := range h.mobstore.take(c[0], c[1]) {
			sm := sm
			h.reloadMob(players, &sm) // players in range get the EntityAdd for the reloaded mob
		}
	}
	h.reloading = false

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
	var evict [][2]int32
	for c, out := range h.chunkOutAt {
		if now-out >= mobUnloadGrace {
			evict = append(evict, c)
		}
	}
	if len(evict) == 0 {
		return
	}
	evicting := make(map[[2]int32]bool, len(evict))
	for _, c := range evict {
		evicting[c] = true
	}
	byChunk := map[[2]int32][]savedMob{}
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
		saved := byChunk[c]
		if prev := h.mobstore.take(c[0], c[1]); len(prev) > 0 {
			saved = append(prev, saved...) // merge with anything already parked there
		}
		h.mobstore.stash(c[0], c[1], saved)
		delete(h.activeChunks, c)
		delete(h.chunkOutAt, c)
	}
	for _, m := range drop {
		h.removeMob(players, m)
	}
}
