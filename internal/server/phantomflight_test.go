package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A phantom circles its target high up and swoops every eight to twelve
// seconds, rather than flying straight at it.
func TestPhantomCirclesThenSwoops(t *testing.T) {
	h := newHub(world.New(1))
	h.playersRef = map[int32]*tracked{}
	m := &mob{etype: entityPhantom, hasTarget: true, x: 0, y: 90, z: 0, tx: 10, tz: 0, ty: 70, flies: true}
	m.setMoveSpeed(0.2)

	// The first steer arms the anchor and circles: movement is mostly across
	// the line to the target, not along it.
	vx, vz := phantomFlightBehavior{}.steer(h, m)
	if m.phantomRadius < phantomCircleMin || m.phantomRadius > phantomCircleMin+phantomCircleSpan {
		t.Fatalf("the circle radius should be 5-15, got %v", m.phantomRadius)
	}
	if math.Abs(vz) <= math.Abs(vx) {
		t.Errorf("circling moves across the line to the target: vx=%v vz=%v", vx, vz)
	}
	// High above the target while it circles.
	y, ok := m.phantomAltitude()
	if !ok || y < m.ty+phantomAnchorLow {
		t.Fatalf("it should circle at least twenty above the target, got %v", y)
	}
	// A target deep below the sea still has the anchor at sea level + 1.
	low := *m
	low.ty = 10
	if y, _ := low.phantomAltitude(); y < float64(worldgen.SeaLevel+1) {
		t.Fatalf("the anchor fell below the sea: %v", y)
	}
	// The swoop comes, and then it dives straight in, at the target's level.
	for i := 0; i < 400 && m.phantomSwoop == 0; i++ {
		phantomFlightBehavior{}.steer(h, m)
	}
	if m.phantomSwoop == 0 {
		t.Fatal("a phantom should swoop within twenty seconds")
	}
	vx, vz = phantomFlightBehavior{}.steer(h, m)
	if vx <= 0 || math.Abs(vz) > math.Abs(vx) {
		t.Errorf("a swoop goes straight at the target: vx=%v vz=%v", vx, vz)
	}
	if y, _ := m.phantomAltitude(); y != m.ty {
		t.Errorf("a swooping phantom drops to the target's level, got %v", y)
	}
	// Without a target it just drifts.
	m.hasTarget = false
	phantomFlightBehavior{}.steer(h, m)
	if m.phantomSwoop != 0 {
		t.Error("losing the target ends the swoop")
	}
}

// PhantomAttackPlayerTargetGoal: of the players in its box (16 sideways, 64
// up and down) it takes the highest it can see, not the nearest, and only
// rescans every sixty ticks.
func TestPhantomPicksTheHighestPlayer(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	low, high := survPlayer(h), survPlayer(h)
	low.p.eid, high.p.eid = 500, 501
	low.x, low.y, low.z = 2.5, 150, 0.5
	high.x, high.y, high.z = 12.5, 170, 0.5
	players := map[int32]*tracked{low.p.eid: low, high.p.eid: high}
	h.playersRef = players
	m := h.spawnHostileY(players, entityPhantom, 0.5, 190, 0.5)
	h.acquireTarget(players, m)
	if !m.hasTarget || m.targetEID != high.p.eid {
		t.Fatalf("the phantom should take the higher player, got %d", m.targetEID)
	}
	delete(players, high.p.eid)
	h.acquireTarget(players, m)
	if m.hasTarget {
		t.Fatal("with its target gone it waits for the next scan")
	}
	for i := 0; i < 30 && !m.hasTarget; i++ {
		h.acquireTarget(players, m)
	}
	if m.targetEID != low.p.eid {
		t.Fatal("the next scan takes the other player")
	}
}
