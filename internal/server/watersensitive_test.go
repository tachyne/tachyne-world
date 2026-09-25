package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// waterPool fills a 5×5, three-deep pool at y=180 on preyFixture's floor.
func waterPool(h *hub) {
	for x := -2; x <= 2; x++ {
		for z := -2; z <= 2; z++ {
			for y := 180; y <= 182; y++ {
				h.world.SetBlock(x, y, z, worldgen.Water)
			}
		}
	}
}

// An enderman in water is hurt and, almost always on that very update,
// teleports out (Enderman.hurtServer's nine-in-ten teleport on a hurt no
// living thing dealt). Only rain used to move it, and water never did.
func TestEndermanInWaterIsHurtAndTeleports(t *testing.T) {
	h, players := preyFixture(t)
	waterPool(h)
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 40, 180, 40
	players[pl.p.eid] = pl
	moved := 0
	for i := 0; i < 5; i++ {
		m := h.spawnHostileY(players, entityEnderman, 0.5, 180, 0.5)
		hp := m.health
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		if m.health >= hp {
			t.Errorf("enderman %d: the water did not hurt it (health %v)", i, m.health)
		}
		if m.x != 0.5 || m.z != 0.5 {
			moved++
		}
		h.removeMob(players, m)
	}
	if moved < 4 {
		t.Errorf("only %d of 5 endermen teleported out of the water", moved)
	}
}

// A blaze standing in water takes 1 every ten ticks and stays put to take it.
func TestBlazeInWaterTakesOneEveryTenTicks(t *testing.T) {
	h, players := preyFixture(t)
	waterPool(h)
	m := h.spawnHostileY(players, entityBlaze, 0.5, 180, 0.5)
	hp := m.health
	for i := 0; i < 10; i++ { // 20 ticks
		m.x, m.y, m.z = 0.5, 180, 0.5
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
	}
	if got := hp - m.health; got != 2 {
		t.Errorf("20 ticks in water cost %v health, want 2", got)
	}
}
