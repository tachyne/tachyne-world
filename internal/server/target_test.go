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
		h.hitTarget(h.playersRef, dimOverworld, pos, targetMin, 5.5, 70.5, 5.5, true, nil)
		if targetPower(w.At(pos.x, pos.y, pos.z)) != 15 {
			t.Fatalf("target power %d after centre hit, want 15", targetPower(w.At(pos.x, pos.y, pos.z)))
		}
		if p := h.emitPower(pos.x, pos.y, pos.z, pos.x+1, pos.y, pos.z); p != 15 {
			t.Errorf("target emits %d to its neighbour, want 15", p)
		}

		// A second hit while the reset is pending changes nothing: the
		// signal and its reset stay as the first hit set them.
		due, ok := h.blockTickDue(dimOverworld, pos, targetMin)
		if !ok || due != 1020 {
			t.Fatalf("reset due %d (pending %v), want 1020", due, ok)
		}
		h.tick.Store(1010)
		h.hitTarget(h.playersRef, dimOverworld, pos, w.At(pos.x, pos.y, pos.z), 5.95, 70.95, 5.95, false, nil)
		if targetPower(w.At(pos.x, pos.y, pos.z)) != 15 {
			t.Errorf("a hit during the hold re-powered the target to %d", targetPower(w.At(pos.x, pos.y, pos.z)))
		}
		if again, _ := h.blockTickDue(dimOverworld, pos, targetMin); again != due {
			t.Errorf("a hit during the hold moved the reset to %d", again)
		}
		// A neighbour's change does nothing; the reset tick drops it to 0.
		h.processUpdate(h.playersRef, dimOverworld, pos)
		if targetPower(w.At(pos.x, pos.y, pos.z)) != 15 {
			t.Error("target decayed early")
		}
		w.ForceLoad(pos.x, pos.z, 2)
		h.tick.Store(1019)
		stepTicks(h, h.playersRef, 1)
		if targetPower(w.At(pos.x, pos.y, pos.z)) != 0 {
			t.Errorf("target power %d after hold, want 0", targetPower(w.At(pos.x, pos.y, pos.z)))
		}
	})
}
