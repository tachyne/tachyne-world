package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// axeHub is a hub with one survival player standing beside (10,70,10).
func axeHub(t *testing.T) (*hub, *tracked, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := riderAt(1, 11.5, 70, 10.5)
	pl.adv = advState{}
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	return h, pl, players
}

// An axe strips a log to its stripped form on the same axis, and wears a point.
func TestAxeStripsLog(t *testing.T) {
	h, pl, players := axeHub(t)
	log := withProps(t, worldgen.BlockBase("oak_log"), map[string]string{"axis": "z"})
	h.world.SetBlock(10, 70, 10, log)
	give(pl, itemByName["iron_axe"])

	h.onUseAxe(players, evUseAxe{eid: 1, x: 10, y: 70, z: 10, slot: 0})

	want := withProps(t, worldgen.BlockBase("stripped_oak_log"), map[string]string{"axis": "z"})
	if got := h.world.At(10, 70, 10); got != want {
		t.Fatalf("stripped log state %d, want %d", got, want)
	}
	if pl.inv.slots[0].dmg != 1 {
		t.Errorf("axe wear %d, want 1", pl.inv.slots[0].dmg)
	}
}

// An axe takes one oxidation stage off copper, then (once unaffected) nothing
// more; on waxed copper it removes the wax and leaves the stage alone.
func TestAxeScrapesAndUnwaxesCopper(t *testing.T) {
	h, pl, players := axeHub(t)
	give(pl, itemByName["iron_axe"])

	h.world.SetBlock(10, 70, 10, worldgen.BlockBase("weathered_copper"))
	h.onUseAxe(players, evUseAxe{eid: 1, x: 10, y: 70, z: 10, slot: 0})
	if got := h.world.At(10, 70, 10); got != worldgen.BlockBase("exposed_copper") {
		t.Fatalf("scraped weathered copper → %d, want exposed_copper", got)
	}

	h.world.SetBlock(10, 70, 10, worldgen.BlockBase("copper_block"))
	h.onUseAxe(players, evUseAxe{eid: 1, x: 10, y: 70, z: 10, slot: 0})
	if got := h.world.At(10, 70, 10); got != worldgen.BlockBase("copper_block") {
		t.Fatalf("unaffected copper changed to %d under an axe", got)
	}

	stairs := withProps(t, worldgen.BlockBase("waxed_oxidized_cut_copper_stairs"),
		map[string]string{"facing": "east", "half": "top"})
	h.world.SetBlock(10, 70, 10, stairs)
	h.onUseAxe(players, evUseAxe{eid: 1, x: 10, y: 70, z: 10, slot: 0})
	want := withProps(t, worldgen.BlockBase("oxidized_cut_copper_stairs"),
		map[string]string{"facing": "east", "half": "top"})
	if got := h.world.At(10, 70, 10); got != want {
		t.Fatalf("un-waxed stairs → %d, want %d (oxidized, same orientation)", got, want)
	}
	if !pl.adv.done(advByID["minecraft:husbandry/wax_off"]) {
		t.Error("wax_off advancement should be granted by un-waxing copper")
	}
}

// Honeycomb waxes copper (keeping stage and orientation), uses up one comb in
// survival and grants wax_on.
func TestHoneycombWaxesCopper(t *testing.T) {
	h, pl, players := axeHub(t)
	give(pl, itemHoneycomb)
	pl.inv.slots[0].count = 3
	slab := withProps(t, worldgen.BlockBase("exposed_cut_copper_slab"), map[string]string{"type": "top"})
	h.world.SetBlock(10, 70, 10, slab)

	h.onUseHoneycomb(players, evUseHoneycomb{eid: 1, x: 10, y: 70, z: 10, slot: 0})

	want := withProps(t, worldgen.BlockBase("waxed_exposed_cut_copper_slab"), map[string]string{"type": "top"})
	if got := h.world.At(10, 70, 10); got != want {
		t.Fatalf("waxed slab → %d, want %d", got, want)
	}
	if pl.inv.slots[0].count != 2 {
		t.Errorf("honeycomb count %d, want 2 (one used)", pl.inv.slots[0].count)
	}
	if !pl.adv.done(advByID["minecraft:husbandry/wax_on"]) {
		t.Error("wax_on advancement should be granted by waxing copper")
	}
	// Waxing again does nothing (already waxed, not in WAXABLES).
	h.onUseHoneycomb(players, evUseHoneycomb{eid: 1, x: 10, y: 70, z: 10, slot: 0})
	if pl.inv.slots[0].count != 2 {
		t.Error("a second comb was used on already-waxed copper")
	}
}

// A shield in the offhand means the player wants to block: the axe does not
// strip unless they sneak (AxeItem.playerHasBlockingItemUseIntent).
func TestAxeYieldsToShieldIntent(t *testing.T) {
	h, pl, players := axeHub(t)
	give(pl, itemByName["iron_axe"])
	pl.offhand = invStack{item: itemShield, count: 1}
	log := worldgen.BlockBase("oak_log")
	h.world.SetBlock(10, 70, 10, log)

	h.onUseAxe(players, evUseAxe{eid: 1, x: 10, y: 70, z: 10, slot: 0})
	if h.world.At(10, 70, 10) != log {
		t.Fatal("axe stripped while a shield was raised in the offhand")
	}
	pl.p.sneaking = true
	h.onUseAxe(players, evUseAxe{eid: 1, x: 10, y: 70, z: 10, slot: 0})
	if h.world.At(10, 70, 10) == log {
		t.Fatal("sneaking should let the axe strip past the shield")
	}
}

// Waxing one half of a double copper chest carries the other half along
// (CopperChestBlock.updateShape adopts the partner's block), each keeping its
// own facing/type.
func TestWaxingCopperChestFollowsPartner(t *testing.T) {
	h, pl, players := axeHub(t)
	give(pl, itemHoneycomb)
	left := withProps(t, worldgen.BlockBase("copper_chest"), map[string]string{"facing": "north", "type": "left"})
	right := withProps(t, worldgen.BlockBase("copper_chest"), map[string]string{"facing": "north", "type": "right"})
	h.world.SetBlock(10, 70, 10, left)
	h.world.SetBlock(11, 70, 10, right)
	if _, _, paired := h.chestPairPositions(0, 10, 70, 10, left); !paired {
		t.Fatal("test setup: the two halves must pair")
	}

	h.onUseHoneycomb(players, evUseHoneycomb{eid: 1, x: 10, y: 70, z: 10, slot: 0})

	wl := withProps(t, worldgen.BlockBase("waxed_copper_chest"), map[string]string{"facing": "north", "type": "left"})
	wr := withProps(t, worldgen.BlockBase("waxed_copper_chest"), map[string]string{"facing": "north", "type": "right"})
	if got := h.world.At(10, 70, 10); got != wl {
		t.Errorf("clicked half → %d, want waxed left %d", got, wl)
	}
	if got := h.world.At(11, 70, 10); got != wr {
		t.Errorf("partner half → %d, want waxed right %d", got, wr)
	}
	// And the right half never ages on its own: only the left ticks.
	h.world.SetBlock(10, 70, 10, left)
	h.world.SetBlock(11, 70, 10, right)
	for i := 0; i < 3000; i++ {
		h.tickCopper(players, 0, 11, 70, 10, h.world.At(11, 70, 10))
	}
	if h.world.At(11, 70, 10) != right {
		t.Error("the right half of a copper chest oxidized on its own tick")
	}
}
