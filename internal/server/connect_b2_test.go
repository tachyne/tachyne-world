package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// FenceBlock.connectsTo: a fence joins a gate hung across it, not a pane or
// leaves, and wooden fences do not join the nether-brick fence.
func TestFenceConnections(t *testing.T) {
	fence := worldgen.BlockID("oak_fence")
	gate := worldgen.BlockID("oak_fence_gate")
	gi, _ := worldgen.InfoForState(gate)
	across := worldgen.SetProperty(gi, gate, "facing", "north") // spans east-west
	if !connectsTo(fence, across, true) {
		t.Error("a fence should join a gate beside it on the gate's line")
	}
	if connectsTo(fence, across, false) {
		t.Error("a fence should not join the end of a gate")
	}
	if connectsTo(fence, worldgen.BlockID("glass_pane"), true) {
		t.Error("a fence joined a glass pane")
	}
	if connectsTo(fence, worldgen.BlockID("oak_leaves"), true) {
		t.Error("a fence joined leaves")
	}
	if connectsTo(fence, worldgen.BlockID("nether_brick_fence"), true) {
		t.Error("an oak fence joined a nether-brick fence")
	}
	if !connectsTo(fence, worldgen.BlockID("spruce_fence"), true) || !connectsTo(fence, worldgen.Stone, true) {
		t.Error("a fence should join another wooden fence and stone")
	}
	if !connectsTo(worldgen.BlockID("glass_pane"), worldgen.BlockID("copper_bars"), true) {
		t.Error("a pane should join copper bars")
	}
}
