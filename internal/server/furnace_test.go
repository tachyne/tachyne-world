package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

var (
	tRawIron   = itemByName["raw_iron"]
	tIronIngot = itemByName["iron_ingot"]
	tCoal      = itemByName["coal"]
)

func furnaceSetup() (*hub, map[int32]*tracked, *tracked, *furnace) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := testTracked()
	players[1] = pl
	h.openFurnace(pl, 10, 70, 10)
	return h, players, pl, h.furnaces[blockPos{10, 70, 10}]
}

func TestFurnaceSmeltsIron(t *testing.T) {
	h, players, pl, f := furnaceSetup()
	if f == nil || pl.winID == 0 || pl.winKind != winFurnace {
		t.Fatalf("furnace window should be open: %+v", pl.winID)
	}
	f.slots[furnaceInput] = invStack{item: tRawIron, count: 2}
	f.slots[furnaceFuel] = invStack{item: tCoal, count: 1}

	for i := 0; i < 200; i++ {
		h.updateFurnaces(players)
	}
	if f.slots[furnaceOutput].item != tIronIngot || f.slots[furnaceOutput].count != 1 {
		t.Fatalf("200 ticks should smelt one ingot, output=%+v", f.slots[furnaceOutput])
	}
	if f.slots[furnaceInput].count != 1 {
		t.Fatalf("one raw iron should be consumed, input=%+v", f.slots[furnaceInput])
	}
	if f.slots[furnaceFuel].item != 0 {
		t.Fatalf("the coal should be consumed (burning), fuel=%+v", f.slots[furnaceFuel])
	}
	// Coal burns 1600 ticks = 8 smelts; the second item finishes by tick 400.
	for i := 0; i < 200; i++ {
		h.updateFurnaces(players)
	}
	if f.slots[furnaceOutput].count != 2 || f.slots[furnaceInput].item != 0 {
		t.Fatalf("second ingot should finish on the same coal: out=%+v in=%+v",
			f.slots[furnaceOutput], f.slots[furnaceInput])
	}
}

func TestFurnaceNeedsFuel(t *testing.T) {
	h, players, _, f := furnaceSetup()
	f.slots[furnaceInput] = invStack{item: tRawIron, count: 1}
	for i := 0; i < 300; i++ {
		h.updateFurnaces(players)
	}
	if f.slots[furnaceOutput].item != 0 {
		t.Fatalf("no fuel must mean no smelting, output=%+v", f.slots[furnaceOutput])
	}
}

func TestFurnaceIgnoresNonSmeltable(t *testing.T) {
	h, players, _, f := furnaceSetup()
	f.slots[furnaceInput] = invStack{item: tStick, count: 1}
	f.slots[furnaceFuel] = invStack{item: tCoal, count: 1}
	for i := 0; i < 300; i++ {
		h.updateFurnaces(players)
	}
	if f.slots[furnaceOutput].item != 0 || f.slots[furnaceFuel].count != 1 {
		t.Fatalf("non-smeltable input must not burn fuel: out=%+v fuel=%+v",
			f.slots[furnaceOutput], f.slots[furnaceFuel])
	}
}

func TestFurnaceKeepsContentsOnClose(t *testing.T) {
	h, players, pl, f := furnaceSetup()
	f.slots[furnaceInput] = invStack{item: tRawIron, count: 3}
	h.closeWindow(players, pl)
	if pl.winKind == winFurnace || pl.winID != 0 {
		t.Fatalf("window should be closed: %+v", pl)
	}
	f2 := h.furnaces[blockPos{10, 70, 10}]
	if f2 == nil || f2.slots[furnaceInput].count != 3 {
		t.Fatalf("furnace must keep its contents after close, got %+v", f2)
	}
	if f2.viewer != 0 {
		t.Fatalf("viewer should be released, got %d", f2.viewer)
	}
}

func TestFurnaceSmeltsUnwatched(t *testing.T) {
	h, players, pl, f := furnaceSetup()
	f.slots[furnaceInput] = invStack{item: tRawIron, count: 1}
	f.slots[furnaceFuel] = invStack{item: tCoal, count: 1}
	h.closeWindow(players, pl)
	for i := 0; i < 200; i++ {
		h.updateFurnaces(players)
	}
	if f.slots[furnaceOutput].item != tIronIngot {
		t.Fatalf("furnace should smelt while unwatched, output=%+v", f.slots[furnaceOutput])
	}
}

