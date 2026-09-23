package server

import (
	"math/rand"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The item-shaping chest functions: potions, names, horns, stews and
// ominous levels come out of the table, not generic.
func TestChestFunctionsShapeItems(t *testing.T) {
	h := newHub(world.New(1))
	r := rand.New(rand.NewSource(7))
	ctx := &lootCtx{rng: r.Intn, randf: r.Float64}

	st := ctx.applyChestFn(h, &lootFn{F: "set_potion", Potion: "strong_regeneration"}, invStack{item: itemPotion, count: 1})
	if st.potion != potStrongRegen {
		t.Fatalf("set_potion strong_regeneration: potion %d", st.potion)
	}
	st = ctx.applyChestFn(h, &lootFn{F: "set_potion", Potion: "weaving"}, invStack{item: itemArrow, count: 3})
	if st.item != itemTippedArrow || st.potion != potWeaving {
		t.Fatalf("an arrow with potion contents is a tipped arrow: item %d potion %d", st.item, st.potion)
	}
	st = ctx.applyChestFn(h, &lootFn{F: "set_name", Name: "Buried Treasure Map"}, invStack{item: itemFilledMap, count: 1})
	if st.name != "Buried Treasure Map" {
		t.Fatalf("set_name: %q", st.name)
	}
	for i := 0; i < 20; i++ {
		st = ctx.applyChestFn(h, &lootFn{F: "set_instrument", Options: "regular"}, invStack{item: itemGoatHorn, count: 1})
		if st.instrument < 0 || st.instrument > 3 {
			t.Fatalf("a regular goat horn plays ponder/sing/seek/feel, got %d", st.instrument)
		}
		st = ctx.applyChestFn(h, &lootFn{F: "set_instrument", Options: "screaming"}, invStack{item: itemGoatHorn, count: 1})
		if st.instrument < 4 || st.instrument > 7 {
			t.Fatalf("a screaming goat horn plays admire/call/yearn/dream, got %d", st.instrument)
		}
	}
	st = ctx.applyChestFn(h, &lootFn{F: "set_stew", Effects: []string{"night_vision"}}, invStack{item: itemSuspiciousStew, count: 1})
	if st.stew == 0 || stewEffects[st.stew-1].effect != effNightVision {
		t.Fatalf("set_stew night_vision: stew %d", st.stew)
	}
	three := 3.0
	st = ctx.applyChestFn(h, &lootFn{F: "set_ominous", NP: &lootNP{T: "const", V: three}}, invStack{item: itemByName["ominous_bottle"], count: 1})
	if st.potion != 4 {
		t.Fatalf("an ominous bottle at amplifier 3 carries level 4, got %d", st.potion)
	}
}

// A shipwreck's map chest hands out a real treasure map: a filled map
// centred near the nearest buried treasure with a red cross on it.
func TestShipwreckMapLeadsToBuriedTreasure(t *testing.T) {
	h := newHub(world.New(1))
	h.maps = newMapStore("")
	tx, tz, ok := h.world.Gen().LocateStructure("buried_treasure", 0, 0, 20000)
	if !ok {
		t.Skip("no buried treasure within reach of the origin on this seed")
	}
	tbl, ok := lootForChest("chests/shipwreck_map")
	if !ok {
		t.Fatal("chests/shipwreck_map table missing")
	}
	hasFn := false
	for _, p := range tbl.Pools {
		for _, e := range p.Entries {
			for _, f := range e.Functions {
				if f.F == "exploration_map" && f.Dest == "buried_treasure" {
					hasFn = true
				}
			}
		}
	}
	if !hasFn {
		t.Fatal("the baked shipwreck_map table lost its exploration_map function")
	}
	var slots [27]invStack
	h.fillSlots(slots[:], "chests/shipwreck_map", blockPos{tx + 40, 40, tz - 40})
	var got invStack
	for _, st := range slots {
		if isMapItem(st.item) {
			got = st
		}
	}
	// 26.3: the treasure map is its own item, named by the item.
	if got.item != int32(itemByName["buried_treasure_map"]) || got.mapID == 0 {
		t.Fatalf("the shipwreck map chest should hold a buried treasure map, slots %v", slots)
	}
	if got.name != "" {
		t.Fatalf("the map carries a custom name %q; the item names itself", got.name)
	}
	md := h.maps.get(got.mapID)
	if md == nil || len(md.Marks) != 1 || md.Marks[0].Type != decorRedX || md.Marks[0].X != int32(tx) || md.Marks[0].Z != int32(tz) {
		t.Fatalf("the map should carry one red cross at the treasure (%d,%d): %+v", tx, tz, md)
	}
	if decs := mapMarkDecorations(md); len(decs) != 1 || decs[0].Type != decorRedX {
		t.Fatalf("the cross should render on the map (centre %d,%d scale %d): %v", md.CenterX, md.CenterZ, md.Scale, decs)
	}
}

// An explorer map is a map: it clones as itself, takes a cartography lock,
// but cannot be extended (only the filled map is #extendable_maps).
func TestExplorerMapsAreMaps(t *testing.T) {
	em := int32(itemByName["ocean_monument_map"])
	if !isMapItem(em) || !isMapItem(itemFilledMap) || isMapItem(itemEmptyMap) {
		t.Fatal("isMapItem must be #clonable_maps")
	}
	grid := make([]invStack, 9)
	grid[0] = invStack{item: em, count: 1, mapID: 7}
	grid[1] = invStack{item: itemEmptyMap, count: 1}
	if res, kind := mapCraftMatch(grid, 3); kind != mapCraftClone || res.item != em || res.count != 2 || res.mapID != 7 {
		t.Errorf("cloning an explorer map gave %+v kind %d, want two of the same explorer map", res, kind)
	}
	grid = make([]invStack, 9)
	for i := range grid {
		grid[i] = invStack{item: itemPaper, count: 1}
	}
	grid[4] = invStack{item: em, count: 1, mapID: 7}
	if _, kind := mapCraftMatch(grid, 3); kind == mapCraftZoom {
		t.Error("an explorer map must not extend")
	}
}
