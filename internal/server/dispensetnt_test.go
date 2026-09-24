package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A dispenser's TNT is a lit charge in the cell ahead; whatever block stood
// there stays. It used to be cleared to air (the block-priming path).
func TestDispensedTNTLeavesTheBlockAhead(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	pos := blockPos{5, 100, 5}
	h.world.ForceLoad(pos.x, pos.z, 1)
	state := eastDispenser(t)
	key := simPos{dim: 0, blockPos: pos}
	glass := worldgen.BlockBase("glass")
	h.world.SetBlock(pos.x, pos.y, pos.z, state)
	h.world.SetBlock(pos.x+1, pos.y, pos.z, glass)
	b := &bin{slots: make([]invStack, 9)}
	b.slots[0] = invStack{item: itemTNTBlock, count: 2}
	h.bins[key] = b
	h.inDim(0, func() { h.ejectFromBin(players, key, state) }) // as the scheduled update runs it
	if len(h.tnt) != 1 {
		t.Fatalf("dispensed TNT: %d primed, want one", len(h.tnt))
	}
	if p := h.tnt[0]; p.x != float64(pos.x+1)+0.5 || p.z != float64(pos.z)+0.5 {
		t.Errorf("the charge sits at %.1f,%.1f, want the centre of the cell ahead", p.x, p.z)
	}
	if got := h.world.At(pos.x+1, pos.y, pos.z); got != glass {
		t.Fatalf("the block ahead became %d, want the glass left alone", got)
	}
	if b.slots[0].count != 1 {
		t.Errorf("one TNT should be used: %d left", b.slots[0].count)
	}

	// tnt_explodes off: the dispenser keeps its TNT.
	h.rules.TNTExplodes = false
	h.inDim(0, func() { h.ejectFromBin(players, key, state) })
	if len(h.tnt) != 1 || b.slots[0].count != 1 {
		t.Errorf("with tnt_explodes off nothing is dispensed: %d charges, %d left", len(h.tnt), b.slots[0].count)
	}
}
