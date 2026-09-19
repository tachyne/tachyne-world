package server

// LocalMobCapCalculator: besides the global cap, vanilla refuses a category
// in a chunk when every player close to that chunk (within 128 blocks of
// its centre, ChunkMap.playerIsCloseEnoughForSpawning) already has the
// category's maxInstancesPerChunk mobs in the chunks close to THEM. With
// one player it is the global cap again; with several it stops one
// player's crowd from filling another's range.

type localCapState struct {
	players []*tracked
	counts  [][catCount]int // per players[i]
}

// closeForSpawning is playerIsCloseEnoughForSpawning: the player within 128
// blocks (2D, squared 16384) of the chunk's centre.
func closeForSpawning(t *tracked, cx, cz int32) bool {
	dx := float64(cx)*16 + 8 - t.x
	dz := float64(cz)*16 + 8 - t.z
	return dx*dx+dz*dz < 16384
}

// buildLocalCaps counts, per overworld player, the capped mobs in the
// chunks close to that player (SpawnState's addMob for every loaded mob).
func (h *hub) buildLocalCaps(players map[int32]*tracked) {
	st := &localCapState{}
	for _, t := range players {
		if t.dim == 0 {
			st.players = append(st.players, t)
		}
	}
	st.counts = make([][catCount]int, len(st.players))
	for _, m := range h.mobs {
		if m.dim != 0 || m.dying > 0 || h.spawnExempt(m) {
			continue
		}
		cat := mobSpawnCategory(m)
		cx, cz := int32(chunkFloor(m.x)), int32(chunkFloor(m.z))
		for i, t := range st.players {
			if closeForSpawning(t, cx, cz) {
				st.counts[i][cat]++
			}
		}
	}
	h.localCaps = st
}

// localCapAllows is canSpawnForCategoryLocal: true when some player close
// to the chunk is still under the category's per-player cap; false when
// none is, and false when no player is close at all.
func (h *hub) localCapAllows(cat int, c [2]int32) bool {
	st := h.localCaps
	if st == nil {
		return true
	}
	for i, t := range st.players {
		if closeForSpawning(t, c[0], c[1]) && st.counts[i][cat] < categoryCap[cat] {
			return true
		}
	}
	return false
}

// localCapAdd is addMob for a mob just spawned at (x,z).
func (h *hub) localCapAdd(cat, x, z int) {
	st := h.localCaps
	if st == nil {
		return
	}
	cx, cz := int32(chunkFloor(float64(x))), int32(chunkFloor(float64(z)))
	for i, t := range st.players {
		if closeForSpawning(t, cx, cz) {
			st.counts[i][cat]++
		}
	}
}
