package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TntBlock.useItemOn: lighting TNT with flint and steel wears it a point
// (hurtAndBreak(1)), as lighting a fire does.
func TestFlintAndSteelWearsLightingTNT(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	s := &Server{world: w, hub: h}
	w.SetBlock(0, 180, 0, worldgen.BlockBase("tnt"))
	p := newPlayer(1, "tester", [16]byte{1})
	p.setHotbarSlot(0, itemFlintSteel)
	s.useFlintSteel(p, 0, 180, 0, 0, 1, 0, 0)
	primed, worn := false, false
	for len(h.events) > 0 {
		switch e := (<-h.events).(type) {
		case evPrimeTNT:
			primed = true
		case evToolWear:
			worn = e.eid == p.eid && e.slot == p.held
		}
	}
	if !primed {
		t.Fatal("the TNT was not primed")
	}
	if !worn {
		t.Error("lighting TNT did not wear the flint and steel")
	}
}
