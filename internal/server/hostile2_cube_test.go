package server

import "testing"

// Slime.addTargetingGoals: the player goal only takes someone within 4
// blocks up or down — a slime at the foot of a cliff does not lock on to
// the player at the top. Once held, the target is kept at any height.
func TestSlimeTakesOnlyPlayersWithinFourBlocksOfHeight(t *testing.T) {
	for _, et := range []int{entitySlime, entityMagmaCube} {
		h, players := preyFixture(t)
		pl := survPlayer(h)
		players[pl.p.eid] = pl
		m := h.spawnHostileY(players, et, 0.5, 180, 0.5)
		pl.x, pl.y, pl.z = 4.5, 185, 0.5 // 5 up, in plain sight
		h.acquireTarget(players, m)
		if m.hasTarget {
			t.Errorf("%s: took a player 5 blocks up", advEntityName[et])
		}
		pl.y = 183.5
		h.acquireTarget(players, m)
		if !m.hasTarget {
			t.Fatalf("%s: ignored a player 3.5 blocks up", advEntityName[et])
		}
		pl.y = 186
		h.acquireTarget(players, m)
		if !m.hasTarget {
			t.Errorf("%s: dropped its target for climbing; the height test is for acquiring only", advEntityName[et])
		}
	}
}
