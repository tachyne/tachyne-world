package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// wallTorchFacing builds a wall torch pointing the given way (its support is
// the block on the opposite side).
func wallTorchFacing(t *testing.T, facing string) uint32 {
	t.Helper()
	base := worldgen.BlockBase("wall_torch")
	info, ok := worldgen.InfoForState(base)
	if !ok {
		t.Fatal("wall_torch has no state layout")
	}
	return worldgen.SetProperty(info, base, "facing", facing)
}

// The predicate: each support shape asks about the right neighbour.
func TestSupportShapesLookTheRightWay(t *testing.T) {
	w := world.New(1)
	stone := worldgen.BlockBase("stone")
	pos := blockPos{4, 180, 4}

	// Floor: a rail needs a solid top face below it.
	rail := worldgen.BlockBase("rail")
	if supported(w, pos, rail) {
		t.Error("a rail floating in air should not be supported")
	}
	w.SetBlock(pos.x, pos.y-1, pos.z, stone)
	if !supported(w, pos, rail) {
		t.Error("a rail on stone should be supported")
	}

	// Wall: a wall torch facing east hangs off the block to its west.
	east := wallTorchFacing(t, "east")
	if supported(w, pos, east) {
		t.Error("a wall torch with nothing behind it should not be supported")
	}
	w.SetBlock(pos.x-1, pos.y, pos.z, stone)
	if !supported(w, pos, east) {
		t.Error("a wall torch should hang off the block behind it")
	}
	// …and not off the one it points at.
	west := wallTorchFacing(t, "west")
	if supported(w, pos, west) {
		t.Error("a wall torch read its support from the wrong side")
	}

	// Ceiling: a hanging sign needs something above.
	hanging := worldgen.BlockBase("oak_hanging_sign")
	if supported(w, pos, hanging) {
		t.Error("a hanging sign with nothing above it should not be supported")
	}
	w.SetBlock(pos.x, pos.y+1, pos.z, stone)
	if !supported(w, pos, hanging) {
		t.Error("a hanging sign should hang from the block above")
	}

	// Soil: a sapling wants ground, not stone.
	sapling := worldgen.BlockBase("oak_sapling")
	if supported(w, pos, sapling) {
		t.Error("a sapling should not root in stone")
	}
	w.SetBlock(pos.x, pos.y-1, pos.z, worldgen.BlockBase("dirt"))
	if !supported(w, pos, sapling) {
		t.Error("a sapling should root in dirt")
	}

	// A full block asks for nothing.
	if !supported(w, blockPos{40, 180, 40}, stone) {
		t.Error("stone should need no support")
	}
}

// Mining a wall drops what was fixed to it — the case the old six-block,
// above-only check could never catch.
func TestMiningAWallDropsWhatWasOnIt(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	wall := blockPos{0, 180, 0}
	torch := blockPos{1, 180, 0}
	w.SetBlock(wall.x, wall.y, wall.z, worldgen.BlockBase("stone"))
	w.SetBlock(torch.x, torch.y, torch.z, wallTorchFacing(t, "east"))

	// Nothing changes while the wall stands.
	h.dropUnsupported(players, 0, wall)
	if w.At(torch.x, torch.y, torch.z) == worldgen.Air {
		t.Fatal("the torch fell while its wall was still there")
	}

	w.SetBlock(wall.x, wall.y, wall.z, worldgen.Air)
	h.dropUnsupported(players, 0, wall)
	if w.At(torch.x, torch.y, torch.z) != worldgen.Air {
		t.Fatal("the wall torch survived the wall it was fixed to")
	}
	if len(h.items) == 0 {
		t.Error("a block that falls down should leave its drop")
	}
}

