package server

import "testing"

func TestTargetBlock(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world

	// Strength scales from the rim (1) to dead centre (15).
	if s := targetStrength(5.5, 70.5, 5.5); s != 15 {
		t.Errorf("centre hit strength %d, want 15", s)
	}
	if s := targetStrength(5.95, 70.95, 5.95); s != 1 {
		t.Errorf("corner hit strength %d, want 1", s)
	}

	onHub(t, h, func() {
		pos := blockPos{5, 70, 5}
		w.SetBlock(pos.x, pos.y, pos.z, targetMin) // power 0
		h.tick.Store(1000)

		// A centre strike energises it to 15 and it emits to every neighbour.
		h.hitTarget(h.playersRef, pos, targetMin, 5.5, 70.5, 5.5, true)
		if targetPower(w.At(pos.x, pos.y, pos.z)) != 15 {
			t.Fatalf("target power %d after centre hit, want 15", targetPower(w.At(pos.x, pos.y, pos.z)))
		}
		if p := h.emitPower(pos.x, pos.y, pos.z, pos.x+1, pos.y, pos.z); p != 15 {
			t.Errorf("target emits %d to its neighbour, want 15", p)
		}

		// Holds before the 20-tick deadline, decays to 0 after it.
		h.tick.Store(1015)
		h.updateTarget(h.playersRef, pos, w.At(pos.x, pos.y, pos.z))
		if targetPower(w.At(pos.x, pos.y, pos.z)) != 15 {
			t.Error("target decayed early")
		}
		h.tick.Store(1021)
		h.updateTarget(h.playersRef, pos, w.At(pos.x, pos.y, pos.z))
		if targetPower(w.At(pos.x, pos.y, pos.z)) != 0 {
			t.Errorf("target power %d after hold, want 0", targetPower(w.At(pos.x, pos.y, pos.z)))
		}
	})
}
