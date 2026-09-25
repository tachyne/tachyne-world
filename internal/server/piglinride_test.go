package server

import "testing"

// PiglinAi RIDE: a baby piglin with a baby hoglin in sight takes it for a
// ride within the forty seconds its ticker can run, a second baby piglin
// climbs on top of the first, and with the ride's memory gone they get off.
func TestBabyPiglinsRideABabyHoglin(t *testing.T) {
	h, _, players, x, y, z := piglinWalk(t)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = float64(x)+20.5, float64(y), float64(z)+6.5
	players[pl.p.eid] = pl
	baby := func(px float64) *mob {
		m := h.spawnHostileYIn(players, entityPiglin, 0, px, float64(y), float64(z)+0.5)
		h.setPiglinBaby(players, m, true)
		m.immuneZombify = true
		return m
	}
	a, b := baby(float64(x)+0.5), baby(float64(x)+1.5)
	hog := walkHoglin(t, h, players, float64(x)+4.5, float64(y), float64(z)+0.5)
	hog.baby = true
	hog.homeR, hog.vx, hog.vz = 0, 0, 0
	stacked := false
	for i := 0; i < 1200 && !stacked; i++ {
		h.tick.Add(mobMoveInterval)
		pl.health, pl.dead = 20, false
		h.updateMobs(players)
		stacked = a.mount != 0 && b.mount != 0
	}
	if !stacked {
		t.Fatalf("the babies never both climbed on: a on %d, b on %d (hoglin %d)", a.mount, b.mount, hog.eid)
	}
	low, high := a, b
	if b.mount == hog.eid {
		low, high = b, a
	}
	if low.mount != hog.eid || high.mount != low.eid {
		t.Fatalf("a stack: one on the hoglin, the other on it (a→%d b→%d hog %d)", a.mount, b.mount, hog.eid)
	}
	// The ride's memory runs out: the babies get down.
	for i := 0; i < 400 && (a.mount != 0 || b.mount != 0); i++ {
		h.tick.Add(mobMoveInterval)
		a.rideTicker, b.rideTicker = 1<<20, 1<<20 // no fresh ride in the meantime
		h.updateMobs(players)
	}
	if a.mount != 0 || b.mount != 0 {
		t.Fatalf("with RIDE_TARGET gone a baby riding a baby gets off (a→%d b→%d)", a.mount, b.mount)
	}
}
