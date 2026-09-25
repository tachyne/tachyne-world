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
	w.SetBlock(0, 181, 0, upper) // a lower half stands only under its upper half
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

// Column plants stand on their own kind. This is a regression guard for a bug
// that DESTROYED player farms: sugar cane, cactus and bamboo are SupportSoil,
// but the soil list holds no plants, so every segment above the base read as
// unsupported — and since dropUnsupported runs on each nearby block edit,
// breaking any block beside a farm wiped the stack.
func TestColumnPlantsSurviveANeighbourEdit(t *testing.T) {
	for _, tc := range []struct{ name, ground string }{
		{"sugar_cane", "sand"},
		{"cactus", "sand"},
		{"bamboo", "dirt"},
	} {
		h := newHub(world.New(1))
		players := map[int32]*tracked{}
		w := h.worldFor(0)
		x, y, z := 400, 180, 400
		w.SetBlock(x, y-1, z, worldgen.BlockBase(tc.ground))
		plant := worldgen.BlockBase(tc.name)
		for dy := 0; dy < 3; dy++ {
			w.SetBlock(x, y+dy, z, plant)
		}
		// Something changes next door, level with the MIDDLE segment — the
		// sweep only reaches a plant it is adjacent to, so this is the edit
		// that used to eat everything above the base.
		side := blockPos{x + 1, y + 1, z}
		w.SetBlock(side.x, side.y, side.z, worldgen.BlockBase("stone"))
		w.SetBlock(side.x, side.y, side.z, worldgen.Air)
		h.dropUnsupported(players, 0, side)
		for dy := 0; dy < 3; dy++ {
			if got := w.At(x, y+dy, z); got != plant {
				t.Errorf("%s segment %d destroyed by a neighbouring edit (state %d)", tc.name, dy, got)
			}
		}
	}
}

// GrindstoneBlock.canSurvive returns true unconditionally: a grindstone hung
// on a wall stays there when the wall goes. It was classed with the buttons
// and levers, which drop.
func TestGrindstoneNeverDrops(t *testing.T) {
	w := world.New(1)
	base := worldgen.BlockBase("grindstone")
	if got := worldgen.SupportFor(base); got != worldgen.SupportNone {
		t.Fatalf("grindstone support class = %d, want none", got)
	}
	// Nothing around it at all, and it still holds.
	if !supported(w, blockPos{40, 180, 40}, base) {
		t.Fatal("a grindstone in mid-air should still survive")
	}
}

// A wall hanging sign facing north hangs between the blocks east and west
// of it (WallHangingSignBlock.canPlace): it stays while either side holds,
// and drops when both are gone. A second sign turned the same way beside it
// counts as a hold.
func TestWallHangingSignHeldFromTheSides(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	players := map[int32]*tracked{}
	stone := worldgen.BlockBase("stone")
	base := worldgen.BlockBase("oak_wall_hanging_sign")
	info, _ := worldgen.InfoForState(base)
	sign := worldgen.SetProperty(info, base, "facing", "north")
	x, y, z := 4, 180, 4
	h.world.SetBlock(x-1, y, z, stone)
	h.world.SetBlock(x+1, y, z, stone)
	h.world.SetBlock(x, y, z, sign)

	h.setBlockAt(players, 0, blockPos{x - 1, y, z}, worldgen.Air)
	if h.world.At(x, y, z) != sign {
		t.Fatal("the sign fell while its east side still held it")
	}
	h.setBlockAt(players, 0, blockPos{x + 1, y, z}, worldgen.Air)
	if h.world.At(x, y, z) == sign {
		t.Fatal("the sign stayed with nothing on either side")
	}

	// Two signs in a row, the outer one on stone: the inner one holds.
	h.world.SetBlock(x+1, y, z, stone)
	h.world.SetBlock(x, y, z, sign)
	h.world.SetBlock(x-1, y, z, sign)
	h.setBlockAt(players, 0, blockPos{x - 2, y, z}, stone)
	h.setBlockAt(players, 0, blockPos{x - 2, y, z}, worldgen.Air)
	if h.world.At(x-1, y, z) != sign {
		t.Fatal("a sign held by the sign beside it fell")
	}
}

// A snow layer lies on a full top face, honey, soul sand or mud, never on
// ice, and drops when an ice block replaces its floor
// (SnowLayerBlock.canSurvive).
func TestSnowLayerFloors(t *testing.T) {
	w := world.New(1)
	pos := blockPos{4, 180, 4}
	snow := worldgen.BlockBase("snow")
	for name, ok := range map[string]bool{
		"stone": true, "mud": true, "soul_sand": true, "honey_block": true,
		"ice": false, "packed_ice": false, "barrier": false,
	} {
		w.SetBlock(pos.x, pos.y-1, pos.z, worldgen.BlockBase(name))
		if got := supported(w, pos, snow); got != ok {
			t.Errorf("snow on %s: supported %v, want %v", name, got, ok)
		}
	}
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	h.world.SetBlock(pos.x, pos.y-1, pos.z, worldgen.Stone)
	h.world.SetBlock(pos.x, pos.y, pos.z, snow)
	h.setBlockAt(map[int32]*tracked{}, 0, blockPos{pos.x, pos.y - 1, pos.z}, worldgen.BlockBase("ice"))
	if h.world.At(pos.x, pos.y, pos.z) == snow {
		t.Fatal("snow stayed on ice")
	}
}

