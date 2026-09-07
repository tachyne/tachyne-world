package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A barrel opens as a 27-slot container, shows its lid open while viewed,
// and shuts it again when the viewer leaves.
func TestBarrelOpensAndCloses(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	h.playersRef = map[int32]*tracked{pl.p.eid: pl}
	closed := setBoolProp(barrelMin, "open", false)
	h.worldFor(0).SetBlock(0, 180, 0, closed)
	if containerOpenFor(closed) != openChestWindow {
		t.Fatal("a barrel should open as a chest window")
	}
	h.openChest(pl, 0, 180, 0)
	if !boolProp(h.worldFor(0).At(0, 180, 0), "open") {
		t.Fatal("the lid should be open while viewed")
	}
	if pl.winKind != winChest || h.chests[simPos{blockPos: blockPos{0, 180, 0}}] == nil {
		t.Fatal("the barrel should have chest storage behind the window")
	}
	h.releaseContainerView(pl)
	if boolProp(h.worldFor(0).At(0, 180, 0), "open") {
		t.Fatal("the lid should close when the viewer leaves")
	}
	// The lid toggling must not spill the contents.
	if h.chests[simPos{blockPos: blockPos{0, 180, 0}}] == nil {
		t.Fatal("closing the lid spilled the barrel")
	}
}
