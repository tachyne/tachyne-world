package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestGolemIgnoresCreepers: an iron golem punches a zombie in reach but
// never a creeper, and does not walk toward one either.
func TestGolemIgnoresCreepers(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 60, 180, 60
	g := h.spawnMob(players, entityIronGolem, 0.5, 180, 0.5)
	c := h.spawnMob(players, entityCreeper, 1.5, 180, 0.5)
	h.gridDirty()
	hp := c.health
	g.attackCD = 0
	h.golemMelee(players, g)
	if c.health != hp {
		t.Fatal("a golem never punches a creeper")
	}
	if vx, vz := (golemBehavior{}).steer(h, g); vx != 0 || vz != 0 {
		t.Fatalf("nor walks at one: %.2f %.2f", vx, vz)
	}
	z := h.spawnMob(players, entityZombie, 1.5, 180, 1.5)
	z.hostile = true // spawnMob leaves the hunting flag to the hostile spawn path
	h.gridDirty()
	hp = z.health
	g.attackCD = 0
	h.golemMelee(players, g)
	if z.health >= hp {
		t.Fatal("but it punches a zombie")
	}
}
