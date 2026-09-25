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

// takes the book (TRY_WITH_EMPTY_HAND → useWithoutItem).
func TestShelfGivesBookWhateverIsHeld(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	pl := testTracked()
	players[1] = pl
	x, y, z := 10, 70, 10
	h.world.SetBlock(x, y, z, withProps(t, bookshelfMin, map[string]string{"facing": "north", "slot_0_occupied": "true"}))
	pos := simPos{blockPos: blockPos{x, y, z}}
	shelf := &[6]invStack{}
	shelf[0] = invStack{item: itemByName["book"], count: 1}
	h.bookshelves[pos] = shelf
	stone := itemByName["stone"]
	pl.inv.slots[0] = invStack{item: stone, count: 5}
	// North face, left third from the front (cx 0.9 → face x 0.1), top row.
	h.onUseShelf(players, evUseShelf{eid: 1, x: x, y: y, z: z, face: 2, cx: 0.9, cy: 0.8, cz: 0})
	if shelf[0].item != 0 {
		t.Fatal("the book should come out with stone in hand")
	}
	found := false
	for _, s := range pl.inv.slots {
		if s.item == itemByName["book"] && s.count == 1 {
			found = true
		}
	}
	if !found || pl.inv.slots[0].item != stone || pl.inv.slots[0].count != 5 {
		t.Errorf("the book should be in the inventory and the stone untouched: %+v", pl.inv.slots[:3])
	}
}

// block is placed against it.
func TestShelfSideClickPlaces(t *testing.T) {
	s, _, p := breakPlaceServer(t)
	w := s.world
	x, y, z := 5, 70, 5
	shelfState := withProps(t, bookshelfMin, map[string]string{"facing": "north"})
	w.SetBlock(x, y, z, shelfState)
	w.SetBlock(x, y+1, z, worldgen.Air)
	p.setHotbarSlot(0, itemByName["stone"])
	selectSlot(p, 0)
	s.handlePlace(p, placeBody(x, y, z, 1)) // the top face
	if w.Block(x, y+1, z) != worldgen.BlockBase("stone") {
		t.Error("a top-face click on a bookshelf should place the held block")
	}
}
