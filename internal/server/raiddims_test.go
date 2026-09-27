package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// gameplay/can_start_raid is off only in the Nether: a village built in the
// End is raided like an overworld one, while a Nether village eats the Bad
// Omen and the fuse burns out on nothing (BadOmenMobEffect turns the omen in
// any village; Raids.createOrExtendRaid is where the dimension says no).
func TestRaidsFollowCanStartRaid(t *testing.T) {
	if !dimType(dimOverworld).CanStartRaid || dimType(dimNether).CanStartRaid || !dimType(dimEnd).CanStartRaid {
		t.Fatal("can_start_raid is false in the Nether only")
	}
	for _, tc := range []struct {
		dim  int
		open func(int64, world.Store) (*world.World, error)
		y    int
		want bool
	}{
		{dimEnd, world.NewEnd, worldgen.EndSurfaceY + 4, true},
		{dimNether, world.NewNether, 100, false},
	} {
		h := newTestHub(world.New(1))
		w, _ := tc.open(7, nil)
		h.dims.set(tc.dim, w)
		h.rules.Difficulty = diffNormal
		center := blockPos{0, tc.y, 0}
		w.ForceLoad(center.x, center.z, 2)
		h.poiWorld(tc.dim)
		w.SetBlock(center.x, center.y, center.z, worldgen.BlockBase("bell"))
		pl := survPlayer(h)
		pl.dim = tc.dim
		pl.x, pl.y, pl.z = 0.5, float64(tc.y), 0.5
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players

		h.applyEffect(players, pl, effBadOmen, 0, badOmenSecs)
		h.checkRaidTrigger(players, pl)
		if pl.hasEffect(effBadOmen) != 0 || !pl.raidOmenSet {
			t.Fatalf("dim %d: a village should turn the Bad Omen into a Raid Omen", tc.dim)
		}
		h.raidOmenExpired(players, pl)
		r := h.raids[center]
		if (r != nil) != tc.want {
			t.Fatalf("dim %d: raid started %v, want %v", tc.dim, r != nil, tc.want)
		}
		if r == nil {
			continue
		}
		if r.dim != tc.dim {
			t.Fatalf("the raid runs in dim %d, want %d", r.dim, tc.dim)
		}
		// The bar shows to the player standing in the End village, and an
		// overworld player at the same coordinates is nowhere near it.
		h.updateRaids(players)
		if !r.shown[pl.p.eid] {
			t.Error("the End raid's bar is not shown to the player in the village")
		}
		if h.raidNear(dimOverworld, 0, 0) || !h.raidNear(tc.dim, 0, 0) {
			t.Error("raidNear should see the raid in its own dimension only")
		}
		// It survives a save in its dimension.
		h.mobstore = newMobStore("")
		h.mobstore.recordRaids(h.raids)
		h2 := newTestHub(world.New(1))
		h2.dims.set(tc.dim, w)
		h2.restoreRaids(h.mobstore.raids())
		if r2 := h2.raids[center]; r2 == nil || r2.dim != tc.dim {
			t.Fatalf("restored raid %+v, want one in dim %d", r2, tc.dim)
		}
	}
}
