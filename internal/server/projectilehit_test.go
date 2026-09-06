package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func hitSetup(t *testing.T) (*hub, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pl := survPlayer(h)
	pl.x, pl.y, pl.z = 0.5, 70, 0.5
	players[pl.p.eid] = pl
	return h, players
}

// A burning projectile lights a candle; a cold one does not.
func TestBurningProjectileLightsACandle(t *testing.T) {
	h, players := hitSetup(t)
	lo, _, ok := worldgen.BlockRangeOK("candle")
	if !ok {
		t.Skip("no candle block")
	}
	info, _ := worldgen.InfoForState(lo)
	unlit := worldgen.SetProperty(info, lo, "lit", "false")
	pos := blockPos{3, 70, 0}

	// A plain arrow leaves it alone.
	h.world.SetBlock(pos.x, pos.y, pos.z, unlit)
	cold := &arrowEntity{dim: 0, x: 3.5, y: 70.5, z: 0.5}
	h.projectileHitBlock(players, cold, pos, unlit)
	if got := h.world.At(pos.x, pos.y, pos.z); worldgen.GetProperty(info, got, "lit") == "true" {
		t.Error("a cold arrow lit the candle")
	}

	// A burning one sets it going.
	burning := &arrowEntity{dim: 0, x: 3.5, y: 70.5, z: 0.5, fire: true}
	h.projectileHitBlock(players, burning, pos, unlit)
	if got := h.world.At(pos.x, pos.y, pos.z); worldgen.GetProperty(info, got, "lit") != "true" {
		t.Error("a burning arrow did not light the candle")
	}
}

// A Flame bow makes its arrows burning ones — the enchantment did nothing at
// all before, so neither the burn nor the candle rule was reachable.
func TestFlameBowLightsItsArrows(t *testing.T) {
	h, players := hitSetup(t)
	pl := players[1]
	if pl == nil {
		for _, p := range players {
			pl = p
		}
	}
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["bow"], count: 1,
		ench: enchList{{id: enchFlame, lvl: 1}}}
	pl.inv.slots[1] = invStack{item: itemArrowAmmo, count: 8}
	pl.drawingAt = 1
	h.tick.Store(40) // a full draw
	h.releaseDraw(players, pl)

	if len(h.arrows) == 0 {
		t.Fatal("the bow loosed nothing")
	}
	for _, a := range h.arrows {
		if !a.fire {
			t.Error("a Flame bow's arrow is not burning")
		}
	}
}

// Infinity spends no arrow.
func TestInfinityKeepsTheArrow(t *testing.T) {
	h, players := hitSetup(t)
	var pl *tracked
	for _, p := range players {
		pl = p
	}
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["bow"], count: 1,
		ench: enchList{{id: enchInfinity, lvl: 1}}}
	pl.inv.slots[1] = invStack{item: itemArrowAmmo, count: 8}
	pl.drawingAt = 1
	h.tick.Store(40)
	h.releaseDraw(players, pl)

	if len(h.arrows) == 0 {
		t.Fatal("the bow loosed nothing — this test would prove nothing")
	}
	if got := pl.inv.slots[1].count; got != 8 {
		t.Errorf("Infinity spent an arrow: %d left, want 8", got)
	}
	// …and without Infinity the same shot DOES cost one, so the check above
	// is measuring the enchantment rather than a dead code path.
	pl.inv.slots[pl.p.heldSlot()] = invStack{item: itemByName["bow"], count: 1}
	pl.drawingAt = 1
	h.tick.Store(80)
	h.releaseDraw(players, pl)
	if got := pl.inv.slots[1].count; got != 7 {
		t.Errorf("a plain bow left %d arrows, want 7 — the control did not fire", got)
	}
}

// A decorated pot shatters on a direct hit.
func TestProjectileShattersADecoratedPot(t *testing.T) {
	h, players := hitSetup(t)
	lo, _, ok := worldgen.BlockRangeOK("decorated_pot")
	if !ok {
		t.Skip("no decorated pot")
	}
	pos := blockPos{5, 70, 0}
	h.world.SetBlock(pos.x, pos.y, pos.z, lo)
	a := &arrowEntity{dim: 0, x: 5.5, y: 70.5, z: 0.5}
	h.projectileHitBlock(players, a, pos, lo)
	if h.world.At(pos.x, pos.y, pos.z) != worldgen.Air {
		t.Error("the pot survived a direct hit")
	}
}

// Dripstone comes off only for a fast trident — an arrow just sticks.
func TestOnlyAFastTridentShearsDripstone(t *testing.T) {
	h, players := hitSetup(t)
	lo, _, ok := worldgen.BlockRangeOK("pointed_dripstone")
	if !ok {
		t.Skip("no pointed dripstone")
	}
	pos := blockPos{7, 70, 0}

	h.world.SetBlock(pos.x, pos.y, pos.z, lo)
	arrow := &arrowEntity{dim: 0, vx: 2, x: 7.5, y: 70.5, z: 0.5}
	h.projectileHitBlock(players, arrow, pos, lo)
	if h.world.At(pos.x, pos.y, pos.z) == worldgen.Air {
		t.Error("an arrow sheared dripstone")
	}

	slow := &arrowEntity{dim: 0, vx: 0.1, x: 7.5, y: 70.5, z: 0.5,
		pickupStack: invStack{item: itemTrident, count: 1}}
	h.projectileHitBlock(players, slow, pos, lo)
	if h.world.At(pos.x, pos.y, pos.z) == worldgen.Air {
		t.Error("a barely-moving trident sheared dripstone")
	}

	fast := &arrowEntity{dim: 0, vx: 2, x: 7.5, y: 70.5, z: 0.5,
		pickupStack: invStack{item: itemTrident, count: 1}}
	h.projectileHitBlock(players, fast, pos, lo)
	if h.world.At(pos.x, pos.y, pos.z) != worldgen.Air {
		t.Error("a fast trident did not shear the dripstone")
	}
}
