package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// repeaterFacing builds a repeater state with the given facing + powered flag.
func repeaterFacing(t *testing.T, facing string, powered bool) uint32 {
	t.Helper()
	info, _ := worldgen.InfoForState(repeaterMin)
	s := worldgen.SetProperty(info, repeaterMin, "facing", facing)
	return setBoolProp(s, "powered", powered)
}

func TestRepeaterLocking(t *testing.T) {
	_, h, _ := breakPlaceServer(t)
	w := h.world
	victimPos := blockPos{0, 70, 0}

	onHub(t, h, func() {
		// Victim faces north; its sides are east/west. A powered repeater on the
		// east side facing WEST (into the victim) locks it.
		victim := repeaterFacing(t, "north", false)
		lock := repeaterFacing(t, "east", true) // outputs west, toward the victim at (0,70,0)? verify below
		w.SetBlock(1, 70, 0, lock)
		w.SetBlock(victimPos.x, victimPos.y, victimPos.z, victim)

		if !h.repeaterLocked(victimPos, victim) {
			t.Fatal("repeater with a powered diode facing its side should be locked")
		}
		h.updateRepeater(h.playersRef, victimPos, victim)
		if !boolProp(w.At(victimPos.x, victimPos.y, victimPos.z), "locked") {
			t.Error("locked property not written")
		}

		// Unpower the side diode → no longer locked.
		w.SetBlock(1, 70, 0, repeaterFacing(t, "east", false))
		if h.repeaterLocked(victimPos, victim) {
			t.Error("repeater still locked after the side diode lost power")
		}

		// A side diode facing a DIFFERENT way (not into the victim) does not lock.
		w.SetBlock(1, 70, 0, repeaterFacing(t, "north", true))
		if h.repeaterLocked(victimPos, victim) {
			t.Error("a powered diode not facing the repeater must not lock it")
		}
	})
}
