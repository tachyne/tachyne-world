package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// windowTitle is the title of the last window opened for p.
func windowTitle(p *player) string {
	title := ""
	for _, ev := range drainEvs(p) {
		if w, ok := ev.(attachproto.WindowOpen); ok {
			title = w.Title
		}
	}
	return title
}

// A chest placed from a renamed item carries the name: it is the menu's
// title, and breaking the chest — by hand or in a blast — drops it named.
func TestNamedChestKeepsItsName(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	chestState := worldgen.BlockID("chest")
	pos := blockPos{0, 100, 0}
	pl.x, pl.y, pl.z = 0.5, 100, 2.5

	// Place it the way the session does: the block is written, then the hub
	// hears of it while the named stack is still in hand.
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["chest"], count: 1, name: "Loot"}
	h.world.SetBlock(pos.x, pos.y, pos.z, chestState)
	h.onBlock(players, evBlock{x: pos.x, y: pos.y, z: pos.z, state: chestState, by: pl.p.eid, placed: true})
	drainEvs(pl.p)
	h.openChest(pl, pos.x, pos.y, pos.z)
	if got := windowTitle(pl.p); got != "Loot" {
		t.Fatalf("a named chest's menu title is its name: got %q", got)
	}
	h.releaseContainerView(pl)

	// Broken by hand: the removal, then the drop.
	h.world.SetBlock(pos.x, pos.y, pos.z, worldgen.Air)
	h.onBlock(players, evBlock{x: pos.x, y: pos.y, z: pos.z, state: worldgen.Air, by: pl.p.eid, broken: chestState})
	h.dropLoose(players, 0, pos, chestState)
	named := 0
	for eid, it := range h.items {
		if it.item == itemByName["chest"] {
			if it.name != "Loot" {
				t.Fatalf("the dropped chest should carry its name, got %q", it.name)
			}
			named++
			delete(h.items, eid)
		}
	}
	if named != 1 {
		t.Fatalf("want one named chest dropped, got %d", named)
	}
	if h.blockNames.get(simPos{blockPos: pos}) != "" {
		t.Fatal("the name must leave with the block")
	}

	// An unnamed chest put back in the same place is plain again.
	h.world.SetBlock(pos.x, pos.y, pos.z, chestState)
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["chest"], count: 1}
	h.onBlock(players, evBlock{x: pos.x, y: pos.y, z: pos.z, state: chestState, by: pl.p.eid, placed: true})
	drainEvs(pl.p)
	h.openChest(pl, pos.x, pos.y, pos.z)
	if got := windowTitle(pl.p); got != "Chest" {
		t.Fatalf("an unnamed chest is titled Chest, got %q", got)
	}
	h.releaseContainerView(pl)

	// A named furnace blown up drops named (copy_components on any drop).
	fpos := blockPos{3, 100, 0}
	furnace := worldgen.BlockID("furnace")
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["furnace"], count: 1, name: "Oven"}
	h.world.SetBlock(fpos.x, fpos.y, fpos.z, furnace)
	h.onBlock(players, evBlock{x: fpos.x, y: fpos.y, z: fpos.z, state: furnace, by: pl.p.eid, placed: true})
	h.setBlockAt(players, 0, fpos, worldgen.Air)
	h.dropExploded(players, 0, fpos, furnace, 4, blastTNT)
	for _, it := range h.items {
		if it.item == itemByName["furnace"] && it.name != "Oven" {
			t.Fatalf("a blasted furnace should drop named, got %q", it.name)
		}
	}
}

// Anything that is not a named-block-entity block ignores the name.
func TestNameOnPlainBlockIsDropped(t *testing.T) {
	if nameableBlock(worldgen.BlockID("stone")) || nameableBlock(worldgen.BlockID("piston_head")) {
		t.Fatal("stone and piston heads are not nameable")
	}
	for _, n := range []string{"chest", "red_shulker_box", "white_wall_banner", "zombie_wall_head", "waxed_oxidized_copper_chest", "beacon"} {
		if !nameableBlock(worldgen.BlockID(n)) {
			t.Fatalf("%s should keep a custom name", n)
		}
	}
}
