package server

// Generated fluid that can run is ticked once, the first time this pod
// loads its chunk: vanilla ticks a spring the moment it places it, and
// without this a spring stood still in its cave wall, a block of water or
// lava hanging in the air (worldgen.UnstableFluids).

// fluidPrimeBudget bounds the chunks primed per tick, so a join does not
// scan a whole view window in one tick.
const fluidPrimeBudget = 8

// primeFluids schedules a fluid update at every unstable generated source in
// the chunks around each player that this pod has not primed yet.
func (h *hub) primeFluids(players map[int32]*tracked) {
	if h.fluidPrimed == nil {
		h.fluidPrimed = map[int]map[[2]int32]bool{}
	}
	budget := fluidPrimeBudget
	for _, t := range players {
		w := h.worldFor(t.dim)
		if w == nil {
			continue
		}
		done := h.fluidPrimed[t.dim]
		if done == nil {
			done = map[[2]int32]bool{}
			h.fluidPrimed[t.dim] = done
		}
		r := t.p.radius()
		cx, cz := int32(chunkFloor(t.x)), int32(chunkFloor(t.z))
		for x := cx - r; x <= cx+r; x++ {
			for z := cz - r; z <= cz+r; z++ {
				c := [2]int32{x, z}
				if done[c] {
					continue
				}
				if budget <= 0 {
					return
				}
				budget--
				done[c] = true
				for _, p := range w.UnstableFluids(x, z) {
					h.scheduleIn(t.dim, blockPos{p[0], p[1], p[2]}, 1)
				}
			}
		}
	}
}
