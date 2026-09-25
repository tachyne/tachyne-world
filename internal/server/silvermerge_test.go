package server

import "testing"

// SilverfishMergeWithStoneGoal.canUse: only once its navigation is done —
// a silverfish partway through a stroll walks on over the stone, and one
// standing idle on it burrows in.
func TestSilverfishMergesOnlyWhenIdle(t *testing.T) {
	h, players := preyFixture(t)
	pl := survPlayer(h)
	pl.gamemode = gmCreative // nobody to go for
	pl.x, pl.y, pl.z = 12.5, 180, 4.5
	players[pl.p.eid] = pl
	m := h.spawnHostileY(players, entitySilverfish, 0.5, 180, 0.5)
	for i := 0; i < 150; i++ {
		m.stroll, m.rest = 1000, 0 // mid-walk
		h.updateMobs(players)
		if h.mobs[m.eid] == nil {
			t.Fatalf("update %d: a strolling silverfish burrowed into the stone", i)
		}
	}
	m.x, m.z = 0.5, 0.5
	for i := 0; i < 400 && h.mobs[m.eid] != nil; i++ {
		m.stroll, m.rest = 0, 1000 // standing idle
		h.updateMobs(players)
	}
	if h.mobs[m.eid] != nil {
		t.Fatal("an idle silverfish on stone should burrow into it")
	}
}
