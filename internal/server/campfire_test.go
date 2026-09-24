package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestCookerTables(t *testing.T) {
	beef := int32(itemByName["beef"])
	ironOre := int32(itemByName["iron_ore"])

	coal := int32(itemByName["coal"])
	// The specialists' recipes say 200 ticks like the furnace's; their fuel
	// cooks twice as fast there (the cooking_fuel speed multiplier), so they
	// run at 100.
	fast := func(kind int8, cook int) int { return cookTotal(cook, cookerFuelSpeed(kind, coal)) }

	// The smoker cooks food at 100 ticks, refuses ore.
	if e, ok := cookerRecipe(cookSmoker, beef); !ok || e.Out != int32(itemByName["cooked_beef"]) || e.Cook != 200 || fast(cookSmoker, e.Cook) != 100 {
		t.Errorf("smoker beef: %+v %v", e, ok)
	}
	if _, ok := cookerRecipe(cookSmoker, ironOre); ok {
		t.Error("smoker must refuse iron ore")
	}
	// The blast furnace smelts ore at 100 ticks, refuses food.
	if e, ok := cookerRecipe(cookBlast, ironOre); !ok || e.Out != int32(itemByName["iron_ingot"]) || e.Cook != 200 || fast(cookBlast, e.Cook) != 100 {
		t.Errorf("blast iron: %+v %v", e, ok)
	}
	if _, ok := cookerRecipe(cookBlast, beef); ok {
		t.Error("blast furnace must refuse beef")
	}
	// The plain furnace does both, at 200.
	if e, ok := cookerRecipe(cookFurnace, beef); !ok || e.Cook != 200 || fast(cookFurnace, e.Cook) != 200 {
		t.Errorf("furnace beef: %+v %v", e, ok)
	}
	// ...and burns for half as long there, so a coal smelts 8 items in every
	// furnace (1.21.x halved getBurnDuration in the specialists too).
	for kind, want := range map[int8]int{cookFurnace: 1600, cookBlast: 800, cookSmoker: 800} {
		if got := cookerFuelTicks(kind, coal); got != want {
			t.Errorf("coal in cooker %d burns %d, want %d", kind, got, want)
		}
	}
	// Fuels the old hand-kept table never had, and the odd tick it had wrong.
	for name, want := range map[string]int{"white_wool": 100, "oak_boat": 1200, "dried_kelp_block": 4001, "white_wool_slab": 50} {
		if got := cookerFuelTicks(cookFurnace, int32(itemByName[name])); got != want {
			t.Errorf("%s burns %d, want %d", name, got, want)
		}
	}
	// Campfire recipes cook at 600.
	if e, ok := campfireResult[beef]; !ok || e.Cook != 600 {
		t.Errorf("campfire beef: %+v %v", e, ok)
	}
}

func TestCampfireFlow(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	w := h.world

	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		tr.gamemode = gmSurvival // exercise item consumption
		bx, bz := int(tr.x)+3, int(tr.z)
		by := int(tr.y)
		beef := int32(itemByName["beef"])

		w.SetBlock(bx, by, bz, campfireMin) // default state is lit
		if !boolProp(campfireMin, "lit") {
			// Default campfire state: facing north, lit, no signal fire, dry.
			// If the base isn't lit, find the lit variant for the test.
			t.Log("base state unlit; using property walk")
		}
		state := w.At(bx, by, bz)
		if !boolProp(state, "lit") {
			info, _ := worldgen.InfoForState(state)
			state = worldgen.SetProperty(info, state, "lit", "true")
			w.SetBlock(bx, by, bz, state)
		}

		tr.inv.slots[tr.p.heldSlot()] = invStack{item: beef, count: 2}
		h.onCampfireAdd(h.playersRef, evCampfireAdd{eid: p.eid, x: bx, y: by, z: bz})
		cf := h.campfires[simPos{blockPos: blockPos{bx, by, bz}}]
		if cf == nil || cf.items[0] != beef || cf.total[0] != 600 {
			t.Errorf("campfire after add: %+v", cf)
			return
		}
		if got := tr.inv.slots[tr.p.heldSlot()].count; got != 1 {
			t.Errorf("held count %d, want 1 after consuming", got)
		}
		if ci, ok := h.cfStore.get(0, bx, by, bz); !ok || ci.Items[0] != "minecraft:beef" {
			t.Errorf("store view: %+v %v", ci, ok)
		}

		// Fill the remaining three slots, then a fifth insert is refused.
		for i := 0; i < 3; i++ {
			h.onCampfireAdd(h.playersRef, evCampfireAdd{eid: p.eid, x: bx, y: by, z: bz})
			tr.inv.slots[tr.p.heldSlot()] = invStack{item: beef, count: 2}
		}
		if cf.items[3] == 0 {
			t.Error("fourth slot should be filled")
		}
		before := cf.items
		h.onCampfireAdd(h.playersRef, evCampfireAdd{eid: p.eid, x: bx, y: by, z: bz})
		if cf.items != before {
			t.Error("fifth insert must be refused")
		}

		// 600 ticks later the first item pops as cooked beef.
		cf.prog[0] = 599
		h.campfireTick(h.playersRef)
		if cf.items[0] != 0 {
			t.Errorf("slot 0 not popped: %+v", cf.items)
		}
		found := false
		for _, it := range h.items {
			if it.item == int32(itemByName["cooked_beef"]) {
				found = true
			}
		}
		if !found {
			t.Error("cooked beef not spawned")
		}

		// Breaking the fire drops the remaining raw food.
		w.SetBlock(bx, by, bz, 0)
		h.spillCampfire(h.playersRef, 0, bx, by, bz, 0)
		if h.campfires[simPos{blockPos: blockPos{bx, by, bz}}] != nil {
			t.Error("campfire not removed on break")
		}
		if _, ok := h.cfStore.get(0, bx, by, bz); ok {
			t.Error("store entry not removed")
		}
	})
}

// TestUnlitCampfireTakesFood: CampfireBlockEntity.placeFood has no lit
// check — food goes on an unlit campfire and waits there, uncooked.
func TestUnlitCampfireTakesFood(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	w := h.world
	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		bx, bz, by := int(tr.x)+3, int(tr.z), int(tr.y)
		info, _ := worldgen.InfoForState(campfireMin)
		unlit := worldgen.SetProperty(info, campfireMin, "lit", "false")
		w.SetBlock(bx, by, bz, unlit)
		beef := int32(itemByName["beef"])
		tr.inv.slots[tr.p.heldSlot()] = invStack{item: beef, count: 1}
		h.onCampfireAdd(h.playersRef, evCampfireAdd{eid: p.eid, x: bx, y: by, z: bz})
		cf := h.campfires[simPos{blockPos: blockPos{bx, by, bz}}]
		if cf == nil || cf.items[0] != beef {
			t.Errorf("an unlit campfire should take the beef: %+v", cf)
			return
		}
		for i := 0; i < 700; i++ {
			h.campfireTick(h.playersRef)
		}
		if cf.items[0] != beef {
			t.Error("an unlit campfire must not cook it")
		}
	})
}
