package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestCookerTables(t *testing.T) {
	beef := int32(itemByName["beef"])
	ironOre := int32(itemByName["iron_ore"])

	// The smoker cooks food at 100 ticks, refuses ore.
	if e, ok := cookerRecipe(cookSmoker, beef); !ok || e.Out != int32(itemByName["cooked_beef"]) || e.Cook != 100 {
		t.Errorf("smoker beef: %+v %v", e, ok)
	}
	if _, ok := cookerRecipe(cookSmoker, ironOre); ok {
		t.Error("smoker must refuse iron ore")
	}
	// The blast furnace smelts ore at 100 ticks, refuses food.
	if e, ok := cookerRecipe(cookBlast, ironOre); !ok || e.Out != int32(itemByName["iron_ingot"]) || e.Cook != 100 {
		t.Errorf("blast iron: %+v %v", e, ok)
	}
	if _, ok := cookerRecipe(cookBlast, beef); ok {
		t.Error("blast furnace must refuse beef")
	}
	// The plain furnace does both, at 200.
	if e, ok := cookerRecipe(cookFurnace, beef); !ok || e.Cook != 200 {
		t.Errorf("furnace beef: %+v %v", e, ok)
	}
	// The specialists burn fuel at double rate.
	coal := int32(itemByName["coal"])
	if cookerFuelTicks(cookFurnace, coal) != 1600 || cookerFuelTicks(cookBlast, coal) != 800 {
		t.Errorf("fuel: furnace %d blast %d", cookerFuelTicks(cookFurnace, coal), cookerFuelTicks(cookBlast, coal))
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
		cf := h.campfires[blockPos{bx, by, bz}]
		if cf == nil || cf.items[0] != beef || cf.total[0] != 600 {
			t.Errorf("campfire after add: %+v", cf)
			return
		}
		if got := tr.inv.slots[tr.p.heldSlot()].count; got != 1 {
			t.Errorf("held count %d, want 1 after consuming", got)
		}
		if ci, ok := h.cfStore.get(bx, by, bz); !ok || ci.Items[0] != "minecraft:beef" {
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
		h.spillCampfire(h.playersRef, bx, by, bz, 0)
		if h.campfires[blockPos{bx, by, bz}] != nil {
			t.Error("campfire not removed on break")
		}
		if _, ok := h.cfStore.get(bx, by, bz); ok {
			t.Error("store entry not removed")
		}
	})
}
