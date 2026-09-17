package server

// Swamp huts: the structure is stamped by worldgen (swamphut.go there);
// the server seeds the witch and her cat (SwampHutPiece.postProcess, both
// persistent) when a player first comes near, and remembers a seeded hut
// so a cleared one stays cleared.

// populateSwampHuts seeds the witch and cat of any hut a player has reached
// (once per hut, persisted).
func (h *hub) populateSwampHuts(players map[int32]*tracked) {
	g := h.world.Gen()
	for _, t := range players {
		if t.dim != dimOverworld {
			continue
		}
		hut := g.SwampHutIn(int(t.x), int(t.z))
		if !hut.Exists {
			continue
		}
		key := [2]int32{int32(hut.X), int32(hut.Z)}
		if h.hutDone[key] {
			continue
		}
		dx, dz := t.x-float64(hut.X), t.z-float64(hut.Z)
		if dx*dx+dz*dz > 64*64 {
			continue // only once the player is at the hut
		}
		h.hutDone[key] = true
		x, y, z := hut.Home()
		if m := h.spawnSpecies(players, entityWitch, dimOverworld, float64(x)+0.5, float64(y), float64(z)+0.5); m != nil {
			m.persistent = true
		}
		if m := h.spawnSpecies(players, entityCat, dimOverworld, float64(x)+0.5, float64(y), float64(z)+0.5); m != nil {
			m.persistent = true // its coat is all_black by the structure check (catVariantRoll)
		}
	}
}
