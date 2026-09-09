package server

// Ocean ruins: the structure is stamped by worldgen (oceanruin.go there); the
// server seeds what the templates' "drowned" DATA markers stand for when a
// player first arrives — vanilla spawns a persistent drowned at each — and
// remembers a seeded site so a cleared ruin stays cleared.

// populateOceanRuins seeds the drowned of any ocean ruin an overworld player
// has reached (once per site, persisted).
func (h *hub) populateOceanRuins(players map[int32]*tracked) {
	g := h.world.Gen()
	for _, t := range players {
		if t.dim != dimOverworld {
			continue
		}
		r := g.OceanRuinsIn(int(t.x), int(t.z))
		if !r.Exists {
			continue
		}
		key := [2]int32{int32(r.X), int32(r.Z)}
		if h.oceanRuinDone[key] {
			continue
		}
		dx, dz := t.x-float64(r.X), t.z-float64(r.Z)
		if dx*dx+dz*dz > 64*64 {
			continue // only once the player is at the site
		}
		h.oceanRuinDone[key] = true
		for _, p := range r.Pieces {
			for _, d := range p.Drowned {
				if m := h.spawnMob(players, entityDrowned, float64(d[0])+0.5, float64(d[1]), float64(d[2])+0.5); m != nil {
					m.persistent = true // Mob.setPersistenceRequired
				}
			}
		}
	}
}
