package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestPolarBearGuardsCub: a bear with a cub beside it turns on a player
// within ten blocks and rears up as it closes; alone, it leaves the player be.
func TestPolarBearGuardsCub(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 6.5, 180, 0.5
	b := h.spawnMob(players, entityPolarBear, 0.5, 180, 0.5)
	b.baby = false
	h.gridDirty()
	h.polarBearStep(players, b)
	if b.hostile {
		t.Fatal("a lone bear is peaceable")
	}
	cub := h.spawnMob(players, entityPolarBear, 1.5, 180, 0.5)
	cub.baby = true
	h.gridDirty()
	h.polarBearStep(players, b)
	if !b.hostile || !b.hasTarget {
		t.Fatal("a bear with a cub near turns on the player")
	}
	pl.x = 2.5
	b.attackCD = 0
	h.polarBearStep(players, b)
	if !b.bearStanding {
		t.Fatal("about to bite, it rears up")
	}
	pl.x = 12.5
	h.polarBearStep(players, b)
	if b.bearStanding {
		t.Fatal("and drops as the player draws off")
	}
}
