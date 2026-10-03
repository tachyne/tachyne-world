package server

import (
	"math"
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
)

// strollCow is a cow on idleFloor's stone, committed to walking the way
// (vx, vz) points (the reroute heading the goals keep).
func strollCow(t *testing.T, h *hub, players map[int32]*tracked, vx, vz float64) *mob {
	t.Helper()
	m := h.spawnMob(players, entityCow, 0.5, 180, 0.5)
	if m == nil {
		t.Fatal("no cow")
	}
	m.baby, m.rest, m.stroll = false, 0, 0
	l := math.Hypot(vx, vz)
	m.vx, m.vz = vx/l*m.moveSpeed(), vz/l*m.moveSpeed()
	m.reroute = 1000
	return m
}

// MoveControl.rotlerp: told to walk the other way, a walker turns 90° a
// tick, and walks the way it faces while it turns (moveRelative goes by
// yRot) — through the mob update.
func TestBodyTurnsNinetyDegreesATick(t *testing.T) {
	h, players := idleFloor(t)
	m := strollCow(t, h, players, 0, -1) // north: yaw 180
	m.yaw = 0                            // facing south
	h.updateMobs(players)
	if math.Abs(float64(wrapDeg(m.yaw)+90)) > 1e-3 && math.Abs(float64(wrapDeg(m.yaw)-90)) > 1e-3 {
		t.Fatalf("after one tick the body faces %.1f, want ±90", m.yaw)
	}
	fx, fz := m.facing()
	if math.Abs(fz) > 1e-6 || math.Abs(fx) < 0.99 {
		t.Fatalf("facing (%.2f, %.2f) after a quarter turn", fx, fz)
	}
	x0 := m.x
	h.updateMobs(players)
	if d := math.Abs(float64(wrapDeg(m.yaw - 180))); d > 1e-3 {
		t.Fatalf("after two ticks the body faces %.1f, want 180", m.yaw)
	}
	if m.x == x0 {
		t.Error("the walker did not move the way it faced while it turned")
	}
	z0 := m.z
	for i := 0; i < 10; i++ {
		h.updateMobs(players)
	}
	if m.z >= z0 {
		t.Errorf("facing north it walked from z %.2f to %.2f", z0, m.z)
	}
}

// LookControl: the head turns to what it watches ten degrees a tick, and
// while the mob is going somewhere it stays within 75° of the body.
func TestHeadTurnsAtTheLookSpeed(t *testing.T) {
	m := &mob{etype: entityCow}
	m.lookTick() // seated
	m.headYaw = 90
	for i := 1; i <= 3; i++ {
		m.lookTick()
		if math.Abs(float64(m.lookYaw)-float64(10*i)) > 1e-3 {
			t.Fatalf("tick %d: head at %.1f, want %d", i, m.lookYaw, 10*i)
		}
	}
	m.vx, m.yaw, m.headYaw, m.lookYaw = 0.1, 0, 170, 160
	m.lookTick()
	if math.Abs(float64(m.lookYaw)-75) > 1e-3 {
		t.Errorf("a walking mob's head at %.1f, want held at 75 off the body", m.lookYaw)
	}
}

// ServerEntity.sendChanges: a walking mob's position goes out every third
// tick (an allay's every second), not every tick and not every goal update.
func TestTrackerSendsEveryUpdateInterval(t *testing.T) {
	for _, c := range []struct {
		etype int
		every int
	}{{entityCow, 3}, {entityAllay, 2}} {
		h, players := idleFloor(t)
		var pl *tracked
		for _, p := range players {
			pl = p
		}
		m := strollCow(t, h, players, 1, 0)
		if c.etype != entityCow {
			h.removeMob(players, m)
			m = h.spawnMob(players, c.etype, 0.5, 181, 0.5)
			m.vx, m.reroute = m.moveSpeed(), 1000
		}
		if pl.tracked == nil {
			pl.tracked = map[int32]bool{}
		}
		pl.tracked[m.eid] = true
		drain := func() (moves int) {
			for {
				select {
				case p := <-pl.p.out:
					if mv, ok := p.ev.(attachproto.EntityMove); ok && mv.EID == m.eid {
						moves++
					}
				default:
					return moves
				}
			}
		}
		h.updateMobs(players) // the first tick of tracking sends
		drain()
		const ticks = 36
		got := 0
		for i := 0; i < ticks; i++ {
			h.updateMobs(players)
			got += drain()
		}
		if want := ticks / c.every; got != want {
			t.Errorf("%s: %d position updates in %d ticks, want %d", entityNameOf(c.etype), got, ticks, want)
		}
	}
}

// The tuned species walk at MOVEMENT_SPEED × the goal's speed modifier as
// vanilla's goals pass it: a villager strolling at its brain's 0.5 goes at
// 0.25, an evoker's stroll at 0.6 of its 0.5, a frog ashore at the 0.1 its
// move control walks at — travel's steady state on an ordinary block.
func TestTunedSpeciesWalkAttributeTimesModifier(t *testing.T) {
	for _, c := range []struct {
		etype int
		mod   float64 // the engine goal's modifier
		speed float64 // vanilla's setSpeed
	}{
		{entityVillager, 1, 0.5 * 0.5},
		{entityEvoker, 0.6, 0.5 * 0.6},
		{entityWanderingTrader, 0.35, 0.7 * 0.35},
		{entityFrog, 1, 1.0 * 0.1},
	} {
		h, players := idleFloor(t)
		m := h.spawnMob(players, c.etype, 0.5, 180, 0.5)
		m.vx, m.vz = m.moveSpeed()*c.mod, 0
		speed, _, _ := m.wantedMove()
		if c.etype == entityFrog {
			m.yaw = -90 // facing +x: no turning slowdown
			speed = m.landMoveControl(speed, 1, 0)
		}
		if math.Abs(speed-c.speed) > 1e-9 {
			t.Errorf("%s: speed %.4f, want %.4f", entityNameOf(c.etype), speed, c.speed)
		}
		if got, want := walkPerUpdate(m, c.mod), mobGoalInterval*mobInputDrag*c.speed*c.speed/(1-defaultFriction*mobAirDrag); math.Abs(got-want) > 1e-9 {
			t.Errorf("%s: pace %.4f, want %.4f", entityNameOf(c.etype), got, want)
		}
	}
}

// TurtleMoveControl on the ground: halved to no less than 0.06 each tick,
// then an eighth of the way to the asked speed — it settles at 0.084 for a
// stroll at 1.0 of 0.25.
func TestTurtleLandMoveControl(t *testing.T) {
	m := &mob{etype: entityTurtle, onGround: true}
	var s float64
	for i := 0; i < 200; i++ {
		s = m.landMoveControl(0.25, 1, 0)
	}
	if want := 0.875*0.06 + 0.125*0.25; math.Abs(s-want) > 1e-9 {
		t.Errorf("turtle settles at %.5f, want %.5f", s, want)
	}
}
