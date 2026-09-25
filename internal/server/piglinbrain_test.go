package server

import (
	"math"
	"testing"
)

// walkPiglin is a grown sword piglin on the walk (piglinWalk), as a natural
// spawn arms and marks it, with its first-hunt delay spent; it and the
// hoglin are kept from zombifying there — the question is the brain.
func walkPiglin(t *testing.T, h *hub, players map[int32]*tracked, x, y, z float64) *mob {
	t.Helper()
	m := h.spawnHostileYIn(players, entityPiglin, 0, x, y, z)
	if m == nil {
		t.Fatal("no piglin")
	}
	h.setPiglinBaby(players, m, false)
	m.held, m.huntedUntil, m.immuneZombify = itemGoldSword, 0, true
	return m
}

func walkHoglin(t *testing.T, h *hub, players map[int32]*tracked, x, y, z float64) *mob {
	t.Helper()
	g := h.spawnSpecies(players, entityHoglin, 0, x, y, z)
	if g == nil {
		t.Fatal("no hoglin")
	}
	g.baby, g.immuneZombify = false, true
	return g
}

// StartHuntingHoglin: a grown piglin with no grudge that has not hunted
// lately goes for the hoglin it sees, the piglin beside it joins in, and
// the hoglin, bitten, turns on its attacker.
func TestPiglinHuntsHoglin(t *testing.T) {
	h, _, players, x, y, z := piglinWalk(t)
	p := walkPiglin(t, h, players, float64(x)+0.5, float64(y), float64(z)+0.5)
	mate := walkPiglin(t, h, players, float64(x)+0.5, float64(y), float64(z)+3.5)
	g := walkHoglin(t, h, players, float64(x)+6.5, float64(y), float64(z)+0.5)
	h.tick.Add(mobMoveInterval)
	h.updateMobs(players)
	if p.targetEID != g.eid || mate.targetEID != g.eid {
		t.Fatalf("the hunt was not started and shared: targets %d, %d, want %d", p.targetEID, mate.targetEID, g.eid)
	}
	now := h.tick.Load()
	if p.huntedUntil <= now || mate.huntedUntil <= now {
		t.Fatal("a hunt did not put the piglins off hunting for a while")
	}
	// Two piglins to one hoglin: it retreats, as vanilla's does. With one
	// piglin left the fight is even; see it through.
	h.removeMob(players, mate)
	g.hogRetreat = 0
	full := g.health
	bitten := false
	for i := 0; i < 300 && !bitten; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		bitten = g.health < full
	}
	if !bitten {
		t.Fatal("the hunting piglin never hurt the hoglin")
	}
	if g.fightBack != p.eid {
		t.Fatalf("the bitten hoglin fights back at %d, want the piglin %d", g.fightBack, p.eid)
	}
}

// No hunt: a bastion piglin (CannotHunt), a bastion hoglin (CannotBeHunted),
// a piglin that hunted lately, and a baby.
func TestPiglinHuntGates(t *testing.T) {
	for _, c := range []string{"cannot-hunt", "cannot-be-hunted", "hunted-recently", "baby"} {
		h, _, players, x, y, z := piglinWalk(t)
		p := walkPiglin(t, h, players, float64(x)+0.5, float64(y), float64(z)+0.5)
		g := walkHoglin(t, h, players, float64(x)+6.5, float64(y), float64(z)+0.5)
		switch c {
		case "cannot-hunt":
			p.noHunt = true
		case "cannot-be-hunted":
			g.noHunt = true
		case "hunted-recently":
			p.huntedUntil = h.tick.Load() + 10000
		case "baby":
			h.setPiglinBaby(players, p, true)
		}
		for i := 0; i < 20; i++ {
			h.tick.Add(mobMoveInterval)
			h.updateMobs(players)
		}
		if p.targetEID == g.eid || p.preyTarget == g.eid {
			t.Errorf("%s: the piglin hunted the hoglin", c)
		}
	}
}

// A bastion's piglins and hoglins carry the template flags.
func TestBastionPiglinsCannotHunt(t *testing.T) {
	h, _, players, x, y, z := piglinWalk(t)
	h.structureSpawn.on, h.structureSpawn.hand = true, itemGoldSword
	p := h.spawnSpecies(players, entityPiglin, 0, float64(x)+0.5, float64(y), float64(z)+0.5)
	g := h.spawnSpecies(players, entityHoglin, 0, float64(x)+3.5, float64(y), float64(z)+0.5)
	h.structureSpawn.on, h.structureSpawn.hand = false, 0
	if !p.noHunt || !g.noHunt {
		t.Fatalf("bastion flags: piglin CannotHunt=%v, hoglin CannotBeHunted=%v", p.noHunt, g.noHunt)
	}
	wild := h.spawnSpecies(players, entityPiglin, 0, float64(x)+0.5, float64(y), float64(z)+6.5)
	if wild.noHunt || wild.huntedUntil <= h.tick.Load() {
		t.Fatalf("a natural piglin: CannotHunt=%v, hunted-recently until %d (now %d)", wild.noHunt, wild.huntedUntil, h.tick.Load())
	}
	sm := toSavedMob(g)
	h.reloading = true
	back := h.reloadMob(players, &sm)
	h.reloading = false
	if back == nil || !back.noHunt {
		t.Fatal("CannotBeHunted did not survive a reload")
	}
}

