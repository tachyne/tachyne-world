package server

import (
	"testing"

	"github.com/tachyne/tachyne-common/protocol"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

const stone = 1

func wallDefault(t *testing.T) (uint32, worldgen.BlockInfo) {
	t.Helper()
	st, ok := protocol.BlockForItem(itemByName["red_nether_brick_wall"])
	if !ok {
		t.Fatal("no red_nether_brick_wall item mapping")
	}
	info, ok := wallInfo(st)
	if !ok {
		t.Fatal("red_nether_brick_wall not classified as a wall")
	}
	return st, info
}

func sideOf(t *testing.T, info worldgen.BlockInfo, st uint32, name string) string {
	t.Helper()
	return worldgen.GetProperty(info, st, name)
}

// TestWallConnections drives the vanilla WallBlock rules: side connection to
// solids/walls/gates, LOW vs TALL under cover, and the post logic (present
// on ends/corners, absent on straight runs).
func TestWallConnections(t *testing.T) {
	def, info := wallDefault(t)
	w := world.New(1)

	// lone wall: the default monopole (post up, no sides)
	if st := wallState(w, 0, 200, 0, info, def); st != def {
		t.Fatalf("lone wall changed: %d -> %d", def, st)
	}

	// straight run between two stones: east+west LOW, post GONE
	w.SetBlock(1, 200, 0, stone)
	w.SetBlock(-1, 200, 0, stone)
	st := wallState(w, 0, 200, 0, info, def)
	if sideOf(t, info, st, "east") != "low" || sideOf(t, info, st, "west") != "low" {
		t.Fatalf("straight run sides: e=%s w=%s", sideOf(t, info, st, "east"), sideOf(t, info, st, "west"))
	}
	if sideOf(t, info, st, "north") != "none" || sideOf(t, info, st, "up") != "false" {
		t.Fatalf("straight run: north=%s up=%s", sideOf(t, info, st, "north"), sideOf(t, info, st, "up"))
	}

	// corner (stone north + east): both LOW, post PRESENT
	w.SetBlock(-1, 200, 0, worldgen.Air)
	w.SetBlock(0, 200, -1, stone)
	st = wallState(w, 0, 200, 0, info, def)
	if sideOf(t, info, st, "north") != "low" || sideOf(t, info, st, "east") != "low" || sideOf(t, info, st, "up") != "true" {
		t.Fatalf("corner: n=%s e=%s up=%s", sideOf(t, info, st, "north"), sideOf(t, info, st, "east"), sideOf(t, info, st, "up"))
	}

	// solid cover above a straight run: sides TALL, and vanilla's
	// shouldRaisePost drops the post unconditionally on a straight tall run
	// (hasHighWall → false, the cover test is never reached)
	w.SetBlock(0, 200, -1, worldgen.Air)
	w.SetBlock(-1, 200, 0, stone)
	w.SetBlock(0, 201, 0, stone)
	st = wallState(w, 0, 200, 0, info, def)
	if sideOf(t, info, st, "east") != "tall" || sideOf(t, info, st, "west") != "tall" {
		t.Fatalf("covered run sides: e=%s w=%s", sideOf(t, info, st, "east"), sideOf(t, info, st, "west"))
	}
	if sideOf(t, info, st, "up") != "false" {
		t.Fatal("straight tall run must stay flush even under cover")
	}
	// a lone covered post (no connections at all) keeps its post via the
	// corner rule
	w.SetBlock(1, 200, 0, worldgen.Air)
	w.SetBlock(-1, 200, 0, worldgen.Air)
	st = wallState(w, 0, 200, 0, info, def)
	if sideOf(t, info, st, "up") != "true" {
		t.Fatal("lone covered wall lost its post")
	}
	w.SetBlock(0, 201, 0, worldgen.Air)
	w.SetBlock(1, 200, 0, stone)
	w.SetBlock(-1, 200, 0, stone)

	// a wall above with a connected side makes the side below TALL, and a
	// straight tall run drops the post (vanilla hasHighWall)
	upper := worldgen.SetProperty(info, def, "east", "low")
	upper = worldgen.SetProperty(info, upper, "west", "low")
	upper = worldgen.SetProperty(info, upper, "up", "false")
	w.SetBlock(0, 201, 0, upper)
	st = wallState(w, 0, 200, 0, info, def)
	if sideOf(t, info, st, "east") != "tall" || sideOf(t, info, st, "west") != "tall" {
		t.Fatalf("under-wall sides: e=%s w=%s", sideOf(t, info, st, "east"), sideOf(t, info, st, "west"))
	}
	if sideOf(t, info, st, "up") != "false" {
		t.Fatal("straight tall run should drop the post")
	}
	w.SetBlock(0, 201, 0, worldgen.Air)

	// fence gates: connect when the gate's facing crosses the wall line
	gate, ok := protocol.BlockForItem(itemByName["oak_fence_gate"])
	if !ok {
		t.Fatal("no oak_fence_gate mapping")
	}
	ginfo, ok := gateInfo(gate)
	if !ok {
		t.Fatal("gate not classified")
	}
	w.SetBlock(-1, 200, 0, worldgen.Air)
	w.SetBlock(1, 200, 0, worldgen.SetProperty(ginfo, gate, "facing", "north")) // crosses an east-west wall line
	st = wallState(w, 0, 200, 0, info, def)
	if sideOf(t, info, st, "east") != "low" {
		t.Fatalf("aligned gate should connect: east=%s", sideOf(t, info, st, "east"))
	}
	w.SetBlock(1, 200, 0, worldgen.SetProperty(ginfo, gate, "facing", "east")) // runs along the wall line
	st = wallState(w, 0, 200, 0, info, def)
	if sideOf(t, info, st, "east") != "none" {
		t.Fatalf("cross gate should not connect: east=%s", sideOf(t, info, st, "east"))
	}
}

// TestWallFencePaneAttachment: panes/bars attach to walls, fences do not,
// and walls connect to both.
func TestWallFencePaneAttachment(t *testing.T) {
	def, _ := wallDefault(t)
	pane, _ := protocol.BlockForItem(itemByName["glass_pane"])
	fence, _ := protocol.BlockForItem(itemByName["oak_fence"])
	if !connectsTo(pane, def) {
		t.Fatal("glass pane should attach to a wall")
	}
	if connectsTo(fence, def) {
		t.Fatal("fence should NOT attach to a wall")
	}
	if !wallConnectsTo(pane, true) {
		t.Fatal("wall should connect to panes")
	}
	if wallConnectsTo(fence, true) {
		t.Fatal("wall should NOT connect to a fence")
	}
}

// TestWallColumnCascade: refreshing the top of a wall stack ripples the
// tall/post changes down the column.
func TestWallColumnCascade(t *testing.T) {
	def, info := wallDefault(t)
	w := world.New(1)
	s := &Server{world: w, hub: newHub(w)}
	// two stacked walls with stone beside both levels
	w.SetBlock(0, 200, 0, def)
	w.SetBlock(0, 201, 0, def)
	w.SetBlock(1, 200, 0, stone)
	w.SetBlock(1, 201, 0, stone)
	w.SetBlock(-1, 200, 0, stone)
	w.SetBlock(-1, 201, 0, stone)
	s.refreshWallColumn(w, 0, 0, 201, 0)
	top, bottom := w.Block(0, 201, 0), w.Block(0, 200, 0)
	if sideOf(t, info, top, "east") != "low" || sideOf(t, info, top, "up") != "false" {
		t.Fatalf("top wall: e=%s up=%s", sideOf(t, info, top, "east"), sideOf(t, info, top, "up"))
	}
	if sideOf(t, info, bottom, "east") != "tall" || sideOf(t, info, bottom, "west") != "tall" {
		t.Fatalf("bottom wall sides should be tall: e=%s w=%s", sideOf(t, info, bottom, "east"), sideOf(t, info, bottom, "west"))
	}
	if sideOf(t, info, bottom, "up") != "false" {
		t.Fatal("bottom straight tall run should drop the post")
	}
}
