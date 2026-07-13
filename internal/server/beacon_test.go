package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func TestBeaconValidEffects(t *testing.T) {
	speed, haste, str, regen := int32(effSpeed+1), int32(effHaste+1), int32(effStrength+1), int32(effRegen+1)
	cases := []struct {
		levels             int
		primary, secondary int32
		want               bool
	}{
		{1, speed, 0, true},
		{1, haste, 0, true},
		{1, str, 0, false},                  // strength needs tier 3
		{3, str, 0, true},                   //
		{1, speed, regen, false},            // secondary needs tier 4
		{4, speed, regen, true},             //
		{4, speed, speed, true},             // the double-up
		{4, speed, haste, false},            // distinct non-regen secondary
		{4, regen, 0, false},                // the tier-4 power can't be primary
		{4, 0, 0, true},                     // clearing the selection
		{2, int32(effPoison) + 1, 0, false}, // not a beacon power
	}
	for _, c := range cases {
		if got := beaconValidEffects(c.levels, c.primary, c.secondary); got != c.want {
			t.Errorf("levels=%d primary=%d secondary=%d: got %v want %v",
				c.levels, c.primary, c.secondary, got, c.want)
		}
	}
}

// The full flow on the hub: pyramid tiers, menu open, payment + power
// selection, the 80-tick effect application, and break cleanup.
func TestBeaconFlow(t *testing.T) {
	_, h, p := breakPlaceServer(t)
	w := h.world

	onHub(t, h, func() {
		tr := h.playersRef[p.eid]
		bx, bz := int(tr.x), int(tr.z)
		by := int(tr.y) + 2
		iron := worldgen.BlockBase("iron_block")

		// A single-layer 3×3 pyramid under the beacon.
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				w.SetBlock(bx+dx, by-1, bz+dz, iron)
			}
		}
		h.beaconsOnBlockChange(h.playersRef, bx, by, bz, beaconState)
		w.SetBlock(bx, by, bz, beaconState)
		if h.beacons[blockPos{bx, by, bz}] == nil {
			t.Error("beacon not registered on place")
			return
		}
		if got := beaconLevels(w, bx, by, bz); got != 1 {
			t.Errorf("pyramid levels = %d, want 1", got)
		}
		if !beaconSkyOpen(w, bx, by, bz) {
			t.Error("open column should reach the sky")
		}

		// The 80-tick scan activates it and fires create_beacon range checks.
		h.beaconTick(h.playersRef)
		b := h.beacons[blockPos{bx, by, bz}]
		if b.levels != 1 {
			t.Errorf("scanned levels = %d, want 1", b.levels)
		}

		// Open the menu, pay an ingot, choose speed.
		h.openBeacon(tr, bx, by, bz)
		if tr.winID == 0 || tr.winKind != winBeacon {
			t.Errorf("window not open: id=%d kind=%d", tr.winID, tr.winKind)
			return
		}
		tr.anvil[0] = invStack{item: int32(itemByName["iron_ingot"]), count: 1}
		h.onSetBeacon(h.playersRef, tr, int32(effSpeed)+1, 0)
		if b.primary != int32(effSpeed)+1 {
			t.Errorf("primary = %d", b.primary)
		}
		if tr.anvil[0].item != 0 {
			t.Error("payment not consumed")
		}

		// Strength needs tier 3 — refused on this tier-1 beacon, payment kept.
		tr.anvil[0] = invStack{item: int32(itemByName["diamond"]), count: 1}
		h.onSetBeacon(h.playersRef, tr, int32(effStrength)+1, 0)
		if b.primary != int32(effSpeed)+1 || tr.anvil[0].item == 0 {
			t.Errorf("invalid choice applied: primary=%d pay=%+v", b.primary, tr.anvil[0])
		}

		// The next scan applies speed to the in-range player.
		h.beaconTick(h.playersRef)
		if tr.hasEffect(effSpeed) != 1 {
			t.Errorf("speed level = %d, want 1 (amp 0)", tr.hasEffect(effSpeed))
		}

		// A roof over the beacon kills the beam.
		w.SetBlock(bx, by+3, bz, 1) // stone
		h.beaconTick(h.playersRef)
		if b.levels != 0 {
			t.Errorf("roofed beacon levels = %d, want 0", b.levels)
		}
		w.SetBlock(bx, by+3, bz, 0)

		// Glass does not.
		w.SetBlock(bx, by+3, bz, worldgen.BlockBase("glass"))
		h.beaconTick(h.playersRef)
		if b.levels != 1 {
			t.Errorf("glass-roofed beacon levels = %d, want 1", b.levels)
		}

		// Breaking the beacon unregisters it.
		h.beaconsOnBlockChange(h.playersRef, bx, by, bz, 0)
		if h.beacons[blockPos{bx, by, bz}] != nil {
			t.Error("beacon not removed on break")
		}
	})
}
