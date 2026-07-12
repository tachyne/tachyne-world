package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestZombieBitesIdlePlayer reproduces the oracle combat experiment: a zombie
// summoned two blocks from a standing survival player must land bites.
func TestZombieBitesIdlePlayer(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	pl := testTracked()
	pl.gamemode = gmSurvival
	// Find open, walkable ground so the fight isn't blocked by water/trees.
	px, pz, found := 0, 0, false
	for x := 0; x < 300 && !found; x += 3 {
		for z := 0; z < 300 && !found; z += 3 {
			ok := true
			for dx := -2; dx <= 2 && ok; dx++ {
				for dz := -2; dz <= 2 && ok; dz++ {
					ok = w.Walkable(x+dx, z+dz)
				}
			}
			if ok {
				px, pz, found = x, z, true
			}
		}
	}
	if !found {
		t.Fatal("no walkable 5x5 patch found")
	}
	sy := w.SurfaceY(px, pz)
	pl.x, pl.y, pl.z = float64(px)+0.5, sy, float64(pz)+0.5
	players := map[int32]*tracked{pl.p.eid: pl}
	m := h.spawnHostile(players, entityZombie, px, pz+2)
	start := pl.health
	for i := 0; i < 300; i++ { // 300 mob updates ≈ 30 s of game time
		h.updateMobs(players)
		if pl.health < start {
			// Vanilla zombie melee at normal difficulty = 3 HP (oracle-measured).
			if got := int(start) - int(pl.health); got != 3 {
				t.Fatalf("zombie bite = %d HP, want 3 (vanilla normal)", got)
			}
			return
		}
	}
	t.Fatalf("no bite in 300 updates: zombie at (%.1f,%.1f,%.1f) player at (%.1f,%.1f,%.1f) rest=%d stroll=%d hasTarget=%v",
		m.x, m.y, m.z, pl.x, pl.y, pl.z, m.rest, m.stroll, m.hasTarget)
}

// TestZombieArmorHitsToKill pins vanilla's armor math (vanilla
// CombatRules): an armor-2 zombie takes 4.92 from a 5-damage hit, so 20 HP
// dies on the FIFTH hit, not the fourth — the fractional carry is what makes
// integer HP reproduce vanilla hits-to-kill.
func TestZombieArmorHitsToKill(t *testing.T) {
	m := &mob{health: 20, armor: 2}
	for i := 1; i <= 4; i++ {
		m.hurt(5)
		if m.health <= 0 {
			t.Fatalf("armor-2 zombie died on hit %d of a 5-damage weapon; vanilla needs 5", i)
		}
	}
	m.hurt(5)
	if m.health > 0 {
		t.Fatalf("armor-2 zombie survived 5 hits of 5 damage (health %d)", m.health)
	}
	// Unarmored control: 4 hits exactly.
	c := &mob{health: 20}
	for i := 0; i < 4; i++ {
		c.hurt(5)
	}
	if c.health > 0 {
		t.Fatalf("unarmored mob survived 4x5 damage (health %d)", c.health)
	}
}
