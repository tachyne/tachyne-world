package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func hookFacing(t *testing.T, facing string) uint32 {
	t.Helper()
	info, _ := worldgen.InfoForState(tripwireHookMin)
	return worldgen.SetProperty(info, tripwireHookMin, "facing", facing)
}

func TestTripwire(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	powered := func(pos blockPos) bool { return boolProp(w.At(pos.x, pos.y, pos.z), "powered") }
	attached := func(pos blockPos) bool { return boolProp(w.At(pos.x, pos.y, pos.z), "attached") }

	onHub(t, h, func() {
		a := blockPos{0, 70, 0}
		b := blockPos{4, 70, 0}
		w.SetBlock(a.x, a.y, a.z, hookFacing(t, "east")) // faces along +x toward b
		w.SetBlock(b.x, b.y, b.z, hookFacing(t, "west")) // faces back toward a
		for x := 1; x <= 3; x++ {
			w.SetBlock(x, 70, 0, tripwireDefaultState()) // a string line between them
		}

		h.calcHook(h.playersRef, a, w.At(a.x, a.y, a.z))
		h.calcHook(h.playersRef, b, w.At(b.x, b.y, b.z))
		if !attached(a) || !attached(b) {
			t.Errorf("hooks should be attached across a full line (a=%v b=%v)", attached(a), attached(b))
			return
		}
		if powered(a) || powered(b) {
			t.Error("hooks powered with nothing on the wire")
			return
		}

		// An entity steps on a middle string → both hooks energise and emit 15.
		h.setWirePressed(h.playersRef, blockPos{2, 70, 0}, true)
		if !powered(a) || !powered(b) {
			t.Errorf("hooks not powered after the wire tripped (a=%v b=%v)", powered(a), powered(b))
			return
		}
		if p := h.emitPower(a.x, a.y, a.z, a.x-1, a.y, a.z); p != 15 {
			t.Errorf("tripped hook emits %d, want 15", p)
		}

		// Step off → hooks release (still attached).
		h.setWirePressed(h.playersRef, blockPos{2, 70, 0}, false)
		if powered(a) || powered(b) {
			t.Error("hooks stayed powered after the wire released")
		}
		if !attached(a) {
			t.Error("hook detached after release (line still intact)")
		}

		// A disarmed string never trips its hooks.
		w.SetBlock(2, 70, 0, setBoolProp(w.At(2, 70, 0), "disarmed", true))
		h.setWirePressed(h.playersRef, blockPos{2, 70, 0}, true)
		if powered(a) {
			t.Error("a disarmed wire tripped its hook")
		}
	})
}
