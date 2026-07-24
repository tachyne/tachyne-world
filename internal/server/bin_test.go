package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// dispEast builds an untriggered dispenser/dropper facing east.
func dispEast(base uint32) uint32 {
	info, _ := worldgen.InfoForState(base + 1)
	s := worldgen.SetProperty(info, base+1, "facing", "east")
	return setBoolProp(s, "triggered", false)
}

func TestDropperEjectsOnRisingEdge(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, dispEast(dropperMin))
	pos := blockPos{x, y, z}
	h.bins[pos] = &bin{slots: make([]invStack, 9)}
	h.bins[pos].slots[3] = invStack{item: 35, count: 5} // some cobble mid-bin
	lever := setBoolProp(uint32((worldgen.BlockBase("lever") + 9)), "powered", false)
	w.SetBlock(x, y, z-1, lever)
	h.toggleLever(players, blockPos{x, y, z - 1}, w.At(x, y, z-1))
	stepTicks(h, players, 8) // rising edge + vanilla's 4-tick dispense delay
	if h.bins[pos].slots[3].count != 4 {
		t.Fatalf("one item should eject on the rising edge, left %d", h.bins[pos].slots[3].count)
	}
	if len(h.items) != 1 {
		t.Fatalf("ejected item should be an entity, have %d", len(h.items))
	}
	// Held power: NO further ejection (edge, not level).
	stepTicks(h, players, 20)
	if h.bins[pos].slots[3].count != 4 {
		t.Fatal("held power must not re-eject")
	}
	// Off + on again: one more.
	h.toggleLever(players, blockPos{x, y, z - 1}, w.At(x, y, z-1))
	stepTicks(h, players, 8)
	h.toggleLever(players, blockPos{x, y, z - 1}, w.At(x, y, z-1))
	stepTicks(h, players, 8)
	if h.bins[pos].slots[3].count != 3 {
		t.Fatalf("second rising edge should eject again, left %d", h.bins[pos].slots[3].count)
	}
}

func TestDispenserShootsArrows(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, dispEast(dispenserMin))
	pos := blockPos{x, y, z}
	h.bins[pos] = &bin{slots: make([]invStack, 9)}
	h.bins[pos].slots[0] = invStack{item: itemArrowAmmo, count: 2}
	w.SetBlock(x, y, z-1, worldgen.BlockBase("redstone_block"))
	h.scheduleAround(pos, 1)
	stepTicks(h, players, 6) // rising edge (tick 1) + vanilla's 4-tick dispense delay
	if h.bins[pos].slots[0].count != 1 {
		t.Fatalf("arrow should be consumed, left %d", h.bins[pos].slots[0].count)
	}
	if len(h.arrows) != 1 {
		t.Fatalf("dispensed arrow should fly, have %d arrow entities", len(h.arrows))
	}
}

func TestHopperPullsAndPushes(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	// chest above → hopper (facing down) → chest below.
	w.SetBlock(x, y+1, z, chestStateMin)
	w.SetBlock(x, y, z, hopperMin) // enabled, facing down
	w.SetBlock(x, y-1, z, chestStateMin)
	top, bot := &chest{}, &chest{}
	top.slots[5] = invStack{item: 35, count: 3}
	h.chests[blockPos{x, y + 1, z}] = top
	h.chests[blockPos{x, y - 1, z}] = bot
	h.schedule(blockPos{x, y, z}, 1)
	stepTicks(h, players, 8*7) // 7 cadences: 3 pulls + 3 pushes overlap
	if top.slots[5].count != 0 && top.slots[5].item != 0 {
		t.Fatalf("top chest should drain: %+v", top.slots[5])
	}
	if bot.slots[0].item != 35 || bot.slots[0].count != 3 {
		t.Fatalf("bottom chest should receive all 3: %+v", bot.slots[0])
	}
}

func TestHopperPausesWhenPowered(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, hopperMin)
	pos := blockPos{x, y, z}
	h.bins[pos] = &bin{slots: make([]invStack, 5)}
	h.bins[pos].slots[0] = invStack{item: 35, count: 1}
	w.SetBlock(x, y-1, z, chestStateMin)
	bot := &chest{}
	h.chests[blockPos{x, y - 1, z}] = bot
	w.SetBlock(x+1, y, z, worldgen.BlockBase("redstone_block")) // powered → disabled
	h.schedule(pos, 1)
	stepTicks(h, players, 24)
	if bot.slots[0].item != 0 {
		t.Fatal("a powered hopper must not move items")
	}
	if hopperEnabled(w.At(x, y, z)) {
		t.Fatal("powered hopper should show enabled=false")
	}
	w.SetBlock(x+1, y, z, worldgen.Stone) // unpower
	h.scheduleAround(pos, 1)
	stepTicks(h, players, 24)
	if bot.slots[0].item != 35 {
		t.Fatalf("unpowered hopper should resume: %+v", bot.slots[0])
	}
}

func TestHopperSucksItemEntities(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, hopperMin)
	pos := blockPos{x, y, z}
	it := h.spawnItem(players, 35, 7, float64(x)+0.5, float64(y)+1.2, float64(z)+0.5)
	if it == nil {
		t.Fatal("test item entity failed to spawn")
	}
	h.schedule(pos, 1)
	stepTicks(h, players, 16)
	if len(h.items) != 0 {
		t.Fatalf("hopper should vacuum the drop, %d items remain", len(h.items))
	}
	if c := h.bins[pos]; c == nil || c.slots[0].item != 35 || c.slots[0].count != 7 {
		t.Fatalf("stack should land in the hopper: %+v", h.bins[pos])
	}
}

func TestComparatorReadsContainer(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, chestStateMin)
	c := &chest{}
	for i := 0; i < 27; i++ { // stuffed chest → signal 15
		c.slots[i] = invStack{item: 35, count: 64}
	}
	h.chests[blockPos{x, y, z}] = c
	info, _ := worldgen.InfoForState(uint32((worldgen.BlockBase("comparator") + 1)))
	comp := worldgen.SetProperty(info, uint32((worldgen.BlockBase("comparator") + 1)), "facing", "west")
	w.SetBlock(x+1, y, z, comp)
	w.SetBlock(x+2, y, z, (worldgen.BlockBase("redstone_wire") + 1160))
	h.schedule(blockPos{x + 1, y, z}, 1)
	stepTicks(h, players, 6)
	if p := wirePower(w.At(x+2, y, z)); p != 15 {
		t.Fatalf("full chest should read 15, wire has %d", p)
	}
	c.slots = [27]invStack{} // empty it
	h.schedule(blockPos{x + 1, y, z}, 1)
	stepTicks(h, players, 6)
	if p := wirePower(w.At(x+2, y, z)); p != 0 {
		t.Fatalf("empty chest should read 0, wire has %d", p)
	}
}
