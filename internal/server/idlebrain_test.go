package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// RunOne's ShufflingList puts an option first in proportion to its weight:
// the frog's 1:1:3:2.
func TestRunOneWeights(t *testing.T) {
	h := newTestHub(world.New(1))
	const n = 70000
	var first [4]int
	for i := 0; i < n; i++ {
		h.runOne([]int{1, 1, 3, 2}, func(i int) bool { first[i]++; return true })
	}
	for i, w := range []float64{1, 1, 3, 2} {
		if got := float64(first[i]) / n; math.Abs(got-w/7) > 0.01 {
			t.Errorf("option %d first %.3f of the time, want %.3f", i, got, w/7)
		}
	}
}

// idleFloor lays stone at y=179 and clears the air above, with a creative
// player far off (so nothing is looked at).
func idleFloor(t *testing.T) (*hub, map[int32]*tracked) {
	t.Helper()
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 3)
	for x := -24; x <= 24; x++ {
		for z := -24; z <= 24; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y <= 184; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z, pl.gamemode = 0.5, 180, -22.5, gmCreative
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	return h, players
}

// FrogAi's idle RunOne, through the mob update: a frog ashore croaks and
// strolls, and never falls back on the generic rest.
func TestFrogIdleRunOne(t *testing.T) {
	h, players := idleFloor(t)
	f := h.spawnMob(players, entityFrog, 0.5, 180, 0.5)
	croaks, strolls := 0, 0
	var last *idleWalk
	wasCroaking := false
	for i := 0; i < 1500; i++ {
		h.tick.Add(mobMoveInterval)
		h.mobUpdate(players)
		if f.croakLeft > 0 && !wasCroaking {
			croaks++
		}
		wasCroaking = f.croakLeft > 0
		if w := f.idleWalk; w != nil && !w.still && w != last {
			last = w
			strolls++
		}
		if f.rest > 0 {
			t.Fatal("the frog took a generic rest")
		}
	}
	if croaks == 0 || strolls == 0 {
		t.Fatalf("croaks %d, strolls %d", croaks, strolls)
	}
	// Croak (3) against the stroll (1): the stroll takes its time, so only
	// the order of the counts is asserted.
	if croaks < strolls {
		t.Errorf("croaks %d fewer than strolls %d", croaks, strolls)
	}
}

// CamelAi's idle RunOne, through the mob update: a camel on its feet for
// twenty seconds sits down in time and gets up again, and RandomLookAround
// sets a gaze cooldown of 150-250 ticks.
func TestCamelIdleRunOne(t *testing.T) {
	h, players := idleFloor(t)
	h.tick.Store(1000)
	c := h.spawnMob(players, entityCamel, 0.5, 180, 0.5)
	c.baby = false
	sat, stood, gazed := false, false, false
	for i := 0; i < 6000 && !(sat && stood); i++ {
		h.tick.Add(mobMoveInterval)
		h.mobUpdate(players)
		if c.camelSitting() {
			sat = true
		} else if sat {
			stood = true
		}
		if c.gazeCD > 0 {
			gazed = true
			if c.gazeCD > randomLookGapHi {
				t.Fatalf("gaze cooldown %d", c.gazeCD)
			}
		}
	}
	if !sat || !stood || !gazed {
		t.Fatalf("sat %v, stood again %v, looked about %v", sat, stood, gazed)
	}
}

// The strider's RandomStrollGoal runs on an interval of 60, not 120: its
// rests between strolls are half the length.
func TestStriderRestsHalfAsLong(t *testing.T) {
	h, players := idleFloor(t)
	s := h.spawnMob(players, entityStrider, 0.5, 180, 0.5)
	most := 0
	for i := 0; i < 3000; i++ {
		h.tick.Add(mobMoveInterval)
		h.mobUpdate(players)
		most = max(most, s.rest)
	}
	if most == 0 || most > restMax*striderStrollEvery/120 {
		t.Fatalf("the strider's longest rest %d, want 1..%d", most, restMax*striderStrollEvery/120)
	}
}

// FishSwimGoal: a lone cod sets off for water cells now and then and
// floats in between; with nobody within 32 blocks for a hundred ticks it
// stops drawing; a school follower never draws.
func TestFishSwimGoal(t *testing.T) {
	h := newTestHub(world.New(1))
	h.world.ForceLoad(0, 0, 3)
	for x := -16; x <= 16; x++ {
		for z := -16; z <= 16; z++ {
			h.world.SetBlock(x, 169, z, worldgen.Stone)
			for y := 170; y <= 185; y++ {
				h.world.SetBlock(x, y, z, worldgen.Water)
			}
		}
	}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z, pl.gamemode = 0.5, 190, 0.5, gmCreative
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	f := h.spawnMob(players, entityCod, 0.5, 177, 0.5)
	swims := 0
	for i := 0; i < 1500; i++ {
		was := f.fishSwimSet
		h.tick.Add(mobMoveInterval)
		h.mobUpdate(players)
		if f.fishSwimSet && !was {
			swims++
			if math.Abs(f.fishSwimX-f.x) > fishSwimXZ+1 || math.Abs(f.fishSwimY-f.y) > fishSwimY+1 {
				t.Fatalf("a swim to (%.1f, %.1f, %.1f) from (%.1f, %.1f, %.1f)", f.fishSwimX, f.fishSwimY, f.fishSwimZ, f.x, f.y, f.z)
			}
		}
	}
	// One draw in twenty a tick, and a swim takes a while: several in 3000 ticks.
	if swims < 3 {
		t.Fatalf("%d swims in 3000 ticks", swims)
	}
	f.fishSwimSet, f.idleSecs = false, 6
	for i := 0; i < 200; i++ {
		h.tick.Add(mobMoveInterval)
		h.fishSwimStep(f)
		if f.fishSwimSet {
			t.Fatal("a fish with noActionTime past 100 drew a swim")
		}
	}
	f.idleSecs = 0
	g := h.spawnMob(players, entityCod, 2.5, 177, 0.5)
	g.schoolLeader = f.eid
	for i := 0; i < 200; i++ {
		h.tick.Add(mobMoveInterval)
		if h.fishSwimStep(g) || g.fishSwimSet {
			t.Fatal("a follower swims after its leader, not at random")
		}
	}
}
