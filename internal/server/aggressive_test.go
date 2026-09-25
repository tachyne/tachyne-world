package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The zombie family raises its arms while it is chasing, and drops them when
// it gives up — and walking through a village is not a chase.
func TestZombieRaisesArmsWhileChasing(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityZombie, 0.5, 70, 0.5)
	m.hostile = true
	if m.aggressive {
		t.Fatal("a zombie starts with its arms down")
	}
	m.hasTarget = true
	h.updateAggression(players, m)
	if !m.aggressive {
		t.Error("a chasing zombie raises its arms")
	}
	m.hasTarget = false
	h.updateAggression(players, m)
	if m.aggressive {
		t.Error("it drops them when it loses the target")
	}
	// Drifting through a village is not aggression.
	m.hasTarget, m.drifting = true, true
	h.updateAggression(players, m)
	if m.aggressive {
		t.Error("walking to a village spot must not raise the arms")
	}
	// A cow has no such flag.
	cow := h.spawnMob(players, entityCow, 2.5, 70, 0.5)
	cow.hasTarget = true
	h.updateAggression(players, cow)
	if cow.aggressive {
		t.Error("only the zombie family carries the flag")
	}
}

// The metadata is Mob's flags byte at index 15, the same on every version.
func TestMobFlagsMetaShape(t *testing.T) {
	b := mobFlagsMeta(7, true)
	// eid varint, index byte, type varint, value byte, terminator.
	if len(b) != 5 || b[1] != metaIndexMobFlags || b[3] != mobFlagAggressive {
		t.Fatalf("mob flags metadata: % x", b)
	}
	if off := mobFlagsMeta(7, false); off[3] != 0 {
		t.Fatalf("arms down should clear the byte: % x", off)
	}
}

// MoveThroughVillageGoal: after dark (and not by day) a zombie walks to the
// nearest village point a villager has claimed, then on to the next one it
// has not visited; points nobody holds are no village.
func TestZombieDriftsThroughVillageAtNight(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.world.ForceLoad(0, 0, 2)
	poiFloor(h, 0, 0, 16)
	bell := blockPos{10, 180, 0}
	h.world.SetBlock(bell.x, bell.y, bell.z, bellDefault)
	far := blockPos{-12, 180, 0}
	h.world.SetBlock(far.x, far.y, far.z, bellDefault)
	m := h.spawnHostileY(players, entityZombie, 0.5, 180, 0.5)

	h.dayTime.Store(18000) // midnight
	if h.villageDriftStep(players, m) {
		t.Fatal("bells nobody has claimed are no village")
	}
	v := h.spawnMob(players, entityVillager, 12.5, 180, 3.5)
	v.meet = bell
	w := h.spawnMob(players, entityVillager, -12.5, 180, 3.5)
	w.meet = far
	h.tick.Add(20) // the failed look waits a second before the next

	h.dayTime.Store(6000) // noon
	if h.villageDriftStep(players, m) {
		t.Error("a zombie does not walk the village by day")
	}
	h.dayTime.Store(18000)
	if !h.villageDriftStep(players, m) || !m.drifting || !m.hasTarget || m.driftPoi != bell {
		t.Fatalf("at night it heads for the nearest claimed point: drifting=%v poi=%v", m.drifting, m.driftPoi)
	}
	m.x, m.z = 9.5, 0.5 // arrived
	if !h.villageDriftStep(players, m) || m.driftPoi != far {
		t.Fatalf("once there it moves on to the next unvisited point, got %v", m.driftPoi)
	}
	// Something real to chase outranks it.
	m.drifting, m.hasTarget = false, true
	if h.villageDriftStep(players, m) {
		t.Error("a zombie with a target does not wander off")
	}
}

// Drowned.okTarget: by daylight a drowned only hunts somebody in the water,
// and it heads back to the water itself when the sun catches it ashore.
func TestDrownedDaylightRules(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	// A pool beside a dry bank at y=70.
	for dx := -3; dx <= 3; dx++ {
		for dz := -3; dz <= 3; dz++ {
			h.world.SetBlock(dx, 69, dz, worldgen.Stone)
			h.world.SetBlock(dx, 70, dz, worldgen.Air)
			h.world.SetBlock(dx, 71, dz, worldgen.Air)
		}
	}
	for dz := 2; dz <= 3; dz++ {
		h.world.SetBlock(0, 70, dz, worldgen.WaterBase)
	}
	pl := testTracked()
	pl.x, pl.y, pl.z = 1.5, 70, 0.5
	players[pl.p.eid] = pl
	m := h.spawnMob(players, entityDrowned, 0.5, 70, 0.5)
	m.hostile = true

	h.dayTime.Store(6000) // noon: a player on dry land is no target
	h.acquireTarget(players, m)
	if m.hasTarget {
		t.Error("a drowned does not chase across dry land at noon")
	}
	pl.x, pl.z = 0.5, 2.5 // …but one standing in the water is
	h.acquireTarget(players, m)
	if !m.hasTarget {
		t.Error("a drowned hunts whoever wades in, whatever the hour")
	}
	// Ashore by day with nobody to chase: it walks back to the water.
	m.hasTarget, m.targetEID = false, 0
	pl.x, pl.z = 40, 40
	if !h.drownedWaterStep(players, m) {
		t.Fatal("a dry drowned should head for water by day")
	}
	if !m.drownedGoal || m.tz < 1 {
		t.Errorf("it should aim at the pool: goal=%v tz=%v", m.drownedGoal, m.tz)
	}
}

// An errand never survives a real target: a drifting zombie that spots
// somebody drops the walk, raises its arms and chases.
func TestRealTargetOutranksTheErrand(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	m := h.spawnHostileY(players, entityZombie, pl.x+2, pl.y, pl.z)
	m.drifting, m.hasTarget = true, true
	m.tx, m.tz = 500, 500 // a village spot far away
	h.updateMobs(players)
	if m.drifting {
		t.Error("spotting a player ends the village walk")
	}
	if !m.hasTarget || math.Abs(m.tx-pl.x) > 2 {
		t.Errorf("it should be chasing the player, target (%v,%v)", m.tx, m.tz)
	}
	if !m.aggressive {
		t.Error("and its arms go up")
	}
}

// PiglinAi / PiglinBruteAi.updateActivity: a piglin or brute with an attack
// target is aggressive, which the client draws as a raised melee weapon.
func TestPiglinsRaiseTheirWeapons(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	for _, et := range []int{entityPiglin, entityPiglinBrute} {
		m := h.spawnMob(players, et, 0.5, 70, 0.5)
		m.hasTarget = true
		h.updateAggression(players, m)
		if !m.aggressive {
			t.Errorf("%s with a target is aggressive", entityNameByID[et])
		}
		m.hasTarget = false
		h.updateAggression(players, m)
		if m.aggressive {
			t.Errorf("%s without one is not", entityNameByID[et])
		}
	}
}
