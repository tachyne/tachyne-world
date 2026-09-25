package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// ServerPlayer.checkRidingStatistics: a ridden nautilus is nautilus_one_cm
// (it was credited as a horse), the distance is the move's 3D length, and
// everyone aboard a happy ghast is credited happy_ghast_one_cm (nothing
// was).
func TestRidingStatisticsByMount(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	n := h.spawnSpecies(players, entityNautilus, 0, 0.5, 60, 0.5)
	n.rider = pl.p.eid
	h.applyMountMove(players, pl, evVehicleMove{eid: pl.p.eid, x: 0.5 + 0.3, y: 60.4, z: 0.5})
	if got := customStat(pl, "nautilus_one_cm"); got != 50 {
		t.Errorf("nautilus_one_cm %d, want 50 (a 0.3 × 0.4 move)", got)
	}
	if got := customStat(pl, "horse_one_cm"); got != 0 {
		t.Errorf("a nautilus is no horse: horse_one_cm %d", got)
	}

	g := h.spawnSpecies(players, entityHappyGhast, 0, 10.5, 90, 10.5)
	g.riders = []int32{pl.p.eid}
	h.applyGhastMove(players, pl, evVehicleMove{eid: pl.p.eid, x: 10.5, y: 91, z: 10.5})
	if got := customStat(pl, "happy_ghast_one_cm"); got != 100 {
		t.Errorf("happy_ghast_one_cm %d, want 100", got)
	}
}
