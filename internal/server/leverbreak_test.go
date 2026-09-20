package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Legion, 2026-09-20: "when I power redstone with a lever and then break the
// lever the redstone dust remains powered". Breaking a powered source is not
// the same event as flipping it off — the block is gone, so nothing toggles;
// only the neighbour update that its removal schedules can clear the line.
func TestBreakingAPoweredLeverClearsTheWire(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	lever := setBoolProp((worldgen.BlockBase("lever") + 9), "powered", false)
	w.SetBlock(x, y, z+1, worldgen.Stone) // the wall it hangs on
	w.SetBlock(x, y, z, lever)
	for i := 1; i <= 4; i++ {
		w.SetBlock(x+i, y, z, worldgen.BlockBase("redstone_wire")+1160)
	}
	w.SetBlock(x+5, y, z, lampOff)

	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 12)
	if w.At(x+5, y, z) != lampOn {
		t.Fatal("setup: the lever should have lit the lamp")
	}

	// Break it, exactly as a dig does: the world write, then the event.
	broken := w.At(x, y, z)
	w.SetBlock(x, y, z, worldgen.Air)
	h.onBlock(players, evBlock{x: x, y: y, z: z, dim: 0, state: worldgen.Air, broken: broken})
	stepTicks(h, players, 40)

	for i := 1; i <= 4; i++ {
		if p := wirePower(w.At(x+i, y, z)); p != 0 {
			t.Errorf("dust cell %d still carries %d after the lever was broken", i, p)
		}
	}
	if w.At(x+5, y, z) != lampOff {
		t.Error("the lamp is still lit with nothing powering it")
	}
}

// …and the same when the lever is not broken directly but LOSES ITS SUPPORT:
// the wall it hangs on goes, the lever pops off as an item, and the line it
// was powering has to notice. This is the harder path — nothing is dug at the
// lever's own position, so only the pop itself can schedule the update.
func TestBreakingALeversSupportClearsTheWire(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	// A WALL lever this time — face=wall, so it hangs on the block behind it.
	// (BlockBase's lever is a FLOOR lever: property index 0 all the way.)
	w.SetBlock(x, y, z-1, worldgen.Stone) // the wall it hangs on, to the north
	w.SetBlock(x, y, z, wallLever(t, "south"))
	for i := 1; i <= 4; i++ {
		w.SetBlock(x+i, y, z, dust(t))
	}
	w.SetBlock(x+5, y, z, lampOff)

	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 12)
	if w.At(x+5, y, z) != lampOn {
		t.Fatal("setup: the lever should have lit the lamp")
	}

	broken := w.At(x, y, z-1)
	w.SetBlock(x, y, z-1, worldgen.Air)
	h.onBlock(players, evBlock{x: x, y: y, z: z - 1, dim: 0, state: worldgen.Air, broken: broken})
	h.dropUnsupported(players, 0, blockPos{x, y, z - 1}) // …as the hub does after onBlock
	stepTicks(h, players, 40)

	if s := w.At(x, y, z); isLever(s) {
		t.Fatalf("the lever kept hanging on nothing: %d", s)
	}
	for i := 1; i <= 4; i++ {
		if p := wirePower(w.At(x+i, y, z)); p != 0 {
			t.Errorf("dust cell %d still carries %d after the lever fell", i, p)
		}
	}
	if w.At(x+5, y, z) != lampOff {
		t.Error("the lamp is still lit with nothing powering it")
	}
}

// The real shape of Legion's report: the lever is not next to the dust, it is
// on the far side of a solid block. That block carries the lever's STRONG
// power, and the dust reads it through the block — so the dust sits two cells
// from the lever and a six-neighbour update around the broken lever never
// reaches it. Vanilla covers this in LeverBlock's own removal, which updates
// the neighbours of the block it hung on as well as its own.
func TestBreakingALeverThroughItsBlockClearsTheWire(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x+1, y, z, worldgen.Stone)       // the block the lever drives
	w.SetBlock(x+2, y, z, wallLever(t, "east")) // on its east face, attached to the west
	w.SetBlock(x, y, z, dust(t))                // dust on the block's far side
	w.SetBlock(x, y, z-1, lampOff)

	h.toggleLever(players, blockPos{x + 2, y, z}, w.At(x+2, y, z))
	stepTicks(h, players, 12)
	if p := wirePower(w.At(x, y, z)); p != 15 {
		t.Fatalf("setup: the dust should read 15 through the block, has %d", p)
	}

	broken := w.At(x+2, y, z)
	w.SetBlock(x+2, y, z, worldgen.Air)
	h.onBlock(players, evBlock{x: x + 2, y: y, z: z, dim: 0, state: worldgen.Air, broken: broken})
	h.dropUnsupported(players, 0, blockPos{x + 2, y, z})
	stepTicks(h, players, 40)

	if p := wirePower(w.At(x, y, z)); p != 0 {
		t.Errorf("the dust still carries %d with the lever gone", p)
	}
	if w.At(x, y, z-1) != lampOff {
		t.Error("the lamp is still lit with nothing powering it")
	}
}

// The same relay in the other direction: a lever PLACED already-on against a
// solid block has to reach the dust on the block's far side straight away,
// without waiting for something else to poke it. (A block of redstone would
// NOT — it powers its neighbours weakly, and dust reads only strong power out
// of a block.)
func TestPlacingASourceThroughItsBlockPowersTheWire(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x+1, y, z, worldgen.Stone)
	w.SetBlock(x, y, z, dust(t))
	stepTicks(h, players, 4)
	if p := wirePower(w.At(x, y, z)); p != 0 {
		t.Fatalf("setup: the dust should be dark, has %d", p)
	}

	on := setBoolProp(wallLever(t, "east"), "powered", true)
	w.SetBlock(x+2, y, z, on)
	h.onBlock(players, evBlock{x: x + 2, y: y, z: z, dim: 0, state: on})
	stepTicks(h, players, 20)

	if p := wirePower(w.At(x, y, z)); p != 15 {
		t.Errorf("the dust reads %d through the block, want 15", p)
	}
}
