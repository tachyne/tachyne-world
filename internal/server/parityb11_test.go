package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Shears carve a pumpkin toward the clicked side and pop out four seeds;
// a click on a dark redstone ore lights it; a comparator beside a chiseled
// bookshelf reads the slot last touched.
func TestCarveOreAndShelfSlot(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	w := h.worldFor(0)

	w.SetBlock(0, 180, 0, pumpkinBlock)
	pl.inv.slots[0] = invStack{item: itemShears, count: 1}
	pl.p.setHotbarSlot(0, itemShears)
	pl.p.held = 0
	before := len(h.items)
	h.carvePumpkin(players, evCarvePumpkin{eid: pl.p.eid, x: 0, y: 180, z: 0, face: 4, yaw: 0}) // west face
	got := w.At(0, 180, 0)
	info, _ := worldgen.InfoForState(carvedPumpkinBase)
	if got < carvedPumpkinBase || got > carvedPumpkinBase+3 || worldgen.GetProperty(info, got, "facing") != "west" {
		t.Fatalf("carved pumpkin state %d (facing %q), want a west-facing carved pumpkin", got, worldgen.GetProperty(info, got, "facing"))
	}
	if len(h.items) != before+1 {
		t.Fatal("carving should drop the seeds")
	}
	if pl.inv.slots[0].dmg != 1 {
		t.Fatalf("the shears should wear one point, dmg=%d", pl.inv.slots[0].dmg)
	}
	// Top face: the carved side faces away from a player looking south (yaw 0).
	w.SetBlock(0, 180, 2, pumpkinBlock)
	h.carvePumpkin(players, evCarvePumpkin{eid: pl.p.eid, x: 0, y: 180, z: 2, face: 1, yaw: 0})
	if f := worldgen.GetProperty(info, w.At(0, 180, 2), "facing"); f != "north" {
		t.Fatalf("top-face carve facing %q, want north (away from a south-looking player)", f)
	}

	dark := setBoolProp(worldgen.BlockBase("redstone_ore"), "lit", false)
	w.SetBlock(0, 180, -2, dark)
	h.lightOre(players, evLightOre{eid: pl.p.eid, x: 0, y: 180, z: -2})
	if !boolProp(w.At(0, 180, -2), "lit") {
		t.Fatal("a click should light redstone ore")
	}

	// Bookshelf: slot 4 (bottom row, middle) last touched → comparator 5.
	shelfInfo, _ := worldgen.InfoForState(bookshelfMin)
	shelf := worldgen.SetProperty(shelfInfo, bookshelfMin, "facing", "north")
	for i := 0; i < 6; i++ {
		shelf = worldgen.SetProperty(shelfInfo, shelf, "slot_"+string(rune('0'+i))+"_occupied", "false")
	}
	w.SetBlock(3, 180, 0, shelf)
	pos := simPos{blockPos: blockPos{3, 180, 0}}
	if sig := h.analogSignal(pos); sig != 0 {
		t.Fatalf("an untouched shelf reads %d, want 0", sig)
	}
	book := itemByName["book"]
	pl.inv.slots[0] = invStack{item: book, count: 1}
	pl.p.setHotbarSlot(0, book)
	// North face (2), face-local x for north is 1-cx: cx=0.5 → middle column; cy<0.5 → bottom row.
	h.onUseShelf(players, evUseShelf{eid: pl.p.eid, x: 3, y: 180, z: 0, face: 2, cx: 0.5, cy: 0.25, cz: 0})
	if sig := h.analogSignal(pos); sig != 5 {
		t.Fatalf("after putting a book in slot 4 the shelf reads %d, want 5", sig)
	}
}

// A wet sponge placed in the Nether dries out at once.
func TestWetSpongeDriesInTheNether(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	nw := h.worldFor(dimNether)
	if nw == nil {
		t.Skip("no nether world in this hub")
	}
	nw.SetBlock(0, 60, 0, wetSpongeState)
	h.soakSponge(players, dimNether, blockPos{0, 60, 0})
	if nw.At(0, 60, 0) != spongeState {
		t.Fatal("a wet sponge in the Nether should dry to a sponge")
	}
	// In the overworld it stays wet.
	h.worldFor(0).SetBlock(0, 180, 0, wetSpongeState)
	h.soakSponge(players, 0, blockPos{0, 180, 0})
	if h.worldFor(0).At(0, 180, 0) != wetSpongeState {
		t.Fatal("a wet sponge in the overworld must stay wet")
	}
}
