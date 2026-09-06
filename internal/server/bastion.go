package server

// Bastion remnants: the structure is stamped by worldgen (bastion.go there);
// the server seeds its garrison — the piglins, brutes and hoglins the
// vanilla "mobs" pieces carry as entities — when a player first arrives,
// and remembers a seeded bastion so a cleared one stays cleared.

var bastionMobTypes = map[string]int{
	"piglin":       entityPiglin,
	"piglin_brute": entityPiglinBrute,
	"hoglin":       entityHoglin,
}

// populateBastions seeds the garrison of any bastion a Nether player has
// reached (once per bastion, persisted).
func (h *hub) populateBastions(players map[int32]*tracked) {
	w := h.worldFor(dimNether)
	if w == nil {
		return
	}
	g := w.Gen()
	for _, t := range players {
		if t.dim != dimNether {
			continue
		}
		b := g.BastionIn(int(t.x), int(t.z))
		if !b.Exists {
			continue
		}
		key := [2]int32{int32(b.X), int32(b.Z)}
		if h.bastionDone[key] {
			continue
		}
		dx, dz := t.x-float64(b.X), t.z-float64(b.Z)
		if dx*dx+dz*dz > 64*64 {
			continue // only once the player is at the bastion
		}
		h.bastionDone[key] = true
		for _, s := range g.BastionMobs(b) {
			et, ok := bastionMobTypes[s.Type]
			if !ok {
				continue
			}
			h.spawnSpecies(players, et, dimNether, float64(s.X)+0.5, float64(s.Y), float64(s.Z)+0.5)
		}
	}
}
