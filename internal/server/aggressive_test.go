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

// A zombie inside a village walks through it after dark, and not by day.
func TestZombieDriftsThroughVillageAtNight(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	v := h.world.Gen().VillageIn(0, 0)
	if !v.Exists {
		t.Skip("no village in this seed's first cell")
	}
	m := h.spawnMob(players, entityZombie, float64(v.X)+5, float64(v.Y), float64(v.Z)+5)
	m.hostile = true

	h.dayTime.Store(6000) // noon
	if h.villageDriftStep(players, m) {
		t.Error("a zombie does not walk the village by day")
	}
	h.dayTime.Store(18000) // midnight
	if !h.villageDriftStep(players, m) {
		t.Fatal("a zombie in a village walks it at night")
	}
	if !m.drifting || !m.hasTarget {
		t.Fatalf("the drift should set a walk target: drifting=%v target=%v", m.drifting, m.hasTarget)
	}
	if d := math.Hypot(m.tx-float64(v.X), m.tz-float64(v.Z)); d > villageDriftSpread {
		t.Errorf("the spot should be inside the village, %v blocks out", d)
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
