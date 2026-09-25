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

// The fight has vanilla's shape, driven every tick: it circles the node
// graph, commits to landing, drops onto the podium, scans for the player
// beside it, roars, breathes, and climbs away again.
func TestDragonRunsItsPhases(t *testing.T) {
	h, m, pl, players := dragonFight(t)
	// Crystals gone: vanilla's roll then makes landing likely, which is the
	// whole reason breaking them is the objective.
	h.crystals = map[int32]*crystal{}
	p := h.dragonPodium()
	seen := map[int]bool{}
	for i := 0; i < 40000; i++ {
		h.tick.Add(1)
		// The subject stands by the podium, alive: the sitting dragon's
		// scan wants a player within twenty and ten up or down.
		pl.x, pl.y, pl.z, pl.health, pl.dead, pl.graceUntil = 6.5, float64(p.y), 0.5, 20, false, h.tick.Load()+100
		h.updateDragon(players)
		seen[m.dragon().phase] = true
		if seen[phaseSittingFlaming] && seen[phaseTakeoff] {
			break
		}
	}
	for _, want := range []struct {
		phase int
		name  string
	}{
		{phaseHoldingPattern, "holding pattern"},
		{phaseLandingApproach, "landing approach"},
		{phaseLanding, "landing"},
		{phaseSittingScanning, "sitting scanning"},
		{phaseSittingAttack, "sitting attacking"},
		{phaseSittingFlaming, "sitting flaming"},
		{phaseTakeoff, "takeoff"},
	} {
		if !seen[want.phase] {
			t.Errorf("the dragon never reached the %s phase", want.name)
		}
	}
}

// Breaking the crystals is what brings it down: at the end of a leg the
// landing roll is one in crystals+3.
func TestCrystalsKeepTheDragonFlying(t *testing.T) {
	landsWith := func(crystals bool) int {
		h, m, _, players := dragonFight(t)
		if !crystals {
			h.crystals = map[int32]*crystal{}
		}
		lands := 0
		for i := 0; i < 2000; i++ {
			h.setDragonPhase(nil, m, phaseHoldingPattern)
			d := m.dragon()
			d.path, d.pathIdx = []dragonPoint{{0, 80, 0}}, 1 // a leg just flown
			h.dragonHoldingNewTarget(players, m)
			if d.phase == phaseLandingApproach {
				lands++
			}
		}
		return lands
	}
	none, all := landsWith(false), landsWith(true)
	if none <= all {
		t.Errorf("landing rolls: %d with no crystals vs %d with all — breaking them must help", none, all)
	}
}

// A dragon hit hard enough while it sits (a quarter of its health) takes
// off; an arrow does nothing to it there.
func TestSittingDragonTakesOffWhenHurt(t *testing.T) {
	h, m, _, players := dragonFight(t)
	h.setDragonPhase(players, m, phaseSittingScanning)
	m.dragon().lastHealth = m.health
	m.dragonHitArrow, m.dragonHitByPlayer = true, true
	m.hurtKind(20, dtArrow)
	if m.health != dragonHealth {
		t.Fatal("an arrow does nothing to a sitting dragon")
	}
	for i := 0; i < 6; i++ {
		m.invulnTicks = 0
		m.dragonMeleePart, m.dragonHitByPlayer = "head", true
		m.hurtKind(10, dtPlayerAttack)
	}
	h.tick.Add(1)
	h.updateDragon(players)
	if m.dragon().phase != phaseTakeoff {
		t.Fatalf("sixty damage while sitting should send it up: phase %d", m.dragon().phase)
	}
}

// A dragon struck down in the air does not fall where it is: it flies back
// to the podium at one health (DYING) and dies there.
func TestDragonFliesHomeToDie(t *testing.T) {
	h, m, _, players := dragonFight(t)
	m.x, m.y, m.z = 80, 100, 0
	h.hurtByEID(m, 0)
	m.health = 0
	h.killMob(players, m)
	if m.health != 1 || m.dragon().phase != phaseDying {
		t.Fatalf("killed in flight it heads home: hp %d phase %d", m.health, m.dragon().phase)
	}
	for i := 0; i < 3000 && h.dragon != nil; i++ {
		h.tick.Add(1)
		h.updateDragon(players)
	}
	if h.dragon != nil {
		t.Fatalf("the dragon never finished dying (at %.1f,%.1f,%.1f)", m.x, m.y, m.z)
	}
	if !h.rules.DragonDefeated {
		t.Fatal("the fight is won")
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
