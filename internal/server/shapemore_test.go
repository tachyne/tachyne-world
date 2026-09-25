package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// MossyCarpetBlock.updateShape: a carpet's side up a wall goes when the wall
// does, and an upper layer that is left with no side at all goes with it.
func TestPaleMossCarpetSidesFollowTheWall(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	players := map[int32]*tracked{}
	const x, y, z = 44, 180, 44
	def := worldgen.BlockID("pale_moss_carpet")
	info, _ := worldgen.InfoForState(def)
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x, y, z-1, worldgen.Stone)   // north wall, both layers
	w.SetBlock(x, y+1, z-1, worldgen.Stone) //
	base := worldgen.SetProperty(info, def, "bottom", "true")
	base = worldgen.SetProperty(info, base, "north", "tall")
	top := worldgen.SetProperty(info, def, "bottom", "false")
	top = worldgen.SetProperty(info, top, "north", "low")
	w.SetBlock(x, y, z, base)
	w.SetBlock(x, y+1, z, top)

	h.setBlockAt(players, 0, blockPos{x, y + 1, z - 1}, worldgen.Air)
	if got := w.At(x, y+1, z); got != worldgen.Air {
		t.Fatalf("the upper layer lost its only wall but stayed: %d", got)
	}
	h.setBlockAt(players, 0, blockPos{x, y, z - 1}, worldgen.Air)
	got := w.At(x, y, z)
	if !isPaleMossCarpet(got) || worldgen.GetProperty(info, got, "north") != "none" {
		t.Fatalf("the base carpet kept a side on a wall that is gone: %d", got)
	}
}

// BambooSaplingBlock.updateShape turns the shoot into a stalk when bamboo
// grows on it; BambooStalkBlock.updateShape passes an older stalk's AGE down.
func TestBambooShootAndStalkFollowTheBambooAbove(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	players := map[int32]*tracked{}
	const x, y, z = 48, 180, 48
	w.SetBlock(x, y-1, z, worldgen.BlockID("dirt"))
	w.SetBlock(x, y, z, worldgen.BlockID("bamboo_sapling"))
	h.setBlockAt(players, 0, blockPos{x, y + 1, z}, bambooState(0, bambooLeavesSmall, 0))
	if got := w.At(x, y, z); got != worldgen.BlockID("bamboo") {
		t.Fatalf("the shoot under new bamboo is %d, want a bamboo stalk", got)
	}
	h.setBlockAt(players, 0, blockPos{x, y + 2, z}, bambooState(1, bambooLeavesSmall, 0))
	if got := w.At(x, y+1, z); bambooAge(got) != 1 {
		t.Errorf("a stalk under an older one kept age %d", bambooAge(got))
	}
	if got := w.At(x, y, z); bambooAge(got) != 1 {
		t.Errorf("the age did not run down the stalk: %d", bambooAge(got))
	}
}

// HangingMossBlock: moss hangs from moss (canStayAtPosition), and TIP marks
// the strand's last cell.
func TestHangingMossStrand(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	players := map[int32]*tracked{}
	const x, y, z = 52, 180, 52
	moss := worldgen.BlockID("pale_hanging_moss")
	info, _ := worldgen.InfoForState(moss)
	body := worldgen.SetProperty(info, moss, "tip", "false")
	tip := worldgen.SetProperty(info, moss, "tip", "true")
	w.SetBlock(x, y+3, z, worldgen.Stone)
	w.SetBlock(x, y+2, z, body)
	w.SetBlock(x, y+1, z, body)
	w.SetBlock(x, y, z, tip)

	h.setBlockAt(players, 0, blockPos{x + 1, y + 1, z}, worldgen.Stone) // any nearby edit
	if w.At(x, y+1, z) != body || w.At(x, y, z) != tip {
		t.Fatalf("a strand fell apart on a nearby edit: %d %d", w.At(x, y+1, z), w.At(x, y, z))
	}
	h.setBlockAt(players, 0, blockPos{x, y, z}, worldgen.Air)
	if got := w.At(x, y+1, z); got != tip {
		t.Errorf("the new last cell is %d, want a tip (%d)", got, tip)
	}
}

// BigDripleafBlock.updateShape: a leaf with another leaf set on top of it
// becomes stem, keeping its facing.
func TestBigDripleafUnderALeafBecomesStem(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	players := map[int32]*tracked{}
	const x, y, z = 56, 180, 56
	leaf := worldgen.BlockID("big_dripleaf")
	info, _ := worldgen.InfoForState(leaf)
	leaf = worldgen.SetProperty(info, leaf, "facing", "west")
	w.SetBlock(x, y-1, z, worldgen.BlockID("moss_block"))
	w.SetBlock(x, y, z, leaf)
	h.setBlockAt(players, 0, blockPos{x, y + 1, z}, leaf)
	got := w.At(x, y, z)
	if !inRange(got, dripleafStemRng) {
		t.Fatalf("a leaf under a leaf is %d, want stem", got)
	}
	if si, _ := worldgen.InfoForState(got); worldgen.GetProperty(si, got, "facing") != "west" {
		t.Errorf("the stem turned to %s", worldgen.GetProperty(si, got, "facing"))
	}
}
