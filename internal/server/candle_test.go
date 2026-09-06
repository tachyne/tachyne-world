package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Flint lights an unlit candle (never a wet or burning one), an empty hand
// snuffs it, and eating a candle cake gives the candle back.
func TestCandlesLightSnuffAndCakeReturnsTheCandle(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	pos := blockPos{0, 180, 0}
	info, _ := worldgen.InfoForState(worldgen.BlockBase("blue_candle"))
	unlit := worldgen.SetProperty(info, worldgen.BlockBase("blue_candle"), "lit", "false")
	unlit = worldgen.SetProperty(info, unlit, "waterlogged", "false")
	wet := worldgen.SetProperty(info, unlit, "waterlogged", "true")

	if canLightBlock(wet) {
		t.Fatal("a waterlogged candle must not light")
	}
	if !canLightBlock(unlit) {
		t.Fatal("a dry unlit candle should light")
	}
	h.worldFor(0).SetBlock(pos.x, pos.y, pos.z, unlit)
	h.onLightBlock(players, evLightBlock{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, sound: sndFlintSteelUse})
	lit := h.worldFor(0).At(pos.x, pos.y, pos.z)
	if worldgen.GetProperty(info, lit, "lit") != "true" {
		t.Fatal("flint should light the candle")
	}
	if canLightBlock(lit) {
		t.Fatal("a burning candle is not lightable")
	}

	// A held item passes; an empty hand snuffs it.
	pl.inv.slots[0] = invStack{}
	pl.p.setHotbarSlot(0, 0)
	pl.p.held = 0
	h.useCandle(players, evUseCandle{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, cy: 0.9})
	if got := h.worldFor(0).At(pos.x, pos.y, pos.z); got != unlit {
		t.Fatalf("an empty hand should snuff the candle: state %d, want %d", got, unlit)
	}

	// A lit candle cake: an empty hand on the candle half snuffs it, a click
	// lower down eats the cake and drops the candle.
	cakeBaseState := worldgen.BlockBase("blue_candle_cake") // lit
	h.worldFor(0).SetBlock(pos.x, pos.y, pos.z, cakeBaseState)
	h.useCandle(players, evUseCandle{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, cy: 0.9})
	if got := h.worldFor(0).At(pos.x, pos.y, pos.z); got != cakeBaseState+1 {
		t.Fatalf("snuffing the candle cake gave state %d, want %d", got, cakeBaseState+1)
	}
	pl.food = 10
	before := len(h.items)
	h.useCandle(players, evUseCandle{eid: pl.p.eid, x: pos.x, y: pos.y, z: pos.z, cy: 0.2})
	if bites, ok := cakeBites(h.worldFor(0).At(pos.x, pos.y, pos.z)); !ok || bites != 1 {
		t.Fatalf("eating a candle cake should leave a once-bitten cake, got %v/%d", ok, bites)
	}
	if pl.food != 12 {
		t.Fatalf("the slice should feed two: food=%d", pl.food)
	}
	if len(h.items) != before+1 {
		t.Fatal("the candle should drop as an item")
	}
	for _, it := range h.items {
		if it.item == int32(itemByName["blue_candle"]) {
			return
		}
	}
	t.Fatal("the dropped item is not the blue candle")
}
