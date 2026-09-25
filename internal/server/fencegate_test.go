package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestFenceGateSwingsAwayFromThePlayer: FenceGateBlock.useWithoutItem turns
// a closed gate facing the player to face away from them before it opens,
// so it always swings away.
func TestFenceGateSwingsAwayFromThePlayer(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 3, 70, 3
	gate := worldgen.BlockBase("oak_fence_gate")
	info, _ := worldgen.InfoForState(gate)
	gate = worldgen.SetProperty(info, gate, "facing", "north")
	gate = setBoolProp(setBoolProp(gate, "open", false), "powered", false)
	w.SetBlock(x, y, z, gate)
	p.setHotbarSlot(0, 0)
	p.held = 0
	p.yaw = 0 // looking south, at the gate's back
	s.tryUseBlock(p, false, x, y, z, 1, 2, 0.5, 0.5, 0.5)
	got := w.Block(x, y, z)
	if !boolProp(got, "open") {
		t.Fatal("the gate did not open")
	}
	if f := worldgen.GetProperty(info, got, "facing"); f != "south" {
		t.Fatalf("a gate opened from behind should turn to face south, got %s", f)
	}
}
