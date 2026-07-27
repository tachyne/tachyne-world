package server

import (
	"testing"
)

func dragonFight(t *testing.T) (*hub, *mob, *tracked, map[int32]*tracked) {
	t.Helper()
	h, pl, players := endHub(t) // the End world has to exist before a dragon can
	pl.dim = 2
	pl.x, pl.y, pl.z = 0.5, 61, 0.5
	h.enterEnd(players, nil)
	if h.dragon == nil {
		t.Fatal("no dragon staged")
	}
	return h, h.dragon, pl, players
}

// The fight has a shape now: it circles, it commits to landing, it perches,
// it breathes, it climbs away. Drive it and watch the phases come round.
func TestDragonRunsItsPhases(t *testing.T) {
	h, m, _, players := dragonFight(t)
	// Crystals gone: vanilla's roll then makes landing near-certain, which is
	// the whole reason breaking them is the objective.
	h.crystals = map[int32]*crystal{}

	seen := map[int]bool{}
	pl := players[1]
	for i := 0; i < 20000; i++ {
		h.tick.Add(1)
		// Keep the subject alive: this is a test of the phase machine, and a
		// dead player is no target, so the dragon would skip the breath.
		pl.health, pl.dead = 20, false
		h.updateDragon(players)
		seen[m.dragonPhase] = true
		if seen[dragonPerched] && seen[dragonFlaming] && seen[dragonTakeoff] {
			break
		}
	}
	for _, p := range []struct {
		phase int
		name  string
	}{
		{dragonLandingApproach, "landing approach"},
		{dragonLanding, "landing"},
		{dragonPerched, "perched"},
		{dragonFlaming, "flaming"},
		{dragonTakeoff, "takeoff"},
	} {
		if !seen[p.phase] {
			t.Errorf("the dragon never reached the %s phase", p.name)
		}
	}
}

// Breaking the crystals is what brings it down: with all of them alive it
// should commit to landing far less readily than with none.
func TestCrystalsKeepTheDragonFlying(t *testing.T) {
	landsWith := func(crystals int) int {
		h, m, _, players := dragonFight(t)
		if crystals == 0 {
			h.crystals = map[int32]*crystal{}
		}
		lands := 0
		for i := 0; i < 4000; i++ {
			m.dragonPhase = dragonHolding
			h.dragonDecide(players, m)
			if m.dragonPhase == dragonLandingApproach {
				lands++
			}
		}
		return lands
	}
	none, all := landsWith(0), landsWith(20)
	if none <= all {
		t.Errorf("landing rolls: %d with no crystals vs %d with all — breaking them must help", none, all)
	}
}

// A perched dragon is the player's window: it must not still be grinding
// anyone who stands next to it with contact damage.
func TestPerchedDragonDoesNotGrind(t *testing.T) {
	h, m, pl, players := dragonFight(t)
	h.setDragonPhase(m, dragonPerched)
	m.x, m.y, m.z = pl.x, pl.y, pl.z // right on top of the player
	pl.health = 20
	pl.graceUntil = 0

	for i := 0; i < 50; i++ {
		h.tick.Add(1)
		h.updateDragon(players)
	}
	if pl.health < 20 {
		t.Errorf("a perched dragon dealt %v contact damage", 20-pl.health)
	}
}

// The breath is a cloud that sits at full radius and burns whoever stands in
// it — unlike a lingering potion, it does not shrink away.
func TestDragonBreathCloudBurnsAndHoldsItsRadius(t *testing.T) {
	h, _, pl, players := dragonFight(t)
	h.spawnBreathCloud(2, pl.x, pl.y, pl.z)
	var c *effectCloud
	for _, cl := range h.clouds {
		c = cl
	}
	if c == nil {
		t.Fatal("no breath cloud spawned")
	}
	if c.radius != breathRadius {
		t.Fatalf("breath radius %v, want %v", c.radius, breathRadius)
	}
	pl.health = 20
	for i := 0; i < 60; i++ {
		h.tick.Add(1)
		h.updateClouds(players)
	}
	if pl.health >= 20 {
		t.Error("standing in the dragon's breath cost nothing")
	}
	if c.radius != breathRadius {
		t.Errorf("the breath shrank to %v — it should hold its radius", c.radius)
	}
}
