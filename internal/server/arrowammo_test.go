package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// The off hand is drawn from first, then the inventory in order, and any
// arrow counts: plain, tipped or spectral.
func TestArrowAmmoOrder(t *testing.T) {
	pl := testTracked()
	pl.inv.slots[5] = invStack{item: itemArrowAmmo, count: 3}
	pl.inv.slots[2] = invStack{item: itemSpectralArr, count: 1}
	if ammoSlot(pl) != 2 {
		t.Fatalf("first arrow in slot order: got %d, want 2", ammoSlot(pl))
	}
	pl.offhand = invStack{item: itemTippedArrow, count: 1, potion: potPoison}
	if ammoSlot(pl) != offhandSlot {
		t.Fatal("an arrow in the off hand is drawn first")
	}
	if got := peekAmmo(pl); got.item != itemTippedArrow || got.potion != potPoison {
		t.Fatalf("peekAmmo %+v", got)
	}
}

// A bow shoots a spectral arrow as one, Infinity does not spare it, and the
// arrow comes back as itself; what it hits glows.
func TestSpectralArrowFromABow(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.x, pl.y, pl.z = 0.5, 200, 0.5
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemBow, count: 1, ench: enchList{{id: enchInfinity, lvl: 1}}}
	pl.inv.slots[9] = invStack{item: itemSpectralArr, count: 2}
	h.tick.Add(1) // drawingAt 0 means "not drawing"
	pl.drawingAt = h.tick.Load()
	h.tick.Add(40)
	h.releaseDraw(players, pl)
	var a *arrowEntity
	for _, x := range h.arrows {
		a = x
	}
	if a == nil || a.etype != entitySpectralArrow || a.glow == 0 || a.pickupStack.item != itemSpectralArr {
		t.Fatalf("shot %+v, want a glowing spectral arrow that returns as itself", a)
	}
	if pl.inv.slots[9].count != 1 {
		t.Errorf("Infinity spared a spectral arrow: %d left", pl.inv.slots[9].count)
	}
	m := h.spawnMob(players, entityZombie, 5, 200, 0.5)
	h.arrowEffectsOnMob(players, a, m)
	if m.hasEffect(effGlowing) == 0 {
		t.Error("the spectral arrow's target does not glow")
	}
}