func TestFurnaceClickSlotMapping(t *testing.T) {
	h, players, pl, f := furnaceSetup()
	pl.inv.slots[0] = invStack{item: tRawIron, count: 4}
	// Client moves raw iron from hotbar (furnace window slot 30) to input (0).
	h.handleClick(players, evClick{
		eid: 1, windowID: pl.winID, slot: 0, mode: 0,
		changed: []slotChange{
			{slot: 30, st: invStack{item: tRawIron, count: 3}},
			{slot: 0, st: invStack{item: tRawIron, count: 1}},
		},
	})
	if f.slots[furnaceInput].item != tRawIron || f.slots[furnaceInput].count != 1 {
		t.Fatalf("click should land raw iron in the furnace input, got %+v", f.slots[furnaceInput])
	}
	if pl.inv.slots[0].count != 3 {
		t.Fatalf("hotbar should have 3 left, got %+v", pl.inv.slots[0])
	}
}

func TestOrphanedLitFurnaceExtinguishedAtBoot(t *testing.T) {
	w := world.New(1)
	// A lit furnace persisted from a crashed/restarted burn (even offset = lit).
	w.SetBlock(8, 70, 8, furnaceStateMin)
	h := newHub(w)
	h.reconcileFurnaceBlocks()
	if got := w.Block(8, 70, 8); got != furnaceStateMin+1 {
		t.Fatalf("boot sweep should unlight the furnace: state %d, want %d", got, furnaceStateMin+1)
	}
	// An unlit furnace is untouched.
	w.SetBlock(9, 70, 8, furnaceStateMin+1)
	h.reconcileFurnaceBlocks()
	if got := w.Block(9, 70, 8); got != furnaceStateMin+1 {
		t.Fatalf("unlit furnace must be untouched, got %d", got)
	}
}

func TestBootSweepRelightsRestoredBurningFurnace(t *testing.T) {
	w := world.New(1)
	// The furnace block was persisted UNLIT (e.g. an older sweep), but restored
	// state says it's mid-burn: the sweep must relight it, not extinguish.
	w.SetBlock(8, 70, 8, furnaceStateMin+1)
	h := newHub(w)
	h.furnaces[blockPos{8, 70, 8}] = &furnace{burnLeft: 500, burnMax: 1600, cookMax: 200}
	h.reconcileFurnaceBlocks()
	if got := w.Block(8, 70, 8); got != furnaceStateMin {
		t.Fatalf("burning restored furnace should be relit: state %d, want %d", got, furnaceStateMin)
	}
	// And a lit block whose restored furnace is NOT burning goes out.
	w.SetBlock(9, 70, 8, furnaceStateMin)
	h.furnaces[blockPos{9, 70, 8}] = &furnace{cookMax: 200}
	h.reconcileFurnaceBlocks()
	if got := w.Block(9, 70, 8); got != furnaceStateMin+1 {
		t.Fatalf("non-burning furnace block should be unlit: state %d", got)
	}
}

func TestContainerStoreFurnaceRoundTrip(t *testing.T) {
	path := t.TempDir() + "/containers.json"
	s := newContainerStore(path)
	in := map[blockPos]*furnace{
		{10, 64, -3}: {
			slots:    [3]invStack{{item: tRawIron, count: 5}, {item: itemByName["leather_chestplate"], count: 2}, {item: tIronIngot, count: 7}},
			burnLeft: 123, burnMax: 1600, cook: 42, cookMax: 200,
		},
	}
	s.recordFurnaces(in)
	s.flush()

	got := newContainerStore(path).loadFurnaces()
	f := got[blockPos{10, 64, -3}]
	if f == nil {
		t.Fatal("furnace not restored")
	}
	if f.slots != in[blockPos{10, 64, -3}].slots {
		t.Fatalf("slots mismatch: %+v", f.slots)
	}
	if f.burnLeft != 123 || f.burnMax != 1600 || f.cook != 42 || f.cookMax != 200 {
		t.Fatalf("progress mismatch: %+v", f)
	}
	if f.viewer != 0 {
		t.Fatalf("viewer must reset on load, got %d", f.viewer)
	}
}

func TestFurnaceLitFollowsWorldBlock(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.world.SetBlock(10, 70, 10, furnaceStateMin+1) // placed unlit
	pl := testTracked()
	players[1] = pl
	h.openFurnace(pl, 10, 70, 10)
	f := h.furnaces[blockPos{10, 70, 10}]
	f.slots[furnaceInput] = invStack{item: tRawIron, count: 1}
	f.slots[furnaceFuel] = invStack{item: tStick, count: 1} // 100 ticks, dies mid-cook

	h.updateFurnaces(players)
	if got := h.world.Block(10, 70, 10); got != furnaceStateMin {
		t.Fatalf("ignition should light the block: %d", got)
	}
	for i := 0; i < 150; i++ {
		h.updateFurnaces(players)
	}
	if got := h.world.Block(10, 70, 10); got != furnaceStateMin+1 {
		t.Fatalf("burnout mid-cook should unlight the block: %d", got)
	}
}
