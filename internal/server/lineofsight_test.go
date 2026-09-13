package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A wall between them: the skeleton holds its fire, the ghast never charges,
// the guardian lets go of its beam. Remove it and all three attack.
func TestRangedMobsNeedLineOfSight(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.world
	for x := -2; x <= 12; x++ {
		for z := -3; z <= 3; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	wall := func(on bool) {
		b := worldgen.Air
		if on {
			b = worldgen.Stone
		}
		for y := 180; y <= 184; y++ {
			for z := -3; z <= 3; z++ {
				w.SetBlock(4, y, z, b)
			}
		}
	}
	pl.x, pl.y, pl.z = 8.5, 180, 0.5
	s := h.spawnMob(players, entitySkeleton, 0.5, 180, 0.5)
	s.hostile = true
	wall(true)
	if h.sightClear(0, 0.5, 181.5, 0.5, 8.5, 181.6, 0.5) {
		t.Fatal("the ray passed through a stone wall")
	}
	h.skeletonShoot(players, s)
	if len(h.arrows) != 0 {
		t.Fatal("the skeleton shot through the wall")
	}
	if s.seeTime >= 0 {
		t.Errorf("seeTime %d while unseen, want negative", s.seeTime)
	}
	wall(false)
	if !h.sightClear(0, 0.5, 181.5, 0.5, 8.5, 181.6, 0.5) {
		t.Fatal("open air blocked the ray")
	}
	h.skeletonShoot(players, s)
	if len(h.arrows) != 1 {
		t.Fatalf("with a clear line the skeleton fired %d arrows, want 1", len(h.arrows))
	}
	if s.seeTime <= 0 {
		t.Errorf("seeTime %d once seen, want positive", s.seeTime)
	}

	g := h.spawnMob(players, entityGhast, 0.5, 181, 0.5)
	g.hostile = true
	wall(true)
	h.ghastTick(players, g)
	if g.ghastCharge != 0 {
		t.Errorf("the ghast charged %d behind a wall", g.ghastCharge)
	}
	wall(false)
	h.ghastTick(players, g)
	if g.ghastCharge == 0 {
		t.Error("the ghast did not charge with a clear line")
	}

	gd := h.spawnMob(players, entityGuardian, 0.5, 180, 0.5)
	gd.hostile = true
	h.guardianTick(players, gd)
	if gd.beamTarget != pl.p.eid {
		t.Fatal("the guardian did not lock on in the open")
	}
	wall(true)
	h.guardianTick(players, gd)
	if gd.beamTarget != 0 {
		t.Error("the guardian kept its beam on a player behind a wall")
	}
}

// The traversal crosses a diagonal cleanly and stops at a lone block on it.
func TestSightClearDiagonal(t *testing.T) {
	h := newHub(world.New(1))
	if !h.sightClear(0, 0.5, 200.5, 0.5, 7.5, 203.5, 6.5) {
		t.Fatal("empty sky blocked")
	}
	h.world.SetBlock(3, 201, 3, worldgen.Stone)
	if h.sightClear(0, 0.5, 200.5, 0.5, 7.5, 203.5, 6.5) {
		t.Fatal("a block on the diagonal was not seen")
	}
	h.world.SetBlock(3, 201, 3, worldgen.Water)
	if !h.sightClear(0, 0.5, 200.5, 0.5, 7.5, 203.5, 6.5) {
		t.Fatal("water blocked the line (vanilla clips no fluids)")
	}
	if h.sightClear(0, 0, 200, 0, 200, 200, 0) {
		t.Fatal("two hundred blocks is past the 128 limit")
	}
}
