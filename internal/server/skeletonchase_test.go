package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// skeletonRig lays a stone strip at y=199 from x=0 to x=40 and stands a
// survival player on it at x=18.5.
func skeletonRig(t *testing.T) (*hub, map[int32]*tracked, *tracked) {
	t.Helper()
	h := newHub(world.New(3))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.world.ForceLoad(20, 0, 1)
	stone := worldgen.BlockBase("stone")
	for x := 0; x <= 40; x++ {
		for z := -1; z <= 1; z++ {
			h.world.SetBlock(x, 199, z, stone)
		}
	}
	h.dayTime.Store(18000) // night: nothing burns
	pl := testTracked()
	pl.x, pl.y, pl.z = 18.5, 200, 0.5
	players[pl.p.eid] = pl
	return h, players, pl
}

// runUpdates steps the mobs n times and returns the mob's top speed.
func runUpdates(h *hub, players map[int32]*tracked, m *mob, n int) float64 {
	top := 0.0
	for i := 0; i < n; i++ {
		h.updateMobs(players)
		top = math.Max(top, math.Hypot(m.vx, m.vz))
	}
	return top
}

// AbstractSkeleton.reassessWeaponGoal: holding anything but a bow, a
// skeleton (and always a wither skeleton, with its sword) chases with
// MeleeAttackGoal(1.2) — faster than its walk.
func TestSwordSkeletonsChaseAtOnePointTwo(t *testing.T) {
	for _, etype := range []int{entityWitherSkeleton, entitySkeleton} {
		h, players, _ := skeletonRig(t)
		m := h.spawnHostileYIn(players, etype, dimOverworld, 4.5, 200, 0.5)
		m.held = itemByName["iron_sword"]
		if etype == entityWitherSkeleton {
			m.held = itemByName["stone_sword"]
		}
		m.gear[0] = invStack{item: itemByName["iron_helmet"], count: 1}
		h.reassessWeapon(m)
		sp := runUpdates(h, players, m, 20)
		if !m.hasTarget {
			t.Fatalf("type %d never took the player as its target", etype)
		}
		if want := m.moveSpeed() * 1.2; sp < want*0.9 || sp > want*1.01 {
			t.Errorf("type %d chases at %.3f a step, want about %.3f (1.2 × %.3f)", etype, sp, want, m.moveSpeed())
		}
	}
}

// RangedBowAttackGoal: a bowman inside its radius strafes at
// MoveControl.strafe's quarter speed, and the illusioner closes in at
// half its pace (RangedBowAttackGoal(0.5)).
func TestBowmenStrafeSlowAndIllusionerClosesAtHalf(t *testing.T) {
	h, players, _ := skeletonRig(t)
	m := h.spawnHostileYIn(players, entitySkeleton, dimOverworld, 28.5, 200, 0.5)
	m.held = itemBow
	h.reassessWeapon(m)
	sp := runUpdates(h, players, m, 16)
	if max := m.moveSpeed() * 0.25; sp > max {
		t.Errorf("a strafing skeleton moves %.3f a step, no more than %.3f", sp, max)
	}

	h, players, _ = skeletonRig(t)
	il := h.spawnHostileYIn(players, entityIllusioner, dimOverworld, 1.5, 200, 0.5)
	il.illMirrorNext = 1 << 40 // no spell in the way
	fastest := 0.0
	for i := 0; i < 16 && il.x < 3.2; i++ { // still outside its radius
		h.updateMobs(players)
		fastest = math.Max(fastest, math.Hypot(il.vx, il.vz))
	}
	if !il.hasTarget {
		t.Fatal("the illusioner never took the player as its target")
	}
	if want := il.moveSpeed() * 0.5; fastest > want*1.01 || fastest < want*0.3 {
		t.Errorf("the illusioner closes at up to %.3f a step, want about %.3f", fastest, want)
	}
}
