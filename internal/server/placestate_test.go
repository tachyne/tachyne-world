package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// holdItem puts one named item in the player's selected hotbar slot.
func holdItem(t *testing.T, p *player, name string) {
	t.Helper()
	id, ok := itemByName[name]
	if !ok {
		t.Fatalf("no item %q", name)
	}
	p.setHotbarSlot(0, int32(id))
	p.held = 0
}

// CrafterBlock.getStateForPlacement: the front faces the player along the
// nearest looking direction; a vertical front takes its top from the yaw.
func TestCrafterPlacedOrientationFollowsLook(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1200, 180, 1200
	clearAirBox(w, x, y, z, 2)
	w.SetBlock(x, y-1, z, worldgen.Stone)
	holdItem(t, p, "crafter")
	for _, c := range []struct {
		yaw, pitch float32
		want       string
	}{
		{180, 0, "south_up"},     // looking north: the front faces back south
		{180, 80, "up_north"},    // looking down: front up, top the way the player faces
		{180, -80, "down_south"}, // looking up: front down, top back toward the player
		{270, 0, "west_up"},      // looking east
	} {
		w.SetBlock(x, y, z, worldgen.Air)
		p.yaw, p.pitch = c.yaw, c.pitch
		s.handlePlace(p, placeBody(x, y-1, z, 1))
		got := w.Block(x, y, z)
		if !isCrafter(got) {
			t.Fatalf("yaw %v pitch %v: no crafter placed (state %d)", c.yaw, c.pitch, got)
		}
		if o := propOf(t, got, "orientation"); o != c.want {
			t.Errorf("yaw %v pitch %v: orientation %s, want %s", c.yaw, c.pitch, o, c.want)
		}
	}
}

// BambooStalkBlock.getStateForPlacement: the bamboo item plants a sapling on
// soil, grows a stalk on a sapling or stalk (keeping its thickness), and
// never goes into water.
func TestBambooItemPlacesSaplingOrStalk(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1200, 180, 1200
	clearAirBox(w, x, y, z, 3)
	holdItem(t, p, "bamboo")

	w.SetBlock(x, y-1, z, worldgen.GrassBlock)
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	if got := w.Block(x, y, z); got != bambooSapling {
		t.Fatalf("bamboo on grass placed %d, want the sapling %d", got, bambooSapling)
	}

	s.handlePlace(p, placeBody(x, y, z, 1)) // onto the sapling
	if got := w.Block(x, y+1, z); got != bambooState(0, bambooLeavesNone, 0) {
		t.Fatalf("bamboo on a sapling placed %d, want an age-0 stalk", got)
	}

	w.SetBlock(x+1, y-1, z, worldgen.GrassBlock)
	w.SetBlock(x+1, y, z, bambooState(1, bambooLeavesLarge, 0))
	s.handlePlace(p, placeBody(x+1, y, z, 1)) // onto a thick stalk
	if got := w.Block(x+1, y+1, z); got != bambooState(1, bambooLeavesNone, 0) {
		t.Fatalf("bamboo on a thick stalk placed %d, want an age-1 stalk", got)
	}

	w.SetBlock(x-1, y-1, z, worldgen.GrassBlock)
	w.SetBlock(x-1, y, z, worldgen.WaterBase)
	s.handlePlace(p, placeBody(x-1, y-1, z, 1)) // into a water source
	if got := w.Block(x-1, y, z); isBamboo(got) || got == bambooSapling {
		t.Fatalf("bamboo went into water (state %d)", got)
	}
}

// DirtPathBlock.getStateForPlacement: a path under a solid block goes down as
// dirt.
func TestDirtPathPlacedUnderSolidBecomesDirt(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1200, 180, 1200
	clearAirBox(w, x, y, z, 2)
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x+1, y-1, z, worldgen.Stone)
	w.SetBlock(x, y+1, z, worldgen.Stone)
	holdItem(t, p, "dirt_path")

	s.handlePlace(p, placeBody(x, y-1, z, 1))
	if got := w.Block(x, y, z); got != worldgen.Dirt {
		t.Fatalf("a path placed under stone is %d, want dirt", got)
	}
	s.handlePlace(p, placeBody(x+1, y-1, z, 1))
	if got := w.Block(x+1, y, z); got != dirtPathState {
		t.Fatalf("a path placed in the open is %d, want dirt_path", got)
	}
}

