package server

import (
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// MovingPistonBlock.useWithoutItem: a moving_piston cell with no block
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

// DoorBlock/TrapDoorBlock.useWithoutItem PASS for iron: the click goes on
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

// ChestBlock.useWithoutItem for a pair: the open_chest statistic, as a single
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

// ChiseledBookShelfBlock: whatever is in hand, a click on an occupied slot
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

// A click on a chiseled bookshelf's side (no slot there) PASSes: the held
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

// ComparatorBlock.getInputSignal: through a conductor, an item frame hung on
// its far face is read (rotation % 8 + 1).
func TestComparatorReadsItemFrameThroughBlock(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	comp := withProps(t, comparatorMin, map[string]string{"facing": "north", "mode": "compare"})
	pos := blockPos{0, 180, 0}
	h.world.SetBlock(pos.x, pos.y, pos.z, comp)
	h.world.SetBlock(0, 180, -1, worldgen.Stone)
	h.itemFrames[9001] = &itemFrame{eid: 9001, x: 0, y: 180, z: -2, dir: 2, held: invStack{item: itemByName["stone"], count: 1}, rot: 3}
	var got int
	h.inDim(0, func() { got = h.comparatorOutput(pos, comp) })
	if got != 4 {
		t.Errorf("comparator reads %d from a framed item turned 3 times, want 4", got)
	}
	h.itemFrames[9001].held = invStack{}
	h.inDim(0, func() { got = h.comparatorOutput(pos, comp) })
	if got != 0 {
		t.Errorf("an empty frame reads %d, want 0", got)
	}
}

// DetectorRailBlock.getAnalogOutputSignal: a powered detector rail gives the
// fullness of a container cart on it; a cart on a plain rail is not a block
// and gives a comparator nothing.
func TestDetectorRailReadsContainerCart(t *testing.T) {
	h, players := cartHub()
	cartTrack(h, 0, 20, railMin, false)
	v := specialCart(t, h, players, entityChestMinecart, 5)
	v.chest.slots[0] = invStack{item: itemByName["stone"], count: 64}
	pos := simPos{blockPos: blockPos{5, cartY, 10}}
	if got := h.analogSignal(pos); got >= 0 {
		t.Errorf("a cart on a plain rail reads %d, want no analog output", got)
	}
	h.world.SetBlock(5, cartY, 10, railWith(detectorRailMin, shapeEW, false))
	h.updateVehicles(players)
	if !railPowered(h.world.At(5, cartY, 10)) {
		t.Fatal("the detector rail should press under the cart")
	}
	if got, want := h.analogSignal(pos), 1+14/27; got != want {
		t.Errorf("detector rail under a chest cart with one full slot reads %d, want %d", got, want)
	}
}

// CreakingHeartBlockEntity.serverTick: when the heart's reading changes as
// its creaking moves, the comparator beside it hears of it that tick.
func TestHeartOutputTellsComparator(t *testing.T) {
	h, pos, link := paleTrunk(t)
	players := map[int32]*tracked{}
	h.playersRef = players
	nightHub(h)
	h.world.SetBlock(pos.x, pos.y, pos.z, worldgen.CreakingHeartAwake)
	comp := withProps(t, comparatorMin, map[string]string{"facing": "north", "mode": "compare"})
	cpos := blockPos{pos.x, pos.y, pos.z + 1}
	h.world.SetBlock(cpos.x, cpos.y, cpos.z, comp)
	m := h.spawnMob(players, entityCreaking, float64(pos.x)+10.5, float64(pos.y), float64(pos.z)+0.5)
	link.creaking = m.eid
	link.nextAt = h.tick.Load() + 1000 // the heart's own self-check stays out of it
	h.updateHearts(players)
	if link.outSig == 0 || !h.hasScheduledTick(cpos) {
		t.Errorf("the reading (%d) should reach the comparator, scheduled %v", link.outSig, h.hasScheduledTick(cpos))
	}
}
