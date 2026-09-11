package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Breaking a powered wall lever must switch off what its wall powered
// (LeverBlock.affectNeighborsAfterRemoval → updateNeighbours, which reaches
// the attached block's own neighbours).
func TestRemovingPoweredLeverUnpowersBeyondWall(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x+1, y, z, worldgen.Stone)
	w.SetBlock(x, y, z, wallLever(t, "west"))
	w.SetBlock(x+2, y, z, lampOff)
	h.toggleLever(players, blockPos{x, y, z}, w.At(x, y, z))
	stepTicks(h, players, 12)
	if w.At(x+2, y, z) != lampOn {
		t.Fatal("lamp beyond the wall should light")
	}
	h.setBlockLive(players, 0, x, y, z, worldgen.Air) // the player breaks the lever
	stepTicks(h, players, 12)
	if w.At(x+2, y, z) != lampOff {
		t.Fatal("lamp beyond the wall should go out when the lever is broken")
	}
}

// Breaking a piston head destroys the extended base behind it
// (PistonHeadBlock.affectNeighborsAfterRemoval → destroyBlock(base, drop)).
func TestRemovingPistonHeadBreaksBase(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	base := setBoolProp(pistonEast(false), "extended", true)
	w.SetBlock(x, y, z, base)
	w.SetBlock(x+1, y, z, headFor(base))
	h.setBlockLive(players, 0, x+1, y, z, worldgen.Air)
	stepTicks(h, players, 4)
	if got := w.At(x, y, z); got != worldgen.Air {
		t.Fatalf("base holds %d after its head was broken", got)
	}
	drops := 0
	for _, it := range h.items {
		if it.item == itemByName["piston"] {
			drops++
		}
	}
	if drops != 1 {
		t.Fatalf("piston drops %d, want 1", drops)
	}
}

// Breaking a container refreshes the comparator reading it through a solid
// block two cells away (Containers.updateNeighboursAfterDestroy →
// updateNeighbourForOutputSignal).
func TestRemovingContainerRefreshesComparatorBeyondBlock(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	w.SetBlock(x, y, z, worldgen.BlockBase("chest")+1)
	c := &chest{}
	c.slots[0] = invStack{item: itemByName["stone"], count: 64}
	h.chests[simPos{blockPos: blockPos{x, y, z}}] = c
	w.SetBlock(x+1, y, z, worldgen.Stone)
	info, _ := worldgen.InfoForState(worldgen.BlockBase("comparator") + 1)
	comp := worldgen.SetProperty(info, worldgen.BlockBase("comparator")+1, "facing", "west")
	w.SetBlock(x+2, y, z, comp)
	w.SetBlock(x+3, y, z, worldgen.BlockBase("redstone_wire")+1160)
	h.scheduleAround(blockPos{x + 2, y, z}, 1)
	stepTicks(h, players, 6)
	if p := wirePower(w.At(x+3, y, z)); p == 0 {
		t.Fatal("comparator should read the chest through the block")
	}
	delete(h.chests, simPos{blockPos: blockPos{x, y, z}})
	h.setBlockLive(players, 0, x, y, z, worldgen.Air) // the player breaks the chest
	stepTicks(h, players, 6)
	if p := wirePower(w.At(x+3, y, z)); p != 0 {
		t.Fatalf("comparator still outputs %d after the chest is gone", p)
	}
}

// Breaking an extended piston's base drops its head (no item); a base
// turning into the moving cell that slides the head back keeps it.
func TestRemovingPistonBaseDropsHead(t *testing.T) {
	h, w, players, x, y, z := redSetup(t)
	base := setBoolProp(pistonEast(false), "extended", true)
	w.SetBlock(x, y, z, base)
	w.SetBlock(x+1, y, z, headFor(base))
	h.setBlockLive(players, 0, x, y, z, worldgen.Air)
	if got := w.At(x+1, y, z); got != worldgen.Air {
		t.Fatalf("head holds %d after its base was broken", got)
	}
	if len(h.items) != 0 {
		t.Fatalf("a piston head drops nothing, got %d items", len(h.items))
	}
	w.SetBlock(x, y, z, base)
	w.SetBlock(x+1, y, z, headFor(base))
	h.setBlock(players, blockPos{x, y, z}, movingPistonState([3]int{1, 0, 0}, false))
	if got := w.At(x+1, y, z); !isPistonHead(got) {
		t.Fatalf("head should stay while the base is a moving cell, holds %d", got)
	}
}
