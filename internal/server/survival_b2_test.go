package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// CarpetBlock.canSurvive: anything that is not air holds a carpet.
func TestCarpetOnAnythingButAir(t *testing.T) {
	w := world.New(1)
	const x, y, z = 94, 180, 94
	carpet := worldgen.BlockID("red_carpet")
	for _, below := range []uint32{worldgen.BlockID("torch"), worldgen.BlockID("poppy"), worldgen.WaterBase} {
		w.SetBlock(x, y-1, z, below)
		if !supported(w, blockPos{x, y, z}, carpet) {
			t.Errorf("a carpet on %d should stay", below)
		}
	}
	w.SetBlock(x, y-1, z, worldgen.Air)
	if supported(w, blockPos{x, y, z}, carpet) {
		t.Error("a carpet on air stayed")
	}
}

// FireBlock.canSurvive on a neighbour change: a fire with no floor and
// nothing to burn beside it goes out at once.
func TestFireGoesWithItsFloor(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	players := map[int32]*tracked{}
	const x, y, z = 100, 180, 100
	w.SetBlock(x, y-1, z, worldgen.Stone)
	w.SetBlock(x, y, z, fireDefault)
	h.setBlockAt(players, 0, blockPos{x + 1, y, z}, worldgen.Stone) // an unrelated edit beside it
	if w.At(x, y, z) != fireDefault {
		t.Fatal("a fire on stone went out")
	}
	h.setBlockAt(players, 0, blockPos{x, y - 1, z}, worldgen.Air)
	if got := w.At(x, y, z); got != worldgen.Air {
		t.Fatalf("a fire whose floor went is still %d", got)
	}
}

// AmethystClusterBlock: a bud points out of the face it was set on — down
// under a ceiling — and needs the block it points away from.
func TestAmethystBudFacesTheClickedFace(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 1330, 180, 1330
	clearAirBox(w, x, y, z, 3)
	w.SetBlock(x, y+1, z, worldgen.Stone)
	p.setHotbarSlot(0, itemByName["small_amethyst_bud"])
	selectSlot(p, 0)
	p.yaw, p.pitch = 0, -60
	s.handlePlace(p, placeBody(x, y+1, z, 0)) // the ceiling's underside
	got := w.Block(x, y, z)
	if st, _ := amethystStage(got); st != 0 || propOf(t, got, "facing") != "down" {
		t.Fatalf("a bud under a ceiling should face down, got %d", got)
	}
	w.SetBlock(x, y-1, z, worldgen.Stone) // a floor does not hold a hanging bud
	w.SetBlock(x, y+1, z, worldgen.Air)
	if supported(w, blockPos{x, y, z}, got) {
		t.Fatal("a downward bud without its ceiling is still supported")
	}
}
