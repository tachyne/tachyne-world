package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// clearBox opens a cube of air around a placement site.
func clearAirBox(w *world.World, x, y, z, r int) {
	for dx := -r; dx <= r; dx++ {
		for dy := -r; dy <= r; dy++ {
			for dz := -r; dz <= r; dz++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
}

func propOf(t *testing.T, state uint32, name string) string {
	t.Helper()
	info, ok := worldgen.InfoForState(state)
	if !ok {
		t.Fatalf("state %d has no property info", state)
	}
	return worldgen.GetProperty(info, state, name)
}

// Direction.orderedByNearest: the nearest axis leads, its opposite trails.
func TestLookOrderLeadsWithTheNearestFace(t *testing.T) {
	cases := []struct {
		yaw, pitch float32
		first      int32
		last       int32
	}{
		{0, 60, 0, 1},   // looking down-south: down first, up last
		{0, -70, 1, 0},  // looking up: up first
		{90, 0, 4, 5},   // yaw 90 = west
		{-90, 0, 5, 4},  // yaw -90 = east
		{180, 10, 2, 3}, // yaw 180 = north
		{10, 5, 3, 2},   // a little off south is still south
	}
	for _, c := range cases {
		o := lookOrder(c.yaw, c.pitch)
		if o[0] != c.first || o[5] != c.last {
			t.Errorf("lookOrder(%v,%v) = %v, want first %d last %d", c.yaw, c.pitch, o, c.first, c.last)
		}
	}
}

// A torch clicked onto a wall with no floor under the target goes on the
// wall, facing away from it; with a floor and the player looking down it
// stands; with nothing to hold it the placement is refused.
func TestTorchWallOrFloorByLook(t *testing.T) {
	w := world.New(1)
	x, y, z := 850, 180, 850
	clearAirBox(w, x, y, z, 3)
	w.SetBlock(x, y, z+1, worldgen.Stone) // wall south of the target
	torch, wallTorch := worldgen.BlockID("torch"), worldgen.BlockID("wall_torch")
	st, ok := standingOrWallState(w, blockPos{x, y, z}, torch, wallTorch, 0, 0)
	if !ok || !isSameBlock(st, wallTorch) || propOf(t, st, "facing") != "north" {
		t.Fatalf("no floor, wall to the south: want wall_torch facing north, got ok=%v state %d", ok, st)
	}
	// Looking down with the wall still there but a floor now too: the floor wins.
	w.SetBlock(x, y-1, z, worldgen.Stone)
	if st, ok := standingOrWallState(w, blockPos{x, y, z}, torch, wallTorch, 0, 70); !ok || st != torch {
		t.Fatalf("floor under the target and looking down: want a standing torch, got ok=%v state %d", ok, st)
	}
	// Looking level at the wall with a floor present: the wall comes first
	// in the look order, so it is a wall torch (vanilla: torches placed
	// while looking straight at a wall go on the wall).
	if st, ok := standingOrWallState(w, blockPos{x, y, z}, torch, wallTorch, 0, 0); !ok || !isSameBlock(st, wallTorch) {
		t.Fatalf("looking level at a wall: want wall_torch, got ok=%v state %d", ok, st)
	}
	w.SetBlock(x, y-1, z, worldgen.Air)
	w.SetBlock(x, y, z+1, worldgen.Air)
	if _, ok := standingOrWallState(w, blockPos{x, y, z}, torch, wallTorch, 0, 0); ok {
		t.Fatal("nothing holds a torch in mid-air")
	}
}

func isSameBlock(a, b uint32) bool { return sameBlockFamily(a, b) }

// A lantern under a ceiling hangs; on a floor it stands; the look order
// picks when both are there.
func TestLanternHangsFromCeiling(t *testing.T) {
	w := world.New(1)
	x, y, z := 860, 180, 860
	clearAirBox(w, x, y, z, 3)
	lantern := worldgen.BlockID("lantern")
	w.SetBlock(x, y+1, z, worldgen.Stone)                         // ceiling only
	st, ok := hangableState(w, blockPos{x, y, z}, lantern, 0, 60) // even looking down: the only hold is above
	if !ok || propOf(t, st, "hanging") != "true" {
		t.Fatalf("ceiling only: want hanging=true, got ok=%v state %d", ok, st)
	}
	w.SetBlock(x, y-1, z, worldgen.Stone) // floor too
	if st, ok := hangableState(w, blockPos{x, y, z}, lantern, 0, 60); !ok || propOf(t, st, "hanging") != "false" {
		t.Fatalf("floor and ceiling, looking down: want standing, got ok=%v state %d", ok, st)
	}
	if st, ok := hangableState(w, blockPos{x, y, z}, lantern, 0, -60); !ok || propOf(t, st, "hanging") != "true" {
		t.Fatalf("floor and ceiling, looking up: want hanging, got ok=%v state %d", ok, st)
	}
	if !supported(w, blockPos{x, y, z}, st) {
		t.Fatal("a hanging lantern under stone is supported")
	}
	w.SetBlock(x, y+1, z, worldgen.Air)
	if supported(w, blockPos{x, y, z}, st) {
		t.Fatal("a hanging lantern with its ceiling gone is not supported")
	}
}

// Cocoa faces the jungle log it grows on; without one it is refused.
func TestCocoaFacesItsLog(t *testing.T) {
	w := world.New(1)
	x, y, z := 870, 180, 870
	clearAirBox(w, x, y, z, 3)
	cocoa := worldgen.BlockID("cocoa")
	if _, ok := cocoaState(w, blockPos{x, y, z}, cocoa, 0, 0); ok {
		t.Fatal("cocoa with no jungle log should be refused")
	}
	w.SetBlock(x-1, y, z, worldgen.BlockID("jungle_log"))   // log to the west
	st, ok := cocoaState(w, blockPos{x, y, z}, cocoa, 0, 0) // looking south: the order still reaches west
	if !ok || propOf(t, st, "facing") != "west" {
		t.Fatalf("want cocoa facing west toward its log, got ok=%v state %d", ok, st)
	}
	w.SetBlock(x-1, y, z, worldgen.Stone)
	if supported(w, blockPos{x, y, z}, st) {
		t.Fatal("cocoa on stone is not supported")
	}
}

// Through the real placement path: a redstone item lays dust, cocoa beans
// lay a pod, a torch clicked onto a wall becomes a wall torch.
func TestHandlePlaceAliasesAndWallTorch(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 880, 180, 880
	clearAirBox(w, x, y, z, 3)
	w.SetBlock(x, y-1, z, worldgen.Stone)

	p.setHotbarSlot(0, itemByName["redstone"])
	p.held = 0
	s.handlePlace(p, placeBody(x, y-1, z, 1)) // click the floor's top
	if got := w.Block(x, y, z); !isWire(got) || propOf(t, got, "power") != "0" {
		t.Fatalf("redstone item should lay unpowered dust, got %d", got)
	}
	w.SetBlock(x, y, z, worldgen.Air)

	p.setHotbarSlot(0, itemByName["torch"])
	p.yaw, p.pitch = 0, 0 // looking south at a wall
	w.SetBlock(x, y-1, z, worldgen.Air)
	w.SetBlock(x, y, z+1, worldgen.Stone)
	s.handlePlace(p, placeBody(x, y, z+1, 2)) // click the wall's north face
	if got := w.Block(x, y, z); !isSameBlock(got, worldgen.BlockID("wall_torch")) || propOf(t, got, "facing") != "north" {
		t.Fatalf("torch on a wall should be a wall_torch facing north, got %d", got)
	}
	w.SetBlock(x, y, z, worldgen.Air)

	p.setHotbarSlot(0, itemByName["cocoa_beans"])
	w.SetBlock(x, y, z+1, worldgen.BlockID("jungle_log"))
	s.handlePlace(p, placeBody(x, y, z+1, 2))
	if got := w.Block(x, y, z); !isCocoa(got) || propOf(t, got, "facing") != "south" {
		t.Fatalf("cocoa beans on a jungle log should place cocoa facing south, got %d", got)
	}
}

// A second vine placed onto a vine joins it with a new held face rather
// than replacing it, and never adds a face nothing holds.
func TestVineJoinsExistingVine(t *testing.T) {
	w := world.New(1)
	x, y, z := 890, 180, 890
	clearAirBox(w, x, y, z, 3)
	def := worldgen.BlockID("vine")
	w.SetBlock(x, y, z+1, worldgen.Stone) // south wall
	w.SetBlock(x+1, y, z, worldgen.Stone) // east wall
	first, ok := multifacePlacement(w, blockPos{x, y, z}, def, worldgen.Air, 0, 0)
	if !ok || propOf(t, first, "south") != "true" || propOf(t, first, "east") != "false" {
		t.Fatalf("first vine should hold south only, got ok=%v %d", ok, first)
	}
	second, ok := multifacePlacement(w, blockPos{x, y, z}, def, first, -90, 0) // looking east
	if !ok || propOf(t, second, "south") != "true" || propOf(t, second, "east") != "true" {
		t.Fatalf("second vine should join with east, got ok=%v %d", ok, second)
	}
	// Looking north where nothing holds: no face is added, so no placement.
	if _, ok := multifacePlacement(w, blockPos{x, y, z}, def, second, 180, 0); ok {
		t.Fatal("no holdable face left in reach: placement should be refused")
	}
}
