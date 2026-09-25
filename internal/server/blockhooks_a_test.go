package server

import (
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// entity behind it (left over from before a restart) goes when clicked.
func TestClickClearsOrphanMovingPiston(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 3, 70, 3
	w.SetBlock(x, y, z, movingPistonState([3]int{0, 1, 0}, false))
	p.setHotbarSlot(0, 0)
	selectSlot(p, 0)
	s.handlePlace(p, placeBody(x, y, z, 1))
	if !pollUntil(3*time.Second, func() bool { return w.At(x, y, z) == worldgen.Air }) {
		t.Fatal("the orphaned moving piston should be removed by the click")
	}
}

// to the held block, which is placed against it.
func TestIronTrapdoorClickPlacesAgainstIt(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 5, 70, 5
	trap := worldgen.BlockBase("iron_trapdoor")
	w.SetBlock(x, y, z, trap)
	w.SetBlock(x, y+1, z, worldgen.Air)
	p.setHotbarSlot(0, itemByName["stone"])
	selectSlot(p, 0)
	s.handlePlace(p, placeBody(x, y, z, 1))
	if w.Block(x, y, z) != trap {
		t.Error("a hand opened the iron trapdoor")
	}
	if w.Block(x, y+1, z) != worldgen.BlockBase("stone") {
		t.Error("the stone was not placed against the iron trapdoor")
	}
}

// chest gives.
func TestDoubleChestOpenAwardsStat(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	pl := testTracked()
	players[1] = pl
	h.world.SetBlock(10, 70, 10, withProps(t, worldgen.BlockBase("chest"), map[string]string{"facing": "north", "type": "left"}))
	h.world.SetBlock(11, 70, 10, withProps(t, worldgen.BlockBase("chest"), map[string]string{"facing": "north", "type": "right"}))
	h.openChest(pl, 10, 70, 10)
	if pl.winKind != winDoubleChest {
		t.Fatalf("the pair should open as a large chest, kind %d", pl.winKind)
	}
	if got := customStat(pl, "open_chest"); got != 1 {
		t.Errorf("open_chest = %d after opening a large chest, want 1", got)
	}
}
