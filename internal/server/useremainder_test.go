package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Item.Properties.usingConvertsTo: a stew leaves its bowl behind and a honey
// bottle its glass. Both simply vanished, which is a quiet tax on every bowl
// a player owns.
func TestEatingReturnsTheBowl(t *testing.T) {
	h := newHub(world.New(97))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.food = 0

	for _, tc := range []struct{ food, left string }{
		{"mushroom_stew", "bowl"},
		{"rabbit_stew", "bowl"},
		{"beetroot_soup", "bowl"},
		{"suspicious_stew", "bowl"},
		{"honey_bottle", "glass_bottle"},
	} {
		pl.food, pl.saturation = 0, 0
		pl.inv.slots[0] = invStack{item: itemByName[tc.food], count: 1}
		h.eat(players, pl, 0)
		if got := pl.inv.slots[0].item; got != int32(itemByName[tc.left]) {
			t.Errorf("eating %s left item %d in the slot, want a %s", tc.food, got, tc.left)
		}
	}

	// A food with no remainder leaves the slot empty.
	pl.food = 0
	pl.inv.slots[0] = invStack{item: itemByName["bread"], count: 1}
	h.eat(players, pl, 0)
	if pl.inv.slots[0].item != 0 {
		t.Errorf("bread left %d behind, want nothing", pl.inv.slots[0].item)
	}

	// Eating one of a stack puts the bowl elsewhere rather than over the rest.
	pl.food = 0
	pl.inv.slots[0] = invStack{item: itemByName["honey_bottle"], count: 3}
	h.eat(players, pl, 0)
	if pl.inv.slots[0].count != 2 || pl.inv.slots[0].item != int32(itemByName["honey_bottle"]) {
		t.Fatalf("the rest of the stack was clobbered: %+v", pl.inv.slots[0])
	}
	found := false
	for _, s := range pl.inv.slots {
		if s.item == int32(itemByName["glass_bottle"]) && s.count > 0 {
			found = true
		}
	}
	if !found {
		t.Error("the empty bottle went nowhere")
	}
}

// DyeItem.interactLivingEntity refuses a SHEARED sheep — there is no wool on
// it to take the colour — where the engine spent the dye anyway.
func TestDyeRefusesAShearedSheep(t *testing.T) {
	h := newHub(world.New(101))
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entitySheep, 0, 70, 0)
	m.color = 0 // DyeColor.WHITE

	blue := int32(itemByName["blue_dye"])
	if !h.dyeSheep(players, m, blue) {
		t.Fatal("a woolly sheep takes the dye")
	}
	m.sheared = true
	if h.dyeSheep(players, m, int32(itemByName["red_dye"])) {
		t.Error("a sheared sheep has no wool to dye")
	}
}

// IceBlock.playerDestroy: ice mined without Silk Touch leaves water.
func TestMinedIceLeavesWater(t *testing.T) {
	w := world.New(103)
	h := newHub(w)
	players := map[int32]*tracked{}
	pos := blockPos{4, 70, 4}
	w.SetBlock(pos.x, pos.y, pos.z, worldgen.Air)
	h.iceMeltsOnBreak(players, 0, pos, iceBlock)
	if got := w.At(pos.x, pos.y, pos.z); got != worldgen.WaterBase {
		t.Errorf("mined ice left %d, want water", got)
	}
	// Packed ice does not melt, and neither does ice in the Nether.
	w.SetBlock(pos.x, pos.y, pos.z, worldgen.Air)
	h.iceMeltsOnBreak(players, 0, pos, worldgen.BlockBase("packed_ice"))
	if w.At(pos.x, pos.y, pos.z) == worldgen.WaterBase {
		t.Error("packed ice must not melt")
	}
}