// KelpBlock.getStateForPlacement: only into a full water source.
// GrowingPlantHeadBlock: a head with an age below 25, and the head it is set
// on turns to body.
func TestKelpPlacementNeedsWaterSourceAndExtends(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1200, 180, 1200
	clearAirBox(w, x, y, z, 3)
	holdItem(t, p, "kelp")
	kelp, _ := growingPlantOf(worldgen.BlockBase("kelp"))

	w.SetBlock(x+2, y-1, z, worldgen.Sand) // in air
	s.handlePlace(p, placeBody(x+2, y-1, z, 1))
	if got := w.Block(x+2, y, z); got != worldgen.Air {
		t.Fatalf("kelp placed in air (state %d)", got)
	}
	w.SetBlock(x-2, y-1, z, worldgen.Sand) // in flowing water
	w.SetBlock(x-2, y, z, worldgen.WaterBase+2)
	s.handlePlace(p, placeBody(x-2, y-1, z, 1))
	if got := w.Block(x-2, y, z); got >= kelp.headLo && got <= kelp.headHi {
		t.Fatalf("kelp placed in flowing water (state %d)", got)
	}

	w.SetBlock(x, y-1, z, worldgen.Sand)
	w.SetBlock(x, y, z, worldgen.WaterBase)
	w.SetBlock(x, y+1, z, worldgen.WaterBase)
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	head := w.Block(x, y, z)
	if head < kelp.headLo || head > kelp.headHi || kelp.age(head) >= growingPlantMaxAge {
		t.Fatalf("kelp in a water source placed %d, want a head aged 0-24", head)
	}
	s.handlePlace(p, placeBody(x, y, z, 1)) // onto the head
	if top := w.Block(x, y+1, z); top < kelp.headLo || top > kelp.headHi {
		t.Fatalf("kelp set on kelp placed %d, want a head", top)
	}
	if got := w.Block(x, y, z); got != kelp.body {
		t.Fatalf("the kelp head under a new one is %d, want kelp_plant %d", got, kelp.body)
	}
}

// GrowingPlantBlock.getStateForPlacement for a hanging vine: placed into a
// cell with more vine below it, it is body; placed under a head, the head
// becomes body. Glow berries place cave vines.
func TestVinePlacementHeadAndBody(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1200, 180, 1200
	clearAirBox(w, x, y, z, 3)
	weeping, _ := growingPlantOf(worldgen.BlockBase("weeping_vines"))
	w.SetBlock(x, y+1, z, worldgen.Stone)
	holdItem(t, p, "weeping_vines")
	s.handlePlace(p, placeBody(x, y+1, z, 0))
	if got := w.Block(x, y, z); got < weeping.headLo || got > weeping.headHi {
		t.Fatalf("weeping vines under stone placed %d, want a head", got)
	}
	s.handlePlace(p, placeBody(x, y, z, 0)) // under the head
	if got := w.Block(x, y, z); got != weeping.body {
		t.Fatalf("the head a vine was hung from is %d, want weeping_vines_plant", got)
	}

	// Between the ceiling and a vine hanging below: the gap fills with body.
	w.SetBlock(x+1, y+1, z, worldgen.Stone)
	w.SetBlock(x+1, y-1, z, weeping.headAt(3, false))
	s.handlePlace(p, placeBody(x+1, y+1, z, 0))
	if got := w.Block(x+1, y, z); got != weeping.body {
		t.Fatalf("a vine placed above more vine is %d, want body", got)
	}

	caves, _ := growingPlantOf(worldgen.BlockBase("cave_vines"))
	w.SetBlock(x-1, y+1, z, worldgen.Stone)
	holdItem(t, p, "glow_berries")
	s.handlePlace(p, placeBody(x-1, y+1, z, 0))
	got := w.Block(x-1, y, z)
	if got < caves.headLo || got > caves.headHi || propOf(t, got, "berries") != "false" {
		t.Fatalf("glow berries under stone placed %d, want a berry-less cave vine head", got)
	}
}

