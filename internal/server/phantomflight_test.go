package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
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
		t.Fatalf("it should circle at least ten above the target, got %v", y)
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
