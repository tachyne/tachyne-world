package server

import "math"

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
func (h *hub) buildLocalCaps(players map[int32]*tracked, dim int) {
	st := &localCapState{}
	for _, t := range players {
		if t.dim == dim {
			st.players = append(st.players, t)
		}
	}
	st.counts = make([][catCount]int, len(st.players))
	for _, m := range h.mobs {
		if m.dim != dim || m.dying > 0 || !h.countsTowardCaps(m) {
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

// PotentialCalculator: a biome may price a species (the soul sand valley's
// ghasts, skeletons, endermen and striders; the warped forest's endermen).
// Every loaded costed mob is a point charge; a new one may spawn only where
// the sum of charge/distance over them, times its own charge, stays within
// the species' energy budget — so they keep their distance instead of
// crowding, and a fresh spawn adds its own charge at once.

type pointCharge struct {
	x, y, z float64
	charge  float64
}

// buildSpawnPotential collects the dimension's charges for this tick.
func (h *hub) buildSpawnPotential(dim int) {
	h.spawnCharges = h.spawnCharges[:0]
	w := h.worldFor(dim)
	for _, m := range h.mobs {
		if m.dim != dim || m.dying > 0 || !h.countsTowardCaps(m) {
			continue
		}
		if c, ok := spawnCostFor(w.BiomeAt3D(int(m.x), int(m.y), int(m.z)), m.etype); ok {
			h.spawnCharges = append(h.spawnCharges, pointCharge{m.x, m.y, m.z, c.charge})
		}
	}
}

// spawnPotentialOK is SpawnState.canSpawn: true for an unpriced species,
// else the potential energy change at the spot must fit the budget.
func (h *hub) spawnPotentialOK(dim, etype, x, y, z int) bool {
	c, ok := spawnCostFor(h.worldFor(dim).BiomeAt3D(x, y, z), etype)
	if !ok || c.charge == 0 {
		return true
	}
	return h.spawnPotential(x, y, z)*c.charge <= c.budget
}

// spawnPotential is PotentialCalculator.getPotentialEnergyChange's sum:
// every charge over its distance (infinite on a charge's own spot).
func (h *hub) spawnPotential(x, y, z int) float64 {
	fx, fy, fz := float64(x), float64(y), float64(z)
	potential := 0.0
	for _, p := range h.spawnCharges {
		d2 := (p.x-fx)*(p.x-fx) + (p.y-fy)*(p.y-fy) + (p.z-fz)*(p.z-fz)
		if d2 == 0 {
			return math.Inf(1)
		}
		potential += p.charge / math.Sqrt(d2)
	}
	return potential
}

// addSpawnCharge is SpawnState.afterSpawn's addCharge for a fresh spawn.
func (h *hub) addSpawnCharge(dim, etype, x, y, z int) {
	if c, ok := spawnCostFor(h.worldFor(dim).BiomeAt3D(x, y, z), etype); ok && c.charge != 0 {
		h.spawnCharges = append(h.spawnCharges, pointCharge{float64(x), float64(y), float64(z), c.charge})
	}
}
