package server

import (
	"testing"

	"tachyne/internal/world"
)

// Species wiring: each night-mob type gets the right behavior/health/day rules.

func TestSpawnHostileSpecies(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}

	z := h.spawnHostile(players, entityZombie, 0, 0)
	if !z.burns || !z.hostile || z.health != zombieHealth {
		t.Fatalf("zombie config wrong: %+v", z)
	}
	sk := h.spawnHostile(players, entitySkeleton, 2, 0)
	if !sk.burns || sk.health != skeletonHealth {
		t.Fatalf("skeleton config wrong: %+v", sk)
	}
	if _, ok := sk.behavior.(rangedBehavior); !ok {
		t.Fatal("skeleton must kite (rangedBehavior)")
	}
	sp := h.spawnHostile(players, entitySpider, 4, 0)
	if sp.burns || sp.speed != speedFor(entitySpider) || sp.health != spiderHealth {
		t.Fatalf("spider config wrong: %+v", sp)
	}
	c := h.spawnHostile(players, entityCreeper, 6, 0)
	if c.burns || c.health != creeperHealth {
		t.Fatalf("creeper config wrong: %+v", c)
	}
	if _, ok := c.behavior.(creeperBehavior); !ok {
		t.Fatal("creeper must use creeperBehavior")
	}
}

func TestSpiderNeutralByDayButRetaliates(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players := map[int32]*tracked{1: pl}
	sp := h.spawnHostile(players, entitySpider, 3, 0)
	sp.y = pl.y

	h.dayTime.Store(6000) // noon
	h.acquireTarget(players, sp)
	if sp.hasTarget {
		t.Fatal("spider must be neutral in daylight")
	}
	h.attackMob(players, 1, sp.eid) // punch it
	if sp.anger == 0 {
		t.Fatal("a hit spider must get angry")
	}
	h.acquireTarget(players, sp)
	if !sp.hasTarget {
		t.Fatal("an angry spider must hunt even at noon")
	}

	h.dayTime.Store(14000) // night: hostile regardless of anger
	fresh := h.spawnHostile(players, entitySpider, 5, 0)
	fresh.y = pl.y
	h.acquireTarget(players, fresh)
	if !fresh.hasTarget {
		t.Fatal("spiders must hunt at night")
	}
}

func TestFarHostilesDespawnPassiveStay(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.z = 0.5, 0.5
	players := map[int32]*tracked{1: pl}

	z := h.spawnHostile(players, entityZombie, 200, 200) // way out of range
	cow := h.spawnMob(players, entityCow, 200, 70, 200)
	near := h.spawnHostile(players, entityZombie, 10, 10) // in range — stays

	h.updateHostiles(players)
	if _, ok := h.mobs[z.eid]; ok {
		t.Fatal("a hostile with no player within range must despawn")
	}
	if _, ok := h.mobs[cow.eid]; !ok {
		t.Fatal("passive mobs must NOT despawn by distance")
	}
	if _, ok := h.mobs[near.eid]; !ok {
		t.Fatal("a hostile near a player must stay")
	}
}

// TestHappyGhastSpawnsPassiveFlyer: the 1.21.6 happy ghast spawns as a
// non-hostile free-flying mob with 20 HP, /summon-able and roster-passive.
func TestHappyGhastSpawnsPassiveFlyer(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	m := h.spawnSpecies(players, entityHappyGhast, 0, 100, 90, 100)
	if m.hostile {
		t.Error("happy ghast must be passive")
	}
	if !m.flies {
		t.Error("happy ghast must be a free flyer")
	}
	if m.health != 20 {
		t.Errorf("happy ghast health %d, want 20", m.health)
	}
	if !isRosterPassive(entityHappyGhast) {
		t.Error("happy ghast must be roster-passive (peaceful /summon)")
	}
	if summonable["happy_ghast"] != entityHappyGhast {
		t.Error("happy ghast must be /summon-able")
	}
}

func TestRollHostileTypeCoversAllSpecies(t *testing.T) {
	h := newHub(world.New(1))
	seen := map[int]bool{}
	for i := 0; i < 1000; i++ {
		seen[h.rollHostileType()] = true
	}
	for _, et := range []int{entityZombie, entitySkeleton, entitySpider, entityCreeper} {
		if !seen[et] {
			t.Fatalf("spawn table never produced entity type %d", et)
		}
	}
}

// ── Full-roster batch ────────────────────────────────────────────────────

// TestSpeciesItemNamesResolve guards the silent-air-drop bug: every item name
// a species references (drops, breeding food, held weapon) must resolve in
// itemByName. An unknown name maps to id 0 (air) — a phantom drop or empty
// hand — so we catch it here, not in-game.
func TestSpeciesItemNamesResolve(t *testing.T) {
	for etype, d := range speciesTable {
		check := func(kind, name string) {
			if name == "" {
				return
			}
			if _, ok := itemByName[name]; !ok {
				t.Errorf("%s (etype %d): unknown %s item %q", d.name, etype, kind, name)
			}
		}
		check("held", d.held)
		check("love", d.love)
		for _, sd := range d.drops {
			check("drop", sd.item)
		}
	}
}

