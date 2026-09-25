package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TntBlock.useItemOn: lighting TNT with flint and steel wears it a point
// (hurtAndBreak(1)) and counts ITEM_USED; with tnt_explodes off the TNT
// stays, the lighter is untouched, and the player is told.
func TestFlintAndSteelWearsLightingTNT(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	s := &Server{world: w, hub: h}
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.inv.slots[0] = invStack{item: itemFlintSteel, count: 1}
	pl.p.setHotbarSlot(0, itemFlintSteel)
	light := func() {
		w.SetBlock(0, 180, 0, worldgen.BlockBase("tnt"))
		s.useFlintSteel(pl.p, false, 0, 180, 0, 0, 1, 0, 0)
		for len(h.events) > 0 {
			if e, ok := (<-h.events).(evPrimeTNT); ok {
				h.onPrimeTNT(players, e)
			}
		}
	}
	h.rules.TNTExplodes = false
	light()
	if len(h.tnt) != 0 || w.At(0, 180, 0) != worldgen.BlockBase("tnt") || pl.inv.slots[0].dmg != 0 {
		t.Fatalf("tnt_explodes off: nothing lit, block kept, lighter unworn (%d charges, dmg %d)", len(h.tnt), pl.inv.slots[0].dmg)
	}
	h.rules.TNTExplodes = true
	light()
	if len(h.tnt) != 1 || w.At(0, 180, 0) != worldgen.Air {
		t.Fatal("the TNT was not primed")
	}
	if pl.inv.slots[0].dmg != 1 {
		t.Errorf("lighting TNT wore the flint and steel %d, want 1", pl.inv.slots[0].dmg)
	}
	// TntBlock.prime: an adventure player's lighter has no can_break for
	// TNT, so nothing is lit.
	h.tnt = nil
	pl.gamemode = gmAdventure
	light()
	if len(h.tnt) != 0 || w.At(0, 180, 0) != worldgen.BlockBase("tnt") {
		t.Fatal("an adventure player lit TNT")
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

// A fiery blast lights soul fire on soul soil (Explosion.createFire uses
// BaseFireBlock.getState).
func TestBlastFireOverSoulSoil(t *testing.T) {
	w := world.New(1)
	w.ForceLoad(0, 0, 1)
	h := newHub(w)
	var cleared []blockPos
	for x := 0; x < 30; x++ {
		w.SetBlock(x, 180, 0, worldgen.BlockBase("soul_soil"))
		w.SetBlock(x, 181, 0, worldgen.Air)
		cleared = append(cleared, blockPos{x, 181, 0})
	}
	h.lightBlastFires(map[int32]*tracked{}, 0, cleared)
	soul, plain := 0, 0
	for x := 0; x < 30; x++ {
		switch st := w.At(x, 181, 0); {
		case st == soulFire:
			soul++
		case isFire(st):
			plain++
		}
	}
	if soul == 0 || plain != 0 {
		t.Fatalf("over soul soil a blast lit %d soul fires and %d plain ones", soul, plain)
	}
}
