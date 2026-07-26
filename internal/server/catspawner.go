package server

// The village cat spawner.
//
// Cats existed as a tameable species and nothing ever spawned one — `spawn.go`
// never mentioned them — so the only cats in the world were summoned ones.
// Vanilla runs them as a CustomSpawner rather than through the biome pools:
// every minute it picks a player, throws a point somewhere nearby, and if that
// point falls in a village with room for another cat, one appears.
//
// Vanilla's second half spawns a persistent black cat at a swamp hut. tachyne
// generates no swamp huts at all, so there is nothing for that branch to
// attach to and it is deliberately absent rather than approximated.

const (
	catSpawnInterval = 1200 // vanilla's TICK_DELAY: once a minute
	catVillageRadius = 48   // the radius its village and cat counts both use
	catVillageMax    = 5    // no more than this many cats around one village
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
	// Vanilla's offset: 8-31 blocks out on each axis, either side.
	x := int(t.x) + (8+h.rng.Intn(24))*h.randSign()
	z := int(t.z) + (8+h.rng.Intn(24))*h.randSign()
	h.catSpawnAt(players, x, z)
}

// catSpawnAt is the per-candidate-point decision: a cat appears only if that
// point sits in a village that is not already full of cats.
//
// The cap is measured around the POINT, not around the village centre — which
// is vanilla's rule, and means a village can end up holding more than
// catVillageMax cats overall as the candidate point wanders with the player.
// That is the behaviour, not a bug in it.
func (h *hub) catSpawnAt(players map[int32]*tracked, x, z int) bool {
	if !h.nearVillage(x, z, catVillageRadius) {
		return false
	}
	if h.countMobsNear(entityCat, 0, float64(x), float64(z), catVillageRadius) >= catVillageMax {
		return false // this corner of the village has all the cats it needs
	}
	y := h.world.SurfaceY(x, z)
	return h.spawnSpecies(players, entityCat, 0, float64(x)+0.5, y, float64(z)+0.5) != nil
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
