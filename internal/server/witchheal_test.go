package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestRaidWitchHealsRaiders: a raid witch beside a hurt pillager throws it a
// regeneration (or healing) potion and holds off the players for ten
// seconds; a witch outside a raid heals nobody.
func TestRaidWitchHealsRaiders(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 40, 180, 40
	w := h.spawnMob(players, entityWitch, 0.5, 180, 0.5)
	p := h.spawnMob(players, entityPillager, 4.5, 180, 0.5)
	p.health = 3
	h.gridDirty()
	for i := 0; i < 5000; i++ {
		if h.witchHealTick(players, w) {
			t.Fatal("no raid, no healing")
		}
	}
	w.raidCenter, p.raidCenter = blockPos{0, 180, 0}, blockPos{0, 180, 0}
	threw := false
	for i := 0; i < 20000 && !threw; i++ {
		threw = h.witchHealTick(players, w)
	}
	if !threw || len(h.arrows) != 1 || w.witchHealCD != witchHealCooldown {
		t.Fatalf("a raid witch heals a hurt raider: threw %v potions %d cd %d", threw, len(h.arrows), w.witchHealCD)
	}
	for _, a := range h.arrows {
		if !a.splash || a.potion != potHealing || !a.mobShot {
			t.Fatalf("healing for a raider on three health: %+v", a)
		}
	}
}
