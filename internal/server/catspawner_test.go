package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// Nothing spawned cats at all, so the only ones in the world were summoned.
// They belong near villages, and only up to a point.
func TestVillageCatsSpawnAndCap(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	// Park the player next to a real village centre so the offset roll lands
	// inside one; without a village nothing should ever spawn.
	vx, vz, found := 0, 0, false
	gen := h.world.Gen()
	for r := 0; r < 12 && !found; r++ {
		if v := gen.VillageIn(r*384, 0); v.Exists {
			vx, vz, found = v.X, v.Z, true
		}
	}
	if !found {
		t.Skip("no village within range of the test seed")
	}
	pl.x, pl.z = float64(vx), float64(vz)
	pl.y = h.world.SurfaceY(vx, vz)

	count := func() int { return h.countMobsNear(entityCat, 0, float64(vx), float64(vz), catVillageRadius) }

	for i := 0; i < 600 && count() == 0; i++ {
		h.catNextAt = 0
		h.catSpawner(players)
	}
	if count() == 0 {
		t.Fatal("no cat ever spawned beside a village")
	}

	// The cap is measured around the CANDIDATE POINT, so test it there rather
	// than around the village centre — a wandering point legitimately leaves
	// more than catVillageMax cats in a village overall, which is vanilla's
	// behaviour and not something to assert against.
	for i := 0; i < 200; i++ {
		h.catSpawnAt(players, vx, vz)
	}
	if n := h.countMobsNear(entityCat, 0, float64(vx), float64(vz), catVillageRadius); n > catVillageMax {
		t.Errorf("%d cats within %d of one point, cap is %d", n, catVillageRadius, catVillageMax)
	}
	// …and once it is full, the point refuses outright.
	if h.catSpawnAt(players, vx, vz) {
		t.Error("a full village still accepted another cat")
	}
}

// Far from any village, no cats.
func TestNoCatsInTheWilderness(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	players[pl.p.eid] = pl

	// Somewhere with no village centre within the spawner's reach.
	for _, at := range [][2]int{{100000, 100000}, {-250000, 173000}} {
		if h.nearVillage(at[0], at[1], catVillageRadius+64) {
			continue
		}
		pl.x, pl.z = float64(at[0]), float64(at[1])
		for i := 0; i < 400; i++ {
			h.catNextAt = 0
			h.catSpawner(players)
		}
		if n := h.countMobsNear(entityCat, 0, pl.x, pl.z, 512); n != 0 {
			t.Errorf("%d cats spawned in open wilderness at %v", n, at)
		}
		return
	}
	t.Skip("could not find a village-free spot to test")
}
