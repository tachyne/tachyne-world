package server

import (
	"math"
	"strconv"
	"testing"
)

// /rotate through the dispatcher: an absolute rotation, then facing a
// point due east (yaw -90) and level; a non-operator is refused.
func TestRotateCommand(t *testing.T) {
	s, h, ps, logs, _ := eventServer(t, "")
	alice, carol := ps["alice"], ps["carol"]
	var bx, by, bz float64
	onHub(t, h, func() { b := h.playersRef[ps["bob"].eid]; bx, by, bz = b.x, b.y, b.z })

	s.handleCommand(alice, "rotate bob 45 10")
	settle(t, h, logs, "R1")
	var yaw, pitch float32
	onHub(t, h, func() { b := h.playersRef[ps["bob"].eid]; yaw, pitch = b.yaw, b.pitch })
	if yaw != 45 || pitch != 10 {
		t.Fatalf("bob faces %v/%v, want 45/10", yaw, pitch)
	}
	s.handleCommand(alice, "rotate bob facing "+ftoa(bx+10)+" "+ftoa(by)+" "+ftoa(bz))
	s.handleCommand(carol, "rotate bob 0 0")
	settle(t, h, logs, "R2")
	onHub(t, h, func() { b := h.playersRef[ps["bob"].eid]; yaw, pitch = b.yaw, b.pitch })
	if math.Abs(float64(yaw)+90) > 0.01 || math.Abs(float64(pitch)) > 0.01 {
		t.Fatalf("facing east: %v/%v, want -90/0", yaw, pitch)
	}
	if !hasLine(linesBetween(logs["alice"], "", "R2"), "Rotated bob") {
		t.Errorf("no success line for alice: %q", linesBetween(logs["alice"], "", "R2"))
	}
	if !hasLine(linesBetween(logs["carol"], "", "R2"), "You don't have permission.") {
		t.Errorf("carol was not refused: %q", linesBetween(logs["carol"], "", "R2"))
	}
}

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 3, 64) }
