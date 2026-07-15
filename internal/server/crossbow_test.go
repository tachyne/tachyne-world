package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// xbowSetup arms a survival player with a plain crossbow and a stack of arrows,
// standing high in the air with a clear flight path (mirrors bowSetup).
func xbowSetup() (*hub, *tracked, map[int32]*tracked) {
	h := newHub(world.New(1))
	pl := testTracked()
	pl.x, pl.y, pl.z = 0.5, 80, 0.5
	pl.yaw, pl.pitch = 0, 0 // facing +z, level
	pl.p.setHotbarSlot(0, itemCrossbow)
	pl.inv.slots[0] = invStack{item: itemCrossbow, count: 1}
	pl.inv.slots[1] = invStack{item: itemArrowAmmo, count: 5}
	h.tick.Store(100)
	return h, pl, map[int32]*tracked{1: pl}
}

func TestCrossbowChargeLoadFire(t *testing.T) {
	h, pl, players := xbowSetup()

	// A first use begins charging (no bolt yet, no ammo spent).
	h.useXbow(players, pl)
	if pl.xbowAt == 0 {
		t.Fatal("a first use should begin charging the crossbow")
	}
	if len(h.arrows) != 0 || pl.inv.slots[1].count != 5 {
		t.Fatal("charging must not fire or consume ammo")
	}

	// A full charge, released, latches the loaded shot and spends one arrow.
	h.tick.Add(xbowBaseCharge)
	h.finishXbowCharge(players, pl)
	if !pl.xbowLoaded {
		t.Fatal("a full charge should latch the crossbow loaded")
	}
	if pl.inv.slots[1].count != 4 {
		t.Fatalf("loading consumes one arrow, left %d", pl.inv.slots[1].count)
	}
	if len(h.arrows) != 0 {
		t.Fatal("loading must not yet fire a bolt")
	}

	// The next use fires the loaded bolt.
	h.useXbow(players, pl)
	if pl.xbowLoaded {
		t.Fatal("firing should clear the loaded shot")
	}
	if len(h.arrows) != 1 {
		t.Fatalf("a loaded crossbow use should loose one bolt, got %d", len(h.arrows))
	}
	for _, a := range h.arrows {
		if !a.playerShot || a.dmg != xbowDamage {
			t.Fatalf("crossbow bolt should be player-shot at %d dmg: %+v", xbowDamage, a)
		}
	}
}

func TestCrossbowEarlyReleaseFizzles(t *testing.T) {
	h, pl, players := xbowSetup()
	h.startXbowCharge(pl)
	h.tick.Add(3) // released well before the crossbow finishes charging
	h.finishXbowCharge(players, pl)
	if pl.xbowLoaded {
		t.Fatal("an early release must not load the crossbow")
	}
	if pl.inv.slots[1].count != 5 {
		t.Fatal("a fizzled charge consumes no arrow")
	}
}

func TestCrossbowQuickCharge(t *testing.T) {
	h, pl, players := xbowSetup()
	// quick_charge III → 25 − 15 = 10 ticks to full charge.
	pl.inv.slots[0] = invStack{item: itemCrossbow, count: 1,
		ench: [2]enchApply{{id: enchQuickCharge, lvl: 3}}}

	if got := xbowChargeTicks(pl); got != 10 {
		t.Fatalf("quick_charge III should charge in 10 ticks, got %d", got)
	}
	h.startXbowCharge(pl)
	h.tick.Add(10)
	h.finishXbowCharge(players, pl)
	if !pl.xbowLoaded {
		t.Fatal("quick_charge III should be fully charged after 10 ticks")
	}
}

func TestCrossbowMultishot(t *testing.T) {
	h, pl, players := xbowSetup()
	pl.xbowLoaded, pl.xbowMulti = true, true // a multishot-loaded crossbow
	h.fireXbow(players, pl)
	if len(h.arrows) != 3 {
		t.Fatalf("multishot should loose three bolts, got %d", len(h.arrows))
	}
	pickable := 0
	for _, a := range h.arrows {
		if !a.noPickup {
			pickable++
		}
	}
	if pickable != 1 {
		t.Fatalf("only the centre multishot bolt is retrievable, got %d", pickable)
	}
}

func TestCrossbowPiercingPassesThrough(t *testing.T) {
	h, pl, players := xbowSetup()
	a := h.launchProjectileIn(players, entityArrow, 0, 0.5, 81, 0.5, 0, 0, xbowSpeed)
	a.shooter, a.dmg, a.playerShot, a.pierce = pl.p.eid, xbowDamage, true, 1
	a.hitMobs = map[int32]bool{}

	first := &mob{eid: 9, etype: entityZombie, hostile: true, health: 100, x: 0.5, y: 80, z: 4.5}
	second := &mob{eid: 10, etype: entityZombie, hostile: true, health: 100, x: 0.5, y: 80, z: 20.5}
	h.mobs[9], h.mobs[10] = first, second

	// The first mob is struck but the bolt passes through (pierce 1 → 0).
	if stop := h.arrowHitsMob(players, a, first.x, 80.5, first.z); stop {
		t.Fatal("a piercing bolt should pass through the first mob, not stop")
	}
	if !first.hitByPlayer || a.pierce != 0 {
		t.Fatalf("first mob should be hit and a pierce spent: hit=%v pierce=%d", first.hitByPlayer, a.pierce)
	}

	// Re-testing the same point must not damage the already-struck mob again.
	firstHP := first.health
	h.arrowHitsMob(players, a, first.x, 80.5, first.z)
	if first.health != firstHP {
		t.Fatal("a piercing bolt must never strike the same mob twice")
	}

	// With pierces exhausted, the second mob stops the bolt.
	if stop := h.arrowHitsMob(players, a, second.x, 80.5, second.z); !stop {
		t.Fatal("with no pierces left the bolt should stop on the second mob")
	}
	if !second.hitByPlayer {
		t.Fatal("the second mob should have been struck")
	}
}