// A bed half whose partner is destroyed by something other than a player
// (an explosion, a piston) goes too, and so does a door's lower half left
// without its upper; a bed with nothing under it stays (BedBlock and
// DoorBlock.updateShape).
func TestPairedHalvesGoTogether(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	players := map[int32]*tracked{}
	w := h.world
	base := worldgen.BlockBase("red_bed")
	info, _ := worldgen.InfoForState(base)
	foot := worldgen.SetProperty(info, worldgen.SetProperty(info, base, "facing", "east"), "part", "foot")
	head := worldgen.SetProperty(info, foot, "part", "head")
	w.SetBlock(4, 180, 4, foot)
	w.SetBlock(5, 180, 4, head)
	h.setBlockAt(players, 0, blockPos{4, 179, 4}, worldgen.Stone)
	h.setBlockAt(players, 0, blockPos{4, 179, 4}, worldgen.Air) // the floor comes and goes
	if w.At(4, 180, 4) != foot || w.At(5, 180, 4) != head {
		t.Fatal("a bed fell for want of a floor")
	}
	h.setBlockAt(players, 0, blockPos{5, 180, 4}, worldgen.Air) // the head blown away
	if w.At(4, 180, 4) == foot {
		t.Fatal("the foot of a bed stayed without its head")
	}

	door := worldgen.BlockBase("oak_door")
	di, _ := worldgen.InfoForState(door)
	lower := worldgen.SetProperty(di, door, "half", "lower")
	upper := worldgen.SetProperty(di, door, "half", "upper")
	w.SetBlock(8, 179, 8, worldgen.Stone)
	w.SetBlock(8, 180, 8, lower)
	w.SetBlock(8, 181, 8, upper)
	h.setBlockAt(players, 0, blockPos{8, 181, 8}, worldgen.Air)
	if w.At(8, 180, 8) == lower {
		t.Fatal("a door's lower half stayed without its upper half")
	}
}

// A crop needs light to stay (CropBlock.canSurvive: raw brightness >= 8): in
// the open it stands; roofed over in the dark it drops on the next update.
func TestCropNeedsLight(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	players := map[int32]*tracked{}
	w := h.world
	farmland := worldgen.BlockBase("farmland")
	wheat := worldgen.BlockBase("wheat")
	x, y, z := 6, 200, 6
	w.SetBlock(x, y-1, z, farmland)
	w.SetBlock(x, y, z, wheat)
	if !supported(w, blockPos{x, y, z}, wheat) {
		t.Fatal("wheat in the open sky was unsupported")
	}
	// Box it in: stone all round, over it and under the farmland, no torch.
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			for dy := -2; dy <= 1; dy++ {
				if dx == 0 && dz == 0 && (dy == 0 || dy == -1) {
					continue // the wheat and its farmland
				}
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Stone)
			}
		}
	}
	h.setBlockAt(players, 0, blockPos{x + 1, y, z}, worldgen.BlockBase("cobblestone"))
	if w.At(x, y, z) == wheat {
		t.Fatal("wheat shut in the dark stayed")
	}
}

// A comparator reads a copper golem statue and a creaking heart, so taking
// one away must tell the comparators (affectNeighborsAfterRemoval).
func TestStatueAndHeartHaveComparatorOutput(t *testing.T) {
	for _, n := range []string{"copper_golem_statue", "oxidized_copper_golem_statue", "creaking_heart"} {
		if !hasComparatorOutput(worldgen.BlockBase(n)) {
			t.Errorf("%s is missing from hasComparatorOutput", n)
		}
	}
}

// A single-wall bell hangs on the wall it faces (BellBlock.canSurvive); a
// double-wall bell that loses one wall hangs on from the other, and a
// single-wall bell that gains a wall on its free side is held from both.
func TestBellWalls(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	players := map[int32]*tracked{}
	w := h.world
	bell := worldgen.BlockBase("bell")
	info, _ := worldgen.InfoForState(bell)
	set := func(att, facing string) uint32 {
		return worldgen.SetProperty(info, worldgen.SetProperty(info, bell, "attachment", att), "facing", facing)
	}
	prop := func(k string) string { s := w.At(5, 180, 5); return worldgen.GetProperty(info, s, k) }
	// Walls west and east; the bell between them.
	w.SetBlock(4, 180, 5, worldgen.Stone)
	w.SetBlock(6, 180, 5, worldgen.Stone)
	w.SetBlock(5, 180, 5, set("double_wall", "west"))
	h.setBlockAt(players, 0, blockPos{6, 180, 5}, worldgen.Air) // the east wall goes
	if !isBellState(w.At(5, 180, 5)) || prop("attachment") != "single_wall" || prop("facing") != "west" {
		t.Fatalf("after losing its east wall: %s facing %s", prop("attachment"), prop("facing"))
	}
	h.setBlockAt(players, 0, blockPos{4, 180, 5}, worldgen.BlockBase("cobblestone")) // a change beside its wall
	if !isBellState(w.At(5, 180, 5)) {
		t.Fatal("a single-wall bell fell although its wall stands")
	}
	h.setBlockAt(players, 0, blockPos{6, 180, 5}, worldgen.Stone) // a wall back on the free side
	if prop("attachment") != "double_wall" {
		t.Fatalf("with both walls again it is %s", prop("attachment"))
	}
}

func isBellState(s uint32) bool {
	lo, hi, _ := worldgen.BlockRangeOK("bell")
	return s >= lo && s <= hi
}
