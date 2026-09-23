package server

// The village cat spawner: vanilla's CatSpawner, a CustomSpawner rather than
// a biome pool. Every minute it picks a player and throws a point 8-31
// blocks out on each axis at the player's own height. If the chunks around
// the point are loaded and a cat could stand there:
//   - near a village (within two sections of a claimed bed, workstation or
//     bell), with more than four claimed beds within 48 and fewer than five
//     cats in a 48×8×48 box, a cat appears;
//   - otherwise, inside a swamp hut with no cat within 16×8×16, a persistent
//     one appears (all black, by the structure check in catVariantRoll).
// It used to measure a radius around the well, drop the cat at the surface
// with no vertical limit, ignore whether anyone lived there, and skip the
// swamp hut, which it said did not exist.

const (
	catSpawnInterval = 1200 // vanilla's TICK_DELAY: once a minute
	catVillageRadius = 48   // spawnInVillage's radius: homes and cats
	catVillageMax    = 5    // fewer than this many cats in the box
	catHomesNeeded   = 4    // more than this many occupied homes
	catHutRadius     = 16   // spawnInHut's box
	catBoxHalfHeight = 8
)

// catSpawner is vanilla's CustomSpawner tick, rate-limiting itself.
func (h *hub) catSpawner(players map[int32]*tracked) {
	if !h.rules.DoMobSpawning {
		return
	}
	now := h.tick.Load()
	if now < h.catNextAt {
		return
	}
	h.catNextAt = now + catSpawnInterval
	t := h.randomPlayer(players)
	if t == nil || t.dim != 0 {
		return
	}
	x := floorInt(t.x) + (8+h.rng.Intn(24))*h.randSign()
	z := floorInt(t.z) + (8+h.rng.Intn(24))*h.randSign()
	h.catSpawnAt(players, x, floorInt(t.y), z)
}

// catSpawnAt is the per-point decision; true when a cat appeared.
func (h *hub) catSpawnAt(players map[int32]*tracked, x, y, z int) bool {
	w := h.world
	for cx := (x - 10) >> 4; cx <= (x+10)>>4; cx++ { // level.hasChunksAt(±10)
		for cz := (z - 10) >> 4; cz <= (z+10)>>4; cz++ {
			if !w.Loaded(int32(cx), int32(cz)) {
				return false
			}
		}
	}
	if !h.spawnPositionOK(0, catCreature, entityCat, x, y, z) {
		return false
	}
	switch {
	case sectionsToVillage(h.villageCentres(0), [3]int{x >> 4, y >> 4, z >> 4}) <= 2:
		if h.occupiedHomesNear(x, y, z, catVillageRadius) <= catHomesNeeded ||
			h.countCatsInBox(x, y, z, catVillageRadius) >= catVillageMax {
			return false
		}
		return h.spawnSpecies(players, entityCat, 0, float64(x)+0.5, float64(y), float64(z)+0.5) != nil
	case w.Gen().SwampHutIn(x, z).Contains(x, y, z):
		if h.countCatsInBox(x, y, z, catHutRadius) > 0 {
			return false
		}
		m := h.spawnSpecies(players, entityCat, 0, float64(x)+0.5, float64(y), float64(z)+0.5)
		if m != nil {
			m.persistent = true
		}
		return m != nil
	}
	return false
}

// occupiedHomesNear counts the claimed beds within r of a point: PoiManager
// getCountInRange(HOME, IS_OCCUPIED).
func (h *hub) occupiedHomesNear(x, y, z, r int) int {
	seen := map[blockPos]bool{}
	for _, m := range h.mobs {
		if m.etype != entityVillager || m.dim != 0 || m.dying > 0 || m.bed == (blockPos{}) || seen[m.bed] {
			continue
		}
		dx, dy, dz := m.bed.x-x, m.bed.y-y, m.bed.z-z
		if dx*dx+dy*dy+dz*dz <= r*r {
			seen[m.bed] = true
		}
	}
	return len(seen)
}

// countCatsInBox counts cats in the AABB of a block inflated r across and
// eight up and down.
func (h *hub) countCatsInBox(x, y, z, r int) (n int) {
	cx, cy, cz := float64(x)+0.5, float64(y)+0.5, float64(z)+0.5
	for _, m := range h.mobs {
		if m.etype != entityCat || m.dim != 0 || m.dying > 0 {
			continue
		}
		if absF(m.x-cx) <= float64(r)+0.5 && absF(m.y-cy) <= catBoxHalfHeight+0.5 && absF(m.z-cz) <= float64(r)+0.5 {
			n++
		}
	}
	return n
}

// randomPlayer picks one player at random, as vanilla's getRandomPlayer does.
func (h *hub) randomPlayer(players map[int32]*tracked) *tracked {
	n := 0
	var pick *tracked
	for _, t := range players {
		if t.dead || t.gamemode == gmSpectator {
			continue
		}
		n++
		if h.rng.Intn(n) == 0 { // reservoir sample: one pass, no allocation
			pick = t
		}
	}
	return pick
}

// countMobsNear counts live mobs of one type within a square radius.
func (h *hub) countMobsNear(etype, dim int, x, z float64, r float64) (n int) {
	for _, m := range h.mobs {
		if m.etype != etype || m.dim != dim || m.dying > 0 {
			continue
		}
		if absF(m.x-x) <= r && absF(m.z-z) <= r {
			n++
		}
	}
	return
}
