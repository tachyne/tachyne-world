package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// eastDispenser builds a dispenser state facing east (+X).
func eastDispenser(t *testing.T) uint32 {
	t.Helper()
	info, ok := worldgen.InfoForState(dispenserMin)
	if !ok || !info.HasProperty("facing") {
		t.Fatalf("dispenser has no facing info: %+v", info.Props)
	}
	return worldgen.SetProperty(info, dispenserMin, "facing", "east")
}

func TestDispenserBehaviors(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	state := eastDispenser(t)
	pos := blockPos{5, 70, 5}
	front := blockPos{6, 70, 5} // +X, east of the dispenser

	fire := func(item int32, dmg int) *invStack {
		w.SetBlock(pos.x, pos.y, pos.z, state)
		b := &bin{slots: make([]invStack, 9)}
		b.slots[0] = invStack{item: item, count: 3, dmg: dmg}
		h.bins[pos] = b
		h.arrows = map[int32]*arrowEntity{} // isolate the projectile count
		h.ejectFromBin(h.playersRef, pos, state)
		return &b.slots[0]
	}

	onHub(t, h, func() {
		// Snowball / egg → a breakable projectile flies out, one consumed.
		for _, it := range []int32{int32(itemSnowball), int32(itemEgg)} {
			s := fire(it, 0)
			if len(h.arrows) != 1 {
				t.Fatalf("item %d: %d projectiles, want 1", it, len(h.arrows))
			}
			for _, a := range h.arrows {
				if !a.breaks {
					t.Errorf("item %d: projectile should be breakable", it)
				}
			}
			if s.count != 2 {
				t.Errorf("item %d: count %d, want 2 (one dispensed)", it, s.count)
			}
		}

		// Fire charge → a (non-breakable) fireball flies out.
		fire(itemFireCharge, 0)
		if len(h.arrows) != 1 {
			t.Fatalf("fire charge: %d projectiles, want 1", len(h.arrows))
		}
		for _, a := range h.arrows {
			if a.breaks {
				t.Error("fire charge projectile should not be marked breakable")
			}
		}

		// Flint and steel → lights a fire in front and wears (not consumes).
		w.SetBlock(front.x, front.y, front.z, worldgen.Air)
		s := fire(int32(itemFlintSteel), 0)
		if !isFire(w.At(front.x, front.y, front.z)) {
			t.Error("flint and steel did not light a fire ahead")
		}
		if s.count != 3 || s.dmg != 1 {
			t.Errorf("flint and steel: count %d dmg %d, want 3/1 (worn, not consumed)", s.count, s.dmg)
		}

		// Bone meal → advances a crop in front, consuming one.
		wheat := worldgen.BlockBase("wheat")
		w.SetBlock(front.x, front.y, front.z, wheat) // age 0
		s = fire(int32(itemBoneMeal), 0)
		if w.At(front.x, front.y, front.z) <= wheat {
			t.Error("bone meal did not advance the crop ahead")
		}
		if s.count != 2 {
			t.Errorf("bone meal: count %d, want 2 (one used)", s.count)
		}

		// Spawn egg → spawns its mob ahead, consuming one.
		w.SetBlock(front.x, front.y, front.z, worldgen.Air)
		mobsBefore := len(h.mobs)
		s = fire(int32(itemByName["zombie_spawn_egg"]), 0)
		if len(h.mobs) != mobsBefore+1 {
			t.Errorf("zombie spawn egg: mob count %d, want %d", len(h.mobs), mobsBefore+1)
		}
		if s.count != 2 {
			t.Errorf("spawn egg: count %d, want 2 (one used)", s.count)
		}

		// Shears → shear an adult sheep standing in the block ahead; the tool
		// wears rather than being consumed.
		sheep := h.spawnMob(h.playersRef, entitySheep, float64(front.x)+0.5, float64(front.y), float64(front.z)+0.5)
		if sheep == nil {
			t.Fatal("could not spawn test sheep")
		}
		sheep.sheared, sheep.baby = false, false
		s = fire(int32(itemShears), 0)
		if !sheep.sheared {
			t.Error("shears did not shear the sheep in front")
		}
		if s.count != 3 || s.dmg != 1 {
			t.Errorf("shears: count %d dmg %d, want 3/1 (worn, not consumed)", s.count, s.dmg)
		}

		// Honeycomb → waxes the copper block ahead, consuming one.
		copper := worldgen.BlockBase("copper_block")
		waxed, ok := waxedCopper(copper)
		if !ok {
			t.Fatal("copper_block has no waxed form")
		}
		w.SetBlock(front.x, front.y, front.z, copper)
		s = fire(int32(itemHoneycomb), 0)
		if w.At(front.x, front.y, front.z) != waxed {
			t.Errorf("honeycomb did not wax the copper ahead (got %d, want %d)", w.At(front.x, front.y, front.z), waxed)
		}
		if s.count != 2 {
			t.Errorf("honeycomb: count %d, want 2 (one used)", s.count)
		}

		// Minecart → placed on a rail ahead.
		w.SetBlock(front.x, front.y, front.z, worldgen.BlockBase("rail"))
		vBefore := len(h.vehicles)
		fire(int32(itemByName["minecart"]), 0)
		if len(h.vehicles) != vBefore+1 {
			t.Errorf("minecart: vehicles %d, want %d", len(h.vehicles), vBefore+1)
		}

		// Boat → placed on water ahead.
		w.SetBlock(front.x, front.y, front.z, worldgen.WaterBase)
		vBefore = len(h.vehicles)
		fire(int32(itemByName["oak_boat"]), 0)
		if len(h.vehicles) != vBefore+1 {
			t.Errorf("boat: vehicles %d, want %d", len(h.vehicles), vBefore+1)
		}
	})
}
