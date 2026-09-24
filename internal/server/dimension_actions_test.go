package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A dispenser in the Nether acts in the Nether. Its TNT, its dropped items
// and its spawn eggs all landed at the same coordinates in the overworld
// until 2026-09-24.
func TestNetherDispenserActsInTheNether(t *testing.T) {
	h := newHub(world.New(1))
	nw, _ := world.NewNether(1, nil)
	h.nether = nw
	players := map[int32]*tracked{}
	pos := blockPos{5, 100, 5}
	nw.ForceLoad(pos.x, pos.z, 1)
	h.world.ForceLoad(pos.x, pos.z, 1)
	state := eastDispenser(t)
	key := simPos{dim: 1, blockPos: pos}
	eject := func(st invStack) {
		nw.SetBlock(pos.x, pos.y, pos.z, state)
		nw.SetBlock(pos.x+1, pos.y, pos.z, worldgen.Air)
		b := &bin{slots: make([]invStack, 9)}
		b.slots[0] = st
		h.bins[key] = b
		h.inDim(1, func() { h.ejectFromBin(players, key, state) }) // as the scheduled update runs it
	}

	eject(invStack{item: itemTNTBlock, count: 1})
	if len(h.tnt) != 1 || h.tnt[0].dim != 1 {
		t.Fatalf("dispensed TNT: %d primed, want one in the Nether", len(h.tnt))
	}

	eject(invStack{item: itemByName["cobblestone"], count: 1})
	var drop *itemEntity
	for _, it := range h.items {
		drop = it
	}
	if drop == nil || drop.dim != 1 {
		t.Fatalf("dispensed item %+v, want it in the Nether", drop)
	}
}

// Redstone-lit TNT and TNT lit by hand go off where they stand.
func TestTNTPrimesInItsOwnDimension(t *testing.T) {
	h := newHub(world.New(1))
	nw, _ := world.NewNether(1, nil)
	h.nether = nw
	players := map[int32]*tracked{}
	nw.ForceLoad(0, 0, 1)
	h.inDim(1, func() { h.primeTNT(players, 0, 100, 0, tntFuseTicks) })
	h.primeTNTIn(players, 1, 3, 100, 0, tntFuseTicks) // the evPrimeTNT path
	for _, p := range h.tnt {
		if p.dim != 1 {
			t.Fatalf("TNT primed in dimension %d, want the Nether", p.dim)
		}
	}
	if len(h.tnt) != 2 {
		t.Fatalf("%d primed, want 2", len(h.tnt))
	}
}