// StartCelebratingIfTargetDead: the hoglin it fought dies; the piglin
// forgets the grudge, will not hunt for a while, and goes to where it fell —
// dancing on the ticks whose roll says so, and stopping when a player hits it.
func TestPiglinCelebratesKill(t *testing.T) {
	var danceTick uint64
	for n := uint64(100); ; n++ {
		if piglinDanceRoll(n) {
			danceTick = n
			break
		}
	}
	for _, dance := range []bool{false, true} {
		h, _, players, x, y, z := piglinWalk(t)
		p := walkPiglin(t, h, players, float64(x)+0.5, float64(y), float64(z)+0.5)
		g := walkHoglin(t, h, players, float64(x)+8.5, float64(y), float64(z)+0.5)
		p.huntedUntil = h.tick.Load() + 10000
		h.piglinAngerAtMob(p, g)
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		if p.piglinFoe != g.eid {
			t.Fatalf("the piglin is not fighting the hoglin (foe %d)", p.piglinFoe)
		}
		h.killMob(players, g)
		want := danceTick
		if !dance {
			for want = danceTick + 1; piglinDanceRoll(want); want++ {
			}
		}
		h.tick.Add(want - h.tick.Load())
		h.updateMobs(players)
		if p.celebrateUntil == 0 {
			t.Fatal("the piglin did not celebrate the kill")
		}
		if p.anger != 0 || p.targetEID != 0 {
			t.Fatalf("the grudge outlived the hoglin: anger %d at %d", p.anger, p.targetEID)
		}
		if p.dancing != dance {
			t.Fatalf("dancing = %v on a tick whose roll is %v", p.dancing, dance)
		}
		if dance && speciesStateMeta(p) == nil {
			t.Fatal("a dancing piglin has no state for a late joiner")
		}
		start := math.Abs(p.x - (float64(x) + 8.5))
		for i := 0; i < 40; i++ {
			h.tick.Add(mobMoveInterval)
			h.updateMobs(players)
		}
		if d := math.Abs(p.x - (float64(x) + 8.5)); d >= start-2 {
			t.Fatalf("the piglin did not go to where the hoglin fell (%.1f → %.1f)", start, d)
		}
		pl := survPlayer(h)
		pl.dim = 0
		players[pl.p.eid] = pl
		h.piglinHurtByPlayer(players, p)
		if p.celebrateUntil != 0 || p.dancing {
			t.Fatal("a blow did not end the celebration")
		}
	}
}

// The nemesis: a grown piglin goes for a wither skeleton it sees; a baby
// runs from it; a brute fights it too.
func TestPiglinsFightNemesis(t *testing.T) {
	h, _, players, x, y, z := piglinWalk(t)
	p := walkPiglin(t, h, players, float64(x)+0.5, float64(y), float64(z)+0.5)
	ws := h.spawnHostileYIn(players, entityWitherSkeleton, 0, float64(x)+6.5, float64(y), float64(z)+0.5)
	if ws == nil {
		t.Fatal("no wither skeleton")
	}
	full := ws.health
	h.tick.Add(mobMoveInterval)
	h.updateMobs(players)
	if !p.hasTarget || p.preyTarget != ws.eid {
		t.Fatalf("the piglin ignores the wither skeleton (target %v, prey %d)", p.hasTarget, p.preyTarget)
	}
	for i := 0; i < 200 && ws.health >= full && ws.dying == 0; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
	}
	if ws.health >= full && ws.dying == 0 {
		t.Fatal("the piglin never struck its nemesis")
	}

	h, _, players, x, y, z = piglinWalk(t)
	baby := walkPiglin(t, h, players, float64(x)+2.5, float64(y), float64(z)+0.5)
	h.setPiglinBaby(players, baby, true)
	ws = h.spawnHostileYIn(players, entityWitherSkeleton, 0, float64(x)+0.5, float64(y), float64(z)+0.5)
	ws.frozen = true // hold it still: the question is the baby's
	h.tick.Add(mobMoveInterval)
	h.updateMobs(players)
	if baby.piglinFlee <= 0 || baby.piglinFleeFrom != ws.eid {
		t.Fatalf("the baby does not avoid the wither skeleton (flee %d from %d)", baby.piglinFlee, baby.piglinFleeFrom)
	}
	for i := 0; i < 20; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
	}
	if baby.x < float64(x)+4 {
		t.Fatalf("the baby did not run from the wither skeleton (x %.1f)", baby.x)
	}

	h, _, players, x, y, z = piglinWalk(t)
	b := h.spawnHostileYIn(players, entityPiglinBrute, 0, float64(x)+0.5, float64(y), float64(z)+0.5)
	ws = h.spawnHostileYIn(players, entityWitherSkeleton, 0, float64(x)+6.5, float64(y), float64(z)+0.5)
	ws.frozen = true
	h.tick.Add(mobMoveInterval)
	h.updateMobs(players)
	if !b.hasTarget || b.preyTarget != ws.eid {
		t.Fatalf("the brute ignores the wither skeleton (target %v, prey %d)", b.hasTarget, b.preyTarget)
	}
}
