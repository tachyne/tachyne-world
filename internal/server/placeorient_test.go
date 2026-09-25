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
	st, ok := standingOrWallState(w, blockPos{x, y, z}, torch, wallTorch, lookOrder(0, 0))
	if !ok || !isSameBlock(st, wallTorch) || propOf(t, st, "facing") != "north" {
		t.Fatalf("no floor, wall to the south: want wall_torch facing north, got ok=%v state %d", ok, st)
	}
	// Looking down with the wall still there but a floor now too: the floor wins.
	w.SetBlock(x, y-1, z, worldgen.Stone)
	if st, ok := standingOrWallState(w, blockPos{x, y, z}, torch, wallTorch, lookOrder(0, 70)); !ok || st != torch {
		t.Fatalf("floor under the target and looking down: want a standing torch, got ok=%v state %d", ok, st)
	}
	// Looking level at the wall with a floor present: the wall comes first
	// in the look order, so it is a wall torch (vanilla: torches placed
	// while looking straight at a wall go on the wall).
	if st, ok := standingOrWallState(w, blockPos{x, y, z}, torch, wallTorch, lookOrder(0, 0)); !ok || !isSameBlock(st, wallTorch) {
		t.Fatalf("looking level at a wall: want wall_torch, got ok=%v state %d", ok, st)
	}
	w.SetBlock(x, y-1, z, worldgen.Air)
	w.SetBlock(x, y, z+1, worldgen.Air)
	if _, ok := standingOrWallState(w, blockPos{x, y, z}, torch, wallTorch, lookOrder(0, 0)); ok {
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
	w.SetBlock(x, y+1, z, worldgen.Stone)                                    // ceiling only
	st, ok := hangableState(w, blockPos{x, y, z}, lantern, lookOrder(0, 60)) // even looking down: the only hold is above
	if !ok || propOf(t, st, "hanging") != "true" {
		t.Fatalf("ceiling only: want hanging=true, got ok=%v state %d", ok, st)
	}
	w.SetBlock(x, y-1, z, worldgen.Stone) // floor too
	if st, ok := hangableState(w, blockPos{x, y, z}, lantern, lookOrder(0, 60)); !ok || propOf(t, st, "hanging") != "false" {
		t.Fatalf("floor and ceiling, looking down: want standing, got ok=%v state %d", ok, st)
	}
	if st, ok := hangableState(w, blockPos{x, y, z}, lantern, lookOrder(0, -60)); !ok || propOf(t, st, "hanging") != "true" {
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
	if _, ok := cocoaState(w, blockPos{x, y, z}, cocoa, lookOrder(0, 0)); ok {
		t.Fatal("cocoa with no jungle log should be refused")
	}
	w.SetBlock(x-1, y, z, worldgen.BlockID("jungle_log"))              // log to the west
	st, ok := cocoaState(w, blockPos{x, y, z}, cocoa, lookOrder(0, 0)) // looking south: the order still reaches west
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
	selectSlot(p, 0)
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
	first, ok := multifacePlacement(w, blockPos{x, y, z}, def, worldgen.Air, lookOrder(0, 0))
	if !ok || propOf(t, first, "south") != "true" || propOf(t, first, "east") != "false" {
		t.Fatalf("first vine should hold south only, got ok=%v %d", ok, first)
	}
	second, ok := multifacePlacement(w, blockPos{x, y, z}, def, first, lookOrder(-90, 0)) // looking east
	if !ok || propOf(t, second, "south") != "true" || propOf(t, second, "east") != "true" {
		t.Fatalf("second vine should join with east, got ok=%v %d", ok, second)
	}
	// Looking north where nothing holds: no face is added, so no placement.
	if _, ok := multifacePlacement(w, blockPos{x, y, z}, def, second, lookOrder(180, 0)); ok {
		t.Fatal("no holdable face left in reach: placement should be refused")
	}
}

// getNearestLookingDirections: the clicked face wins. Legion hit this in
// game — highlighting a dirt wall and placing a torch put it somewhere else,
// and a lever would only take a wall if she stood square to it.
func TestClickedFaceComesFirst(t *testing.T) {
	// Looking mostly downward (pitch 60) but clicking the SOUTH face of a
	// block: the order must start with north (the opposite of south), so a
	// torch takes that wall rather than the floor.
	order := placeOrder(3 /*south*/, false, 0, 60)
	if order[0] != 2 /*north*/ {
		t.Fatalf("the clicked face's opposite comes first, got %v", order)
	}
	// Every direction still appears exactly once.
	seen := map[int32]bool{}
	for _, d := range order {
		if seen[d] {
			t.Fatalf("duplicate direction in %v", order)
		}
		seen[d] = true
	}
	if len(seen) != 6 {
		t.Fatalf("all six directions must survive: %v", order)
	}
	// Clicking the floor (up face) puts down first.
	if o := placeOrder(1, false, 0, -60); o[0] != 0 {
		t.Errorf("clicking a top face should start with down, got %v", o)
	}
	// A click that REPLACED the block it landed on keeps the plain look order.
	if o, plain := placeOrder(3, true, 0, 60), lookOrder(0, 60); o != plain {
		t.Errorf("a replacing click uses the look order: %v vs %v", o, plain)
	}
}

// A torch clicked onto a wall takes that wall even while the player looks
// down at the floor.
func TestTorchTakesTheClickedWall(t *testing.T) {
	w := world.New(1)
	x, y, z := 40, 70, 40
	torch := worldgen.BlockBase("torch")
	wallTorch := worldgen.BlockBase("wall_torch")
	for dy := -1; dy <= 2; dy++ {
		w.SetBlock(x, y+dy, z, worldgen.Air)
	}
	w.SetBlock(x, y-1, z, worldgen.Stone) // a floor, which would win on look alone
	w.SetBlock(x, y, z+1, worldgen.Stone) // and a wall to the south
	st, ok := standingOrWallState(w, blockPos{x, y, z}, torch, wallTorch,
		placeOrder(2 /*clicked the north face of that wall*/, false, 0, 60))
	if !ok || !isSameBlock(st, wallTorch) {
		t.Fatalf("the torch should take the clicked wall, got %d (ok=%v)", st, ok)
	}
	// Clicking the floor instead gives the standing torch.
	st, ok = standingOrWallState(w, blockPos{x, y, z}, torch, wallTorch, placeOrder(1, false, 0, 60))
	if !ok || st != torch {
		t.Fatalf("clicking the floor gives a standing torch, got %d (ok=%v)", st, ok)
	}
}

// A lever clicked onto a wall sits on that wall even when the player is not
// square to it — Legion's "I need to be facing a surface perpendicularly
// before a lever will sit on a wall otherwise it defaults to the floor".
func TestLeverTakesTheClickedWall(t *testing.T) {
	w := world.New(1)
	x, y, z := 44, 70, 44
	clearAirBox(w, x, y, z, 2)
	w.SetBlock(x, y-1, z, worldgen.Stone) // a floor, which wins on look alone
	w.SetBlock(x, y, z+1, worldgen.Stone) // and a wall to the south
	lever := worldgen.BlockBase("lever")
	// Looking steeply down and off to the side, but the highlighted face is
	// the wall's north face.
	st, ok := faceAttachedState(w, blockPos{x, y, z}, lever, placeOrder(2, false, 35, 55), 35)
	if !ok {
		t.Fatal("the lever should have found the clicked wall")
	}
	if got := propOf(t, st, "face"); got != "wall" {
		t.Fatalf("face = %q, want wall", got)
	}
	if got := propOf(t, st, "facing"); got != "north" {
		t.Fatalf("facing = %q, want north (away from the wall)", got)
	}
	// Clicking the floor still gives a floor lever facing the player's way.
	st, ok = faceAttachedState(w, blockPos{x, y, z}, lever, placeOrder(1, false, 35, 55), 35)
	if !ok || propOf(t, st, "face") != "floor" {
		t.Fatalf("clicking the floor gives a floor lever, got %d (ok=%v)", st, ok)
	}
}

// Glow lichen goes on the face you highlighted, not on whatever else is in
// reach. Legion hit this where a floor meets a wall: clicking the floor put
// the lichen up the wall instead, a "corner" placement with the patch
// standing out into the air.
func TestGlowLichenTakesTheClickedFace(t *testing.T) {
	w := world.New(1)
	x, y, z := 48, 70, 48
	clearAirBox(w, x, y, z, 2)
	w.SetBlock(x, y-1, z, worldgen.Stone) // the floor she clicked
	w.SetBlock(x, y, z+1, worldgen.Stone) // a wall alongside it
	lichen := worldgen.BlockBase("glow_lichen")
	info, _ := worldgen.InfoForState(lichen)

	// Looking across the floor at a shallow angle, but the highlighted face
	// is the floor's top.
	st, ok := multifacePlacement(w, blockPos{x, y, z}, lichen, 0, placeOrder(1 /*up*/, false, 0, 20))
	if !ok {
		t.Fatal("the lichen should have found the floor")
	}
	if worldgen.GetProperty(info, st, "down") != "true" {
		t.Fatalf("the lichen should lie on the clicked floor, got %d", st)
	}
	if worldgen.GetProperty(info, st, "north") == "true" || worldgen.GetProperty(info, st, "south") == "true" {
		t.Fatalf("no wall face should be set by a click on the floor, got %d", st)
	}
	// And clicking the wall's north face takes the wall, not the floor.
	st, ok = multifacePlacement(w, blockPos{x, y, z}, lichen, 0, placeOrder(2 /*north*/, false, 0, 20))
	if !ok || worldgen.GetProperty(info, st, "south") != "true" {
		t.Fatalf("clicking the wall should set the south face, got %d (ok=%v)", st, ok)
	}
	if worldgen.GetProperty(info, st, "down") == "true" {
		t.Fatalf("a wall click must not lay it on the floor as well, got %d", st)
	}
	// A face with nothing behind it is never chosen: no support at all means
	// no placement, never a patch hanging in the air.
	w.SetBlock(x, y-1, z, worldgen.Air)
	w.SetBlock(x, y, z+1, worldgen.Air)
	if _, ok := multifacePlacement(w, blockPos{x, y, z}, lichen, 0, placeOrder(1, false, 0, 20)); ok {
		t.Fatal("with nothing to hold it the placement must be refused")
	}
}

// Two lichens side by side on a flat floor each stay flat. The fence/pane
// connector matched anything with boolean north/east/south/west properties,
// which is also how a multiface block names its FACES — so placing one beside
// another "connected" them, and each grew a vertical face with nothing behind
// it. Legion reported it as lichen sprouting outcroppings when two are placed
// adjacent.
func TestAdjacentLichenStayFlat(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 904, 180, 904
	for dx := -3; dx <= 3; dx++ {
		for dy := -2; dy <= 3; dy++ {
			for dz := -3; dz <= 3; dz++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	for dx := -3; dx <= 3; dx++ {
		for dz := -3; dz <= 3; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
		}
	}
	p.setHotbarSlot(0, itemByName["glow_lichen"])
	p.held, p.yaw, p.pitch = 0, 0, 45

	s.handlePlace(p, placeBody(x, y-1, z, 1))   // click the floor
	s.handlePlace(p, placeBody(x, y-1, z+1, 1)) // and the floor beside it

	for _, c := range []blockPos{{x, y, z}, {x, y, z + 1}} {
		got := w.Block(c.x, c.y, c.z)
		if !isMultiface(got) {
			t.Fatalf("no lichen at %v, got %d", c, got)
		}
		info, _ := worldgen.InfoForState(got)
		if worldgen.GetProperty(info, got, "down") != "true" {
			t.Errorf("the lichen at %v should lie on the floor", c)
		}
		for _, f := range []string{"up", "north", "south", "east", "west"} {
			if worldgen.GetProperty(info, got, f) == "true" {
				t.Errorf("the lichen at %v grew a %s face with nothing behind it", c, f)
			}
		}
	}
}

// A shulker box faces the face it was placed against, up and down included
// (ShulkerBoxBlock.getStateForPlacement), whatever the look.
func TestShulkerBoxFacesTheClickedFace(t *testing.T) {
	box := worldgen.BlockBase("shulker_box")
	info, _ := worldgen.InfoForState(box)
	for dir, want := range map[int32]string{0: "down", 1: "up", 2: "north", 5: "east"} {
		got := worldgen.GetProperty(info, orientState(box, dir, 0.5, 0, 0, worldgen.Stone), "facing")
		if got != want {
			t.Errorf("clicked face %d: facing %s, want %s", dir, got, want)
		}
	}
}
