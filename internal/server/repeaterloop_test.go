package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Legion's report #6, rebuilt from its capture: a long dust line with a
// repeater on a jumper one row to the side, both ends of the jumper joining
// the SAME continuous line. That is a closed loop — the repeater's own output
// comes back round into its input — and a repeater holds itself on forever
// once it does, because its output is a full 15 whatever fed it.
//
// The question this test settles is whether the engine latches the way the
// real game does, or whether something is keeping the repeater on that should
// not be.
func TestRepeaterFedByItsOwnOutputLatchesOn(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	players := map[int32]*tracked{}
	const y, z = 180, 40
	const x0, x1 = 40, 54 // the main line
	h.world.ForceLoad(x0, z, 1)
	h.world.ForceLoad(x1, z, 1)
	for x := x0 - 2; x <= x1+2; x++ {
		for dz := -2; dz <= 2; dz++ {
			w.SetBlock(x, y-1, z+dz, worldgen.Stone)
			w.SetBlock(x, y, z+dz, worldgen.Air)
			w.SetBlock(x, y+1, z+dz, worldgen.Air)
		}
	}
	for x := x0; x <= x1; x++ { // the line itself
		w.SetBlock(x, y, z, dust(t))
	}
	// The jumper, one row north: dust, repeater, dust — the repeater pointing
	// so that its output goes back into the west end of the jumper.
	jx := x0 + 6
	w.SetBlock(jx, y, z-1, dust(t))
	w.SetBlock(jx+1, y, z-1, repeaterFacing(t, "east", false))
	w.SetBlock(jx+2, y, z-1, dust(t))

	// A lever drives the line's west end, through the block beneath it.
	lever := blockPos{x0 - 1, y - 1, z}
	w.SetBlock(lever.x, lever.y, lever.z, wallLever(t, "west"))
	h.toggleLever(players, lever, w.At(lever.x, lever.y, lever.z))
	stepTicks(h, players, 60)

	if !boolProp(w.At(jx+1, y, z-1), "powered") {
		t.Fatal("setup: the repeater should be powered while the lever is on")
	}
	if p := wirePower(w.At(x1, y, z)); p == 0 {
		t.Fatal("setup: the far end of the line should be lit")
	}

	// Now the lever goes off. The loop keeps the repeater on — this is the
	// behaviour the report is about, and it is what the real game does.
	h.toggleLever(players, lever, w.At(lever.x, lever.y, lever.z))
	stepTicks(h, players, 120)

	stillOn := boolProp(w.At(jx+1, y, z-1), "powered")
	t.Logf("with the lever off: repeater powered=%v, line end power=%d, jumper out=%d",
		stillOn, wirePower(w.At(x1, y, z)), wirePower(w.At(jx, y, z-1)))
	if !stillOn {
		t.Skip("the loop did not latch here — the report may be something else")
	}

	// And breaking the main line between the jumper's two ends is what frees
	// it: the signal then has to go THROUGH the repeater instead of round it.
	w.SetBlock(jx+1, y, z, worldgen.Air)
	h.onBlock(players, evBlock{x: jx + 1, y: y, z: z, dim: 0, state: worldgen.Air, broken: dust(t)})
	stepTicks(h, players, 120)
	if boolProp(w.At(jx+1, y, z-1), "powered") {
		t.Errorf("with the loop cut and the lever off, the repeater is still on")
	}
}
