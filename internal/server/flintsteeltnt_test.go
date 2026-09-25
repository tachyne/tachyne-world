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
	s.useFlintSteel(p, false, 0, 180, 0, 0, 1, 0, 0)
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

// Fire lit over soul sand or soul soil is soul fire (BaseFireBlock.getState),
// by flint and steel and by spreading fire alike, and it goes out when its
// soul block is taken away (SoulFireBlock.canSurvive).
func TestSoulFireOverSoulBlocks(t *testing.T) {
	w := world.New(1)
	w.ForceLoad(0, 0, 1)
	h := newHub(w)
	s := &Server{world: w, hub: h, modes: newModeStore("", gmCreative)}
	players := map[int32]*tracked{}
	w.SetBlock(0, 180, 0, worldgen.BlockBase("soul_soil"))
	p := newPlayer(1, "tester", [16]byte{1})
	p.setHotbarSlot(0, itemFlintSteel)
	s.useFlintSteel(p, false, 0, 180, 0, 0, 1, 0, 0)
	if got := w.Block(0, 181, 0); got != soulFire {
		t.Fatalf("flint and steel on soul soil lit %d, want soul fire %d", got, soulFire)
	}
	h.setBlockAt(players, 0, blockPos{0, 180, 0}, worldgen.Stone)
	if got := w.At(0, 181, 0); got == soulFire {
		t.Fatal("soul fire stayed once its soul soil was gone")
	}

	w.SetBlock(3, 180, 0, worldgen.SoulSand)
	h.inDim(0, func() { h.igniteFire(players, blockPos{3, 181, 0}, 0) })
	if got := w.At(3, 181, 0); got != soulFire {
		t.Fatalf("fire caught over soul sand is %d, want soul fire", got)
	}
}
