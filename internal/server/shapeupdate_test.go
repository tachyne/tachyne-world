package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// shapeSetup is an empty, loaded pocket of air at y=180 with a stone floor.
func shapeSetup(t *testing.T) (*hub, *world.World, map[int32]*tracked, int, int, int) {
	t.Helper()
	w := world.New(1)
	h := newHub(w)
	x, y, z := 8, 180, 8
	w.ForceLoad(x, z, 1)
	for dx := -3; dx <= 3; dx++ {
		for dz := -3; dz <= 3; dz++ {
			w.SetBlock(x+dx, y-2, z+dz, worldgen.Stone)
			for dy := -1; dy <= 3; dy++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	return h, w, map[int32]*tracked{}, x, y, z
}

// SnowyBlock.updateShape: the ground follows the block above it — snow,
// a snow block or powder snow make it snowy, anything else clears it, and
// a change beside it does nothing.
func TestSnowyGroundFollowsTheBlockAbove(t *testing.T) {
	for _, ground := range []string{"grass_block", "podzol", "mycelium"} {
		for _, cover := range []string{"snow", "snow_block", "powder_snow"} {
			h, w, players, x, y, z := shapeSetup(t)
			w.SetBlock(x, y-1, z, worldgen.Stone)
			w.SetBlock(x, y, z, withProps(t, worldgen.BlockBase(ground), map[string]string{"snowy": "false"}))
			snowy := func() string { return worldgen.StateProps(w.At(x, y, z))["snowy"] }

			h.setBlockAt(players, 0, blockPos{x, y + 1, z}, worldgen.BlockBase(cover))
			if snowy() != "true" {
				t.Fatalf("%s under %s: snowy=%s, want true", ground, cover, snowy())
			}
			h.setBlockAt(players, 0, blockPos{x + 1, y, z}, worldgen.Stone)
			if snowy() != "true" {
				t.Fatalf("%s: a block beside it cleared snowy", ground)
			}
			h.setBlockAt(players, 0, blockPos{x, y + 1, z}, worldgen.Air)
			if snowy() != "false" {
				t.Fatalf("%s with %s taken off: snowy=%s, want false", ground, cover, snowy())
			}
		}
	}
}

// FenceGateBlock.updateShape: a wall on either side of the gate's span sinks
// it (in_wall); it rises again only when neither side is a wall, and a wall
// in front of or behind the gate does not count.
func TestFenceGateInWallFollowsTheWallsBesideIt(t *testing.T) {
	h, w, players, x, y, z := shapeSetup(t)
	// Facing north, the gate spans east–west.
	w.SetBlock(x, y, z, withProps(t, worldgen.BlockBase("oak_fence_gate"),
		map[string]string{"facing": "north", "in_wall": "false", "open": "false", "powered": "false"}))
	inWall := func() string { return worldgen.StateProps(w.At(x, y, z))["in_wall"] }
	wall := worldgen.BlockBase("cobblestone_wall")

	h.setBlockAt(players, 0, blockPos{x, y, z - 1}, wall)
	if inWall() != "false" {
		t.Fatal("a wall in front of the gate sank it")
	}
	h.setBlockAt(players, 0, blockPos{x + 1, y, z}, wall)
	if inWall() != "true" {
		t.Fatal("a wall to the east did not sink the gate")
	}
	h.setBlockAt(players, 0, blockPos{x - 1, y, z}, wall)
	h.setBlockAt(players, 0, blockPos{x + 1, y, z}, worldgen.Air)
	if inWall() != "true" {
		t.Fatal("the gate rose with a wall still to its west")
	}
	h.setBlockAt(players, 0, blockPos{x - 1, y, z}, worldgen.Air)
	if inWall() != "false" {
		t.Fatal("the gate stayed sunk with no wall beside it")
	}
}

// AttachedStemBlock.updateShape: take the fruit away and the stem stands
// fully grown again; a change on another side, or the fruit itself staying,
// leaves it attached.
func TestAttachedStemLetsGoOfItsFruit(t *testing.T) {
	h, w, players, x, y, z := shapeSetup(t)
	w.SetBlock(x, y-1, z, worldgen.BlockBase("farmland"))
	attached := withProps(t, worldgen.BlockBase("attached_pumpkin_stem"), map[string]string{"facing": "east"})
	w.SetBlock(x, y, z, attached)
	w.SetBlock(x+1, y, z, pumpkinBlock)

	h.setBlockAt(players, 0, blockPos{x - 1, y, z}, worldgen.Stone)
	if got := w.At(x, y, z); got != attached {
		t.Fatalf("a block on the stem's other side changed it to %d", got)
	}
	h.setBlockAt(players, 0, blockPos{x + 1, y, z}, worldgen.Air)
	if got := w.At(x, y, z); got != pumpkinStemBase+7 {
		t.Fatalf("stem without its pumpkin is %d, want pumpkin_stem age 7 (%d)", got, pumpkinStemBase+7)
	}

	// A melon stem whose melon becomes a pumpkin lets go too: it is not ITS fruit.
	melon := withProps(t, worldgen.BlockBase("attached_melon_stem"), map[string]string{"facing": "south"})
	w.SetBlock(x, y, z, melon)
	w.SetBlock(x, y, z+1, melonBlock)
	h.setBlockAt(players, 0, blockPos{x, y, z + 1}, pumpkinBlock)
	if got := w.At(x, y, z); got != melonStemBase+7 {
		t.Fatalf("melon stem beside a pumpkin is %d, want melon_stem age 7 (%d)", got, melonStemBase+7)
	}
}

// HugeMushroomBlock.updateShape: a face turned to more of the same block
// closes; a different mushroom block does not close it, and taking the
// neighbour away does not reopen it.
func TestHugeMushroomFacesCloseAgainstTheirOwnKind(t *testing.T) {
	h, w, players, x, y, z := shapeSetup(t)
	all := map[string]string{"up": "true", "down": "true", "north": "true", "south": "true", "east": "true", "west": "true"}
	w.SetBlock(x, y, z, withProps(t, worldgen.BlockBase("brown_mushroom_block"), all))
	face := func(f string) string { return worldgen.StateProps(w.At(x, y, z))[f] }

	h.setBlockAt(players, 0, blockPos{x + 1, y, z}, withProps(t, worldgen.BlockBase("brown_mushroom_block"), all))
	if face("east") != "false" {
		t.Fatal("the east face stayed open against another brown mushroom block")
	}
	for _, f := range []string{"up", "down", "north", "south", "west"} {
		if face(f) != "true" {
			t.Fatalf("the %s face closed with nothing there", f)
		}
	}
	h.setBlockAt(players, 0, blockPos{x - 1, y, z}, worldgen.BlockBase("red_mushroom_block"))
	if face("west") != "true" {
		t.Fatal("the west face closed against a RED mushroom block")
	}
	h.setBlockAt(players, 0, blockPos{x, y + 1, z}, withProps(t, worldgen.BlockBase("brown_mushroom_block"), all))
	if face("up") != "false" {
		t.Fatal("the top face stayed open against a brown mushroom block above")
	}
	h.setBlockAt(players, 0, blockPos{x + 1, y, z}, worldgen.Air)
	if face("east") != "false" {
		t.Fatal("the east face reopened when its neighbour went")
	}
}

// PistonHeadBlock.neighborChanged: an update that reaches the head is passed
// on to its base at once. An extended piston with no power, told only through
// its head, queues its retraction in the same cascade.
func TestPistonHeadForwardsUpdatesToItsBase(t *testing.T) {
	h, w, players, x, y, z := shapeSetup(t)
	w.SetBlock(x, y, z, withProps(t, worldgen.BlockBase("piston"), map[string]string{"facing": "east", "extended": "true"}))
	head := withProps(t, worldgen.BlockBase("piston_head"), map[string]string{"facing": "east", "short": "false", "type": "normal"})
	w.SetBlock(x+1, y, z, head)

	h.notifyAround(players, 0, blockPos{x + 2, y, z}) // the cell in front of the head: no neighbour of the base
	want := blockEvent{pos: simPos{dim: 0, blockPos: blockPos{x, y, z}}, extend: false}
	found := false
	for _, e := range h.blockEvents {
		if e == want {
			found = true
		}
	}
	if !found {
		t.Fatalf("the base never heard the update through its head: block events %v", h.blockEvents)
	}

	// A head whose base is not its own (a sticky head on a plain piston)
	// passes nothing on.
	h.blockEvents = nil
	w.SetBlock(x+1, y, z, withProps(t, head, map[string]string{"type": "sticky"}))
	h.notifyAround(players, 0, blockPos{x + 2, y, z})
	if len(h.blockEvents) != 0 {
		t.Fatalf("a mismatched head forwarded to the base: %v", h.blockEvents)
	}
}
