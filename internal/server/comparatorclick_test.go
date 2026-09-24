package server

import (
	"testing"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestComparatorClickPitch: ComparatorBlock.useWithoutItem clicks at 0.55
// switching to subtract and 0.5 switching back to compare.
func TestComparatorClickPitch(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	listener := survPlayer(h)
	listener.x, listener.y, listener.z = float64(x), float64(y), float64(z)
	players[listener.p.eid] = listener
	info, _ := worldgen.InfoForState(comparatorMin)
	w.SetBlock(x, y, z, worldgen.SetProperty(info, comparatorMin, "mode", "compare"))
	pitchOf := func() float32 {
		for {
			select {
			case pkt := <-listener.p.out:
				if ev, ok := pkt.ev.(attachproto.Sound); ok && ev.Name == "minecraft:block.comparator.click" {
					return ev.Pitch
				}
			default:
				return -1
			}
		}
	}
	drainOut(listener.p)
	h.useRedstone1b(players, blockPos{x, y, z}, w.At(x, y, z)) // → subtract
	if p := pitchOf(); p != 0.55 {
		t.Errorf("to subtract: pitch %v, want 0.55", p)
	}
	drainOut(listener.p)
	h.useRedstone1b(players, blockPos{x, y, z}, w.At(x, y, z)) // → compare
	if p := pitchOf(); p != 0.5 {
		t.Errorf("to compare: pitch %v, want 0.5", p)
	}
}