// HugeMushroomBlock.getStateForPlacement: faces against the same block lose
// their skin; the rest keep it.
func TestHugeMushroomPlacedFacesFromNeighbours(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1200, 180, 1200
	clearAirBox(w, x, y, z, 2)
	red := withProps(t, worldgen.BlockBase("red_mushroom_block"), map[string]string{
		"up": "true", "down": "true", "north": "true", "south": "true", "east": "true", "west": "true"})
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x+1, y, z, red)
	w.SetBlock(x, y+1, z, worldgen.BlockBase("mushroom_stem"))
	holdItem(t, p, "red_mushroom_block")
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	got := w.Block(x, y, z)
	want := map[string]string{"east": "false", "west": "true", "north": "true", "south": "true", "up": "true", "down": "true"}
	for k, v := range want {
		if g := propOf(t, got, k); g != v {
			t.Errorf("%s = %s, want %s", k, g, v)
		}
	}
}

// MossyCarpetBlock.getStateForPlacement: a side climbs a wall that can hold
// it; setPlacedBy may add a layer above, which turns that side tall.
func TestPaleMossCarpetPlacedSidesClimbWalls(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1200, 180, 1200
	holdItem(t, p, "pale_moss_carpet")
	for i := 0; i < 8; i++ { // the layer above is a coin flip; both outcomes are checked
		cx := x + i*3
		clearAirBox(w, cx, y, z, 1)
		w.SetBlock(cx, y-1, z, worldgen.Stone)
		w.SetBlock(cx, y, z-1, worldgen.Stone)
		w.SetBlock(cx, y+1, z-1, worldgen.Stone)
		s.handlePlace(p, placeBody(cx, y-1, z, 1))
		base := w.Block(cx, y, z)
		if !isPaleMossCarpet(base) || propOf(t, base, "bottom") != "true" {
			t.Fatalf("no base carpet placed (state %d)", base)
		}
		for _, side := range []string{"east", "south", "west"} {
			if g := propOf(t, base, side); g != "none" {
				t.Errorf("open side %s = %s, want none", side, g)
			}
		}
		top := w.Block(cx, y+1, z)
		switch {
		case isPaleMossCarpet(top):
			if propOf(t, top, "bottom") != "false" || propOf(t, top, "north") != "low" {
				t.Errorf("layer above is %d, want a base-less layer low on the north", top)
			}
			if g := propOf(t, base, "north"); g != "tall" {
				t.Errorf("north under a layer = %s, want tall", g)
			}
		case top == worldgen.Air:
			if g := propOf(t, base, "north"); g != "low" {
				t.Errorf("north against a wall = %s, want low", g)
			}
		default:
			t.Fatalf("cell above the carpet is %d", top)
		}
	}
}

// A pale moss carpet is not a wall: a block resting on it leaves its sides
// low, where a wall's would turn tall, and it carries no post.
func TestPaleMossCarpetIsNotAWall(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1200, 180, 1200
	clearAirBox(w, x, y, z, 1)
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x, y, z-1, worldgen.Stone)
	w.SetBlock(x, y+1, z, worldgen.Stone)
	holdItem(t, p, "pale_moss_carpet")
	s.handlePlace(p, placeBody(x, y-1, z, 1))
	base := w.Block(x, y, z)
	if !isPaleMossCarpet(base) {
		t.Fatalf("no carpet placed (state %d)", base)
	}
	if g := propOf(t, base, "north"); g != "low" {
		t.Errorf("north under a stone lid = %s, want low", g)
	}
	if info, _ := worldgen.InfoForState(base); worldgen.IsWallConnector(info) {
		t.Error("pale moss carpet classified as a wall")
	}
}
