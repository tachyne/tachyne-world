package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A zombie picks a visible player out at twelve blocks, but not an invisible
// one (0.07 of its 35-block follow range is 2.45), nor one wearing a zombie
// head at twenty blocks (half the range) — and the invisible player in full
// armour is back in reach (0.7 × 1).
func TestInvisibilityHidesFromMobs(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.world.ForceLoad(0, 0, 3)
	for x := -4; x <= 28; x++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y < 184; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	pl.x, pl.y, pl.z = 12.5, 180, 0.5
	h.allocEID()
	z := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	reach := 35.0
	if got := h.huntTarget(players, z, reach); got != pl {
		t.Fatal("a zombie did not see a visible player twelve blocks off")
	}
	z.targetEID = 0
	h.applyEffect(players, pl, effInvisibility, 0, 60)
	if got := h.huntTarget(players, z, reach); got != nil {
		t.Fatal("a zombie picked out an invisible, unarmoured player at twelve blocks")
	}
	for i := range pl.armor {
		pl.armor[i] = invStack{item: itemByName["iron_helmet"], count: 1}
	}
	if got := h.huntTarget(players, z, reach); got != pl {
		t.Fatal("an invisible player in full armour should be seen at twelve blocks")
	}
	z.targetEID = 0
	h.removeEffect(pl, effInvisibility)
	pl.armor = [4]invStack{{item: itemByName["zombie_head"], count: 1}}
	pl.x = 20.5
	if got := h.huntTarget(players, z, reach); got != nil {
		t.Fatal("a zombie head did not halve the zombie's range")
	}
}

// invisPad is a stone floor with open air over it from x -4 to 36, a zombie
// at the origin and the test player standing at x on it.
func invisPad(t *testing.T, x float64) (*hub, map[int32]*tracked, *tracked, *mob) {
	t.Helper()
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.world.ForceLoad(0, 0, 3)
	for bx := -4; bx <= 36; bx++ {
		for bz := -3; bz <= 3; bz++ {
			h.world.SetBlock(bx, 179, bz, worldgen.Stone)
			for y := 180; y < 184; y++ {
				h.world.SetBlock(bx, y, bz, worldgen.Air)
			}
		}
	}
	pl.x, pl.y, pl.z = x, 180, 0.5
	h.allocEID()
	z := h.spawnHostileY(players, entityZombie, 0.5, 180, 0.5)
	h.gridDirty()
	return h, players, pl, z
}

// Through the mob update: the zombie's 35-block range shrinks to 0.07 for a
// bare invisible player (never below 2), 0.7 × the share of armour worn for
// a clad one, and 0.8 for one crouching.
func TestInvisibilityRangeThroughUpdateMobs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		x      float64
		invis  bool
		armour int
		sneak  bool
		want   bool
	}{
		{"visible at 12", 12.5, false, 0, false, true},
		{"invisible at 12", 12.5, true, 0, false, false},
		{"invisible at 3", 3.5, true, 0, false, false}, // 2.45 blocks
		{"invisible at 2", 2.4, true, 0, false, true},
		{"invisible, one piece, at 6", 6.5, true, 1, false, true}, // 6.125
		{"invisible, one piece, at 12", 12.5, true, 1, false, false},
		{"invisible, two pieces, at 12", 12.5, true, 2, false, true}, // 12.25
		{"invisible, two pieces, at 13", 13.5, true, 2, false, false},
		{"crouching at 27", 27.5, false, 0, true, true}, // 28
		{"crouching at 29", 29.5, false, 0, true, false},
	} {
		h, players, pl, z := invisPad(t, tc.x)
		if tc.invis {
			h.applyEffect(players, pl, effInvisibility, 0, 60)
		}
		for i := 0; i < tc.armour; i++ {
			pl.armor[i] = invStack{item: itemByName["iron_chestplate"], count: 1}
		}
		pl.sneaking = tc.sneak
		h.updateMobs(players)
		if got := z.hasTarget && z.targetEID == pl.p.eid; got != tc.want {
			t.Errorf("%s: zombie targeting %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A target already held is kept however invisible it turns (canContinueToUse
// reads no visibility); the shooting and fleeing steps' nearest-player pick
// follows the same rule.
func TestHeldTargetIgnoresInvisibility(t *testing.T) {
	h, players, pl, z := invisPad(t, 12.5)
	h.updateMobs(players)
	if z.targetEID != pl.p.eid {
		t.Fatal("the zombie did not pick out the visible player")
	}
	h.applyEffect(players, pl, effInvisibility, 0, 60)
	pl.x = 12.5 + (z.x - 0.5) // keep the same gap after its step
	if got := h.nearestTargetable(players, z, 35); got != pl {
		t.Error("the held target should stay targetable while invisible")
	}
	z.targetEID = 0
	if got := h.nearestTargetable(players, z, 35); got != nil {
		t.Error("an invisible player nobody holds was picked at twelve blocks")
	}
}

// LookAtPlayerGoal's TargetingConditions: a cow stares at a visible player
// six blocks off, never at an invisible one there.
func TestLookGoalMissesInvisiblePlayer(t *testing.T) {
	h, players, pl, z := invisPad(t, 6.5)
	cow := h.spawnMob(players, entityCow, 0.5, 180, 2.5)
	_ = z
	if h.nearestPlayerIn(players, cow, 8) != pl {
		t.Fatal("a cow should see a visible player six blocks off")
	}
	h.applyEffect(players, pl, effInvisibility, 0, 60)
	if h.nearestPlayerIn(players, cow, 8) != nil {
		t.Error("a cow looked at an invisible player six blocks off")
	}
}