// A stack comes down together: dirt, grass on it, and the flower on that.
func TestUnsupportedBlocksCascade(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	w := h.worldFor(0)
	base := blockPos{0, 180, 0}
	w.SetBlock(base.x, base.y, base.z, worldgen.BlockBase("dirt"))
	w.SetBlock(base.x, base.y+1, base.z, worldgen.BlockBase("dandelion"))
	// A lantern hanging under the dirt, and a rail on top of the dandelion's
	// neighbour, to prove the sweep is not only vertical.
	side := blockPos{1, 180, 0}
	w.SetBlock(side.x, side.y, side.z, worldgen.BlockBase("stone"))
	w.SetBlock(side.x, side.y+1, side.z, worldgen.BlockBase("rail"))

	w.SetBlock(base.x, base.y, base.z, worldgen.Air)
	h.dropUnsupported(players, 0, base)
	if w.At(base.x, base.y+1, base.z) != worldgen.Air {
		t.Fatal("the flower kept standing on nothing")
	}
	if w.At(side.x, side.y+1, side.z) == worldgen.Air {
		t.Fatal("the rail on its own stone came down too")
	}
}

// The cases that would have torn down an existing world the first time
// anything near them changed. Each is a shape the naive reading gets wrong.
func TestSupportDoesNotEatExistingBuilds(t *testing.T) {
	w := world.New(1)
	stone := worldgen.BlockBase("stone")
	dirt := worldgen.BlockBase("dirt")

	// The upper half of tall grass stands on the lower half, not on soil.
	tall := worldgen.BlockBase("tall_grass")
	info, _ := worldgen.InfoForState(tall)
	lower := worldgen.SetProperty(info, tall, "half", "lower")
	upper := worldgen.SetProperty(info, tall, "half", "upper")
	w.SetBlock(0, 179, 0, dirt)
	w.SetBlock(0, 180, 0, lower)
	if !supported(w, blockPos{0, 180, 0}, lower) {
		t.Error("tall grass on dirt should stand")
	}
	if !supported(w, blockPos{0, 181, 0}, upper) {
		t.Error("the top half of tall grass rests on its own lower half")
	}

	// An open door: the upper half sits on a lower half that does not collide.
	door := worldgen.BlockBase("oak_door")
	dinfo, _ := worldgen.InfoForState(door)
	dlow := worldgen.SetProperty(dinfo, worldgen.SetProperty(dinfo, door, "half", "lower"), "open", "true")
	dup := worldgen.SetProperty(dinfo, worldgen.SetProperty(dinfo, door, "half", "upper"), "open", "true")
	w.SetBlock(2, 179, 0, stone)
	w.SetBlock(2, 180, 0, dlow)
	if !supported(w, blockPos{2, 181, 0}, dup) {
		t.Error("the top of an open door should stay on its own bottom half")
	}

	// Glow lichen on a cave wall carries no facing at all.
	lichen := worldgen.BlockBase("glow_lichen")
	w.SetBlock(5, 180, 0, stone)
	if !supported(w, blockPos{4, 180, 0}, lichen) {
		t.Error("lichen on a wall should hold")
	}
	if supported(w, blockPos{40, 180, 40}, lichen) {
		t.Error("lichen with nothing to cling to should not hold")
	}

	// A lily pad floats on water, which is not soil and does not collide.
	lily := worldgen.BlockBase("lily_pad")
	w.SetBlock(8, 179, 0, worldgen.WaterBase)
	if !supported(w, blockPos{8, 180, 0}, lily) {
		t.Error("a lily pad should float on water")
	}
	if supported(w, blockPos{9, 180, 0}, lily) {
		t.Error("a lily pad over air should not hold")
	}

	// A torch on a fence post, and a carpet on a slab: legal in vanilla, and
	// the reason support asks whether the face can HOLD something rather than
	// whether the block is a full cube.
	w.SetBlock(12, 179, 0, worldgen.BlockBase("oak_fence"))
	if !supported(w, blockPos{12, 180, 0}, worldgen.BlockBase("torch")) {
		t.Error("a torch on a fence post should stand")
	}
	w.SetBlock(14, 179, 0, worldgen.BlockBase("oak_slab"))
	if !supported(w, blockPos{14, 180, 0}, worldgen.BlockBase("white_carpet")) {
		t.Error("a carpet on a slab should stay")
	}
}
