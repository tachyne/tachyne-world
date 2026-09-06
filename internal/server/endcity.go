package server

// End cities: the structure is stamped by worldgen (endcity.go there); the
// server seeds what vanilla's DATA markers stand for when a player first
// arrives — the shulker sentries and, in the ship, the item frame holding
// the elytra — and remembers a seeded city so a looted one stays looted.

// endCityFrameDir is the elytra frame's facing: vanilla hangs it facing
// SOUTH rotated by the piece (3D direction index: 2 north, 3 south, 4 west,
// 5 east).
var endCityFrameDir = [4]int32{3, 4, 2, 5}

// populateEndCities seeds any End city a player has reached (once, persisted).
func (h *hub) populateEndCities(players map[int32]*tracked) {
	w := h.worldFor(dimEnd)
	if w == nil {
		return
	}
	g := w.Gen()
	for _, t := range players {
		if t.dim != dimEnd {
			continue
		}
		c := g.EndCityIn(int(t.x), int(t.z))
		if !c.Exists {
			continue
		}
		key := [2]int32{int32(c.X), int32(c.Z)}
		if h.endCityDone[key] {
			continue
		}
		dx, dz := t.x-float64(c.X), t.z-float64(c.Z)
		if dx*dx+dz*dz > 96*96 {
			continue // only once the player is at the city (the ship sails far out)
		}
		h.endCityDone[key] = true
		for _, m := range g.EndCityMobs(c) {
			switch m.Type {
			case "shulker":
				h.spawnSpecies(players, entityShulker, dimEnd, float64(m.X)+0.5, float64(m.Y), float64(m.Z)+0.5)
			case "elytra_frame":
				f := &itemFrame{eid: h.allocEID(), x: m.X, y: m.Y, z: m.Z, dim: dimEnd,
					dir: endCityFrameDir[m.Rot&3], held: invStack{item: itemByName["elytra"], count: 1}}
				h.itemFrames[f.eid] = f
				h.showFrame(players, f)
			}
		}
	}
}
