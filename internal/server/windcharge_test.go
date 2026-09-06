package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A wind charge's burst swings wooden doors (both halves), leaves iron and
// redstone-held doors alone, flips levers and snuffs candles.
func TestWindBurstTriggersBlocks(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	w := h.worldFor(0)

	oakInfo, _ := worldgen.InfoForState(worldgen.BlockBase("oak_door"))
	closedDoor := func(name string) uint32 { // lower, closed, unpowered
		info, _ := worldgen.InfoForState(worldgen.BlockBase(name))
		st := worldgen.SetProperty(info, worldgen.BlockBase(name), "half", "lower")
		st = worldgen.SetProperty(info, st, "open", "false")
		return worldgen.SetProperty(info, st, "powered", "false")
	}
	oak := closedDoor("oak_door")
	upper := worldgen.SetProperty(oakInfo, oak, "half", "upper")
	w.SetBlock(2, 180, 0, oak)
	w.SetBlock(2, 181, 0, upper)
	h.triggerBlock(players, 0, blockPos{2, 180, 0}, oak, pl.p.eid)
	if !boolProp(w.At(2, 180, 0), "open") || !boolProp(w.At(2, 181, 0), "open") {
		t.Fatal("a wind charge should swing both halves of a wooden door")
	}
	h.triggerBlock(players, 0, blockPos{2, 180, 0}, w.At(2, 180, 0), pl.p.eid)
	if boolProp(w.At(2, 180, 0), "open") {
		t.Fatal("a second gust should close it again")
	}
	// The upper half on its own does nothing (vanilla triggers on the lower).
	h.triggerBlock(players, 0, blockPos{2, 181, 0}, w.At(2, 181, 0), pl.p.eid)
	if boolProp(w.At(2, 180, 0), "open") {
		t.Fatal("the upper half must not swing the door")
	}

	iron := closedDoor("iron_door")
	w.SetBlock(-2, 180, 0, iron)
	h.triggerBlock(players, 0, blockPos{-2, 180, 0}, iron, pl.p.eid)
	if boolProp(w.At(-2, 180, 0), "open") {
		t.Fatal("an iron door ignores a wind charge")
	}
	powered := setBoolProp(oak, "powered", true)
	w.SetBlock(0, 180, 2, powered)
	h.triggerBlock(players, 0, blockPos{0, 180, 2}, powered, pl.p.eid)
	if boolProp(w.At(0, 180, 2), "open") {
		t.Fatal("a redstone-held door ignores a wind charge")
	}

	lever := setBoolProp(worldgen.BlockBase("lever"), "powered", false)
	w.SetBlock(0, 180, -2, lever)
	h.triggerBlock(players, 0, blockPos{0, 180, -2}, lever, pl.p.eid)
	if !boolProp(w.At(0, 180, -2), "powered") {
		t.Fatal("a wind charge should flip a lever")
	}

	cInfo, _ := worldgen.InfoForState(worldgen.BlockBase("candle"))
	lit := worldgen.SetProperty(cInfo, worldgen.BlockBase("candle"), "lit", "true")
	w.SetBlock(0, 182, 0, lit)
	h.triggerBlock(players, 0, blockPos{0, 182, 0}, lit, pl.p.eid)
	if boolProp(w.At(0, 182, 0), "lit") {
		t.Fatal("a wind charge should snuff a candle")
	}

	// The real burst: a gust a quarter block off a closed door's face reaches it.
	w.SetBlock(5, 180, 5, oak)
	w.SetBlock(5, 181, 5, upper)
	h.windBurst(players, 0, 4.75, 180.5, 5.5, pl.p.eid)
	if !boolProp(w.At(5, 180, 5), "open") {
		t.Fatal("a burst beside a door should open it")
	}
}