// TestEverySpeciesSummonsAndConfigures spawns every table species and checks
// applySpecies produced a coherent mob: table health, a behavior, and the
// stance flags its archetype implies.
func TestEverySpeciesSummonsAndConfigures(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	for etype, d := range speciesTable {
		m := h.spawnSpecies(players, etype, 0, 100, 70, 100)
		if d.health != 0 && m.health != d.health {
			t.Errorf("%s: health %d, want %d", d.name, m.health, d.health)
		}
		switch d.arch {
		case archHostile, archRanged, archWaterHostile, archFlyerHostile, archStatic:
			if !m.hostile {
				t.Errorf("%s: hostile archetype but m.hostile=false", d.name)
			}
		case archWater:
			if !m.swims {
				t.Errorf("%s: water archetype but m.swims=false", d.name)
			}
		case archFlyer:
			if !m.flies {
				t.Errorf("%s: flyer archetype but m.flies=false", d.name)
			}
		}
		if m.behavior == nil {
			t.Errorf("%s: nil behavior", d.name)
		}
	}
}

// TestRosterSpeciesAreSummonable confirms every table species registered a
// /summon name via init().
func TestRosterSpeciesAreSummonable(t *testing.T) {
	for etype, d := range speciesTable {
		if got, ok := summonable[d.name]; !ok || got != etype {
			t.Errorf("%s not summonable (got %d, ok=%v)", d.name, got, ok)
		}
	}
}

// TestWitherSkeletonWithers checks melee status-effect wiring: a wither
// skeleton bite lays the wither effect on its victim.
func TestWitherSkeletonWithers(t *testing.T) {
	h := newHub(world.New(7))
	h.rules.Difficulty = diffNormal
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.dim, pl.x, pl.y, pl.z = 1, 100.5, 70, 100.5
	players[1] = pl
	m := h.spawnMobIn(players, entityWitherSkeleton, 1, 100.5, 70, 101.5)
	h.applySpecies(players, m)
	m.attackCD = 0
	h.mobMelee(players, m)
	if pl.hasEffect(effWither) == 0 {
		t.Fatal("wither skeleton bite should apply the wither effect")
	}
}

// TestParchedArrowWeakens checks the parched's signature: the arrows it fires
// carry the Weakness effect (vanilla Parched.getArrow).
func TestParchedArrowWeakens(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.x, pl.y, pl.z = 100.5, 70, 100.5
	players[1] = pl
	m := h.spawnSpecies(players, entityParched, 0, 100.5, 70, 110.5)
	h.spawnArrow(players, m, pl)
	got := false
	for _, a := range h.arrows {
		if a.shooter == m.eid {
			got = a.weaken == parchedWeaknessSecs
		}
	}
	if !got {
		t.Fatalf("parched arrow should carry Weakness for %d s", parchedWeaknessSecs)
	}
	// A skeleton's arrow must NOT (guards against a blanket stamp).
	sk := h.spawnHostile(players, entitySkeleton, 100, 70)
	h.spawnArrow(players, sk, pl)
	for _, a := range h.arrows {
		if a.shooter == sk.eid && a.weaken != 0 {
			t.Fatal("skeleton arrow must not carry Weakness")
		}
	}
}

// TestCaveSpiderPoisons checks difficulty-gated melee poison (vanilla: normal
// and hard only, never easy).
func TestCaveSpiderPoisons(t *testing.T) {
	for _, tc := range []struct {
		diff    int
		poisons bool
	}{{diffEasy, false}, {diffNormal, true}, {diffHard, true}} {
		h := newHub(world.New(7))
		h.rules.Difficulty = tc.diff
		players := map[int32]*tracked{}
		pl := testTracked()
		pl.x, pl.y, pl.z = 100.5, 70, 100.5
		players[1] = pl
		m := h.spawnHostileY(players, entityCaveSpider, 100.5, 70, 101.5)
		m.attackCD = 0
		h.mobMelee(players, m)
		if got := pl.hasEffect(effPoison) > 0; got != tc.poisons {
			t.Errorf("difficulty %d: poison=%v, want %v", tc.diff, got, tc.poisons)
		}
	}
}

// TestRetaliateWakesThePack: hitting one wolf turns nearby wolves hostile too.
func TestRetaliateWakesThePack(t *testing.T) {
	h := newHub(world.New(7))
	players := map[int32]*tracked{}
	pl := testTracked()
	pl.x, pl.y, pl.z = 100.5, 70, 100.5
	players[1] = pl
	a := h.spawnSpecies(players, entityWolf, 0, 101.5, 70, 100.5)
	b := h.spawnSpecies(players, entityWolf, 0, 103.5, 70, 100.5)
	if a.hostile || b.hostile {
		t.Fatal("wolves start peaceful")
	}
	h.provoke(a, pl)
	if !a.hostile || !b.hostile {
		t.Fatalf("hitting one wolf must anger the pack: a=%v b=%v", a.hostile, b.hostile)
	}
}
