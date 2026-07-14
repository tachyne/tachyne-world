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
	})
}
