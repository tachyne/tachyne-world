package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The target classes vanilla hangs on each species, with their selectors.
func TestPreyClasses(t *testing.T) {
	h := newHub(world.New(1))
	mk := func(et int, mods ...func(*mob)) *mob {
		// High and dry: the baby-turtle selector asks whether it is in water.
		m := &mob{eid: int32(et) + 1000, etype: et, x: 0.5, y: 200, z: 0.5}
		for _, f := range mods {
			f(m)
		}
		return m
	}
	baby := func(m *mob) { m.baby = true }
	cases := []struct {
		hunter, prey int
		mods         []func(*mob)
		want         bool
	}{
		{entityZombie, entityIronGolem, nil, true},
		{entityZombie, entityVillager, nil, true},
		{entityZombie, entityTurtle, []func(*mob){baby}, true},
		{entityZombie, entityTurtle, nil, false}, // an adult turtle is safe
		{entityZombie, entityCow, nil, false},
		{entityDrowned, entityAxolotl, nil, true},
		{entityZombie, entityAxolotl, nil, false}, // only the drowned hunts them
		{entityPillager, entityVillager, nil, true},
		{entityPillager, entityIronGolem, nil, true},
		{entityRavager, entityVillager, nil, true},
		{entitySlime, entityIronGolem, nil, true},
		{entityEnderman, entityEndermite, nil, true},
		{entityGuardian, entitySquid, nil, true},
		{entityGuardian, entityCow, nil, false},
		{entitySkeleton, entityTurtle, []func(*mob){baby}, true},
		{entityFox, entityChicken, nil, true},
		{entityFox, entityCod, nil, true},
		{entityPolarBear, entityFox, nil, true},
		{entityCreeper, entityVillager, nil, false}, // creepers hunt players only
		{entityWither, entityCow, nil, true},        // a wither fights everything living
		{entityWither, entityZombie, nil, false},    // …but not the undead
	}
	for _, c := range cases {
		if got := h.preyOf(mk(c.hunter), mk(c.prey, c.mods...)); got != c.want {
			t.Errorf("%s hunting %s = %v, want %v", entityNameByID[c.hunter], entityNameByID[c.prey], got, c.want)
		}
	}
}

// With no player about, a hunter latches onto its prey and bites it — and
// an iron golem, which nothing used to attack, is now fair game.
func TestZombieHuntsAndBitesAnIronGolem(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	z := h.spawnMob(players, entityZombie, 100, 70, 100)
	z.hostile = true
	g := h.spawnMob(players, entityIronGolem, 101, 70, 100)
	before := g.health

	h.acquireTarget(players, z)
	if z.preyTarget != g.eid || !z.hasTarget {
		t.Fatalf("a zombie with no player near should hunt the golem: prey=%d target=%v", z.preyTarget, z.hasTarget)
	}
	if !h.mobBitesPrey(players, z) {
		t.Fatal("the golem is in reach — the zombie should bite it")
	}
	if g.health >= before {
		t.Fatalf("the bite should hurt the golem: %d → %d", before, g.health)
	}
	// Out of reach, nothing lands.
	g.x = 140
	z.attackCD = 0
	if h.mobBitesPrey(players, z) {
		t.Fatal("a golem out of reach should not be bitten")
	}
}

// A zombie still infects a villager it kills — the special case vanilla
// makes of that bite survives the generalisation.
func TestZombieStillInfectsVillagers(t *testing.T) {
	h := newHub(world.New(1))
	h.rules.Difficulty = diffHard
	players := map[int32]*tracked{}
	z := h.spawnMob(players, entityZombie, 200, 70, 200)
	z.hostile = true
	v := h.spawnMob(players, entityVillager, 201, 70, 200)
	v.health = 1
	h.acquireTarget(players, z)
	if z.preyTarget != v.eid {
		t.Fatalf("the zombie should hunt the villager, prey=%d", z.preyTarget)
	}
	h.mobBitesPrey(players, z)
	zv := 0
	for _, m := range h.mobs {
		if m.etype == entityZombieVillager {
			zv++
		}
	}
	if zv != 1 {
		t.Fatalf("a killing bite on Hard should leave a zombie villager, got %d", zv)
	}
}

// Prey goals are mustSee where vanilla registers them so: a zombie does not
// lock onto an iron golem through a wall, though its villager goal (not
// mustSee) still finds a villager behind one. A zoglin reads only the
// living entities it can see.
func TestPreyNeedsLineOfSight(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.world.ForceLoad(0, 0, 2)
	for x := -4; x <= 10; x++ {
		for z := -4; z <= 4; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y < 186; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
				if x == 3 { // a full-height wall between hunter and prey
					h.world.SetBlock(x, y, z, worldgen.Stone)
				}
			}
		}
	}
	zm := h.spawnHostileY(players, entityZombie, 0.5, 180, 0.5)
	g := h.spawnMob(players, entityIronGolem, 6.5, 180, 0.5)
	h.acquireTarget(players, zm)
	if zm.preyTarget == g.eid {
		t.Fatal("a zombie must not acquire an iron golem it cannot see")
	}
	v := h.spawnMob(players, entityVillager, 6.5, 180, 2.5)
	h.acquireTarget(players, zm)
	if zm.preyTarget != v.eid {
		t.Fatalf("the zombie's villager goal is not mustSee: prey=%d want %d", zm.preyTarget, v.eid)
	}
	h.removeMob(players, v)
	zm.preyTarget = 0

	zg := h.spawnMob(players, entityZoglin, 9.5, 180, 3.5)
	zg.x = 9.5 // its side of the wall holds only the golem, which it may bite
	if p, _ := h.nearestHuntPrey(zg); p != g {
		t.Fatalf("a zoglin should pick the golem beside it, got %v", p)
	}
	h.removeMob(players, g)
	if p, _ := h.nearestHuntPrey(zg); p != nil {
		t.Fatalf("a zoglin must not pick a mob behind a wall (got %s)", entityNameByID[p.etype])
	}
	h.removeMob(players, zg)
	g = h.spawnMob(players, entityIronGolem, 6.5, 180, 0.5)
	for y := 180; y < 186; y++ { // open the wall: now both see the golem
		for z := -4; z <= 4; z++ {
			h.world.SetBlock(3, y, z, worldgen.Air)
		}
	}
	h.acquireTarget(players, zm)
	if zm.preyTarget != g.eid {
		t.Fatalf("with the wall gone the zombie should acquire the golem: prey=%d", zm.preyTarget)
	}
}
