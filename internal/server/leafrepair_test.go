package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// leafState is a flowering azalea leaf, dry, at distance d and persistence p.
func leafState(d int, p bool) uint32 {
	base := worldgen.BlockBase("flowering_azalea_leaves")
	info, _ := worldgen.InfoForState(base)
	v := "false"
	if p {
		v = "true"
	}
	s := worldgen.SetProperty(info, base, "persistent", v)
	s = worldgen.SetProperty(info, s, "waterlogged", "false")
	return worldgen.SetProperty(info, s, "distance", string(rune('0'+d)))
}

func leafPersistent(w *world.World, x, y, z int) bool {
	_, _, p, _ := leafInfo(w.At(x, y, z))
	return p
}

// Bugs #40/#43: a hedge saved before placed leaves were persistent — edited,
// non-persistent, no trunk in reach, beside a player's build — is made
// persistent. Canopy standing among nothing, and a tree's own leaf, are left
// to decay as vanilla's would.
func TestRepairPersistsHedgesOnly(t *testing.T) {
	w := world.New(1)
	const y = 200 // open sky: nothing generated nearby
	for x := 0; x < 3; x++ {
		w.SetBlock(x, y, 0, leafState(7, false)) // a hedge…
	}
	w.SetBlock(1, y-1, 0, worldgen.BlockBase("mossy_cobblestone_wall")) // …on a player's wall
	w.SetBlock(60, y, 60, leafState(7, false))                          // canopy of a felled tree, alone
	w.SetBlock(20, y, 0, worldgen.BlockBase("oak_log"))
	w.SetBlock(21, y, 0, leafState(1, false)) // a tree's leaf beside its trunk

	if n := persistHedges(w); n != 3 {
		t.Fatalf("repaired %d leaves, want the 3 of the hedge", n)
	}
	for x := 0; x < 3; x++ {
		if !leafPersistent(w, x, y, 0) {
			t.Errorf("hedge leaf %d still not persistent", x)
		}
	}
	if leafPersistent(w, 60, y, 60) {
		t.Error("a lone canopy leaf must keep decaying")
	}
	if leafPersistent(w, 21, y, 0) {
		t.Error("a leaf held by a trunk must stay a tree's leaf")
	}
	if n := persistHedges(w); n != 0 {
		t.Errorf("a second pass changed %d leaves; the repair must be idempotent", n)
	}
}

// The first version made lone canopy persistent too; the undo pass returns it
// to decaying and leaves hedges and trees alone.
func TestLeafUndoReturnsCanopyToDecaying(t *testing.T) {
	w := world.New(1)
	const y = 200
	w.SetBlock(0, y, 0, leafState(7, true)) // a hedge…
	w.SetBlock(1, y, 0, worldgen.BlockBase("lantern"))
	w.SetBlock(60, y, 60, leafState(7, true)) // canopy the first version made persistent
	w.SetBlock(20, y, 0, worldgen.BlockBase("oak_log"))
	w.SetBlock(21, y, 0, leafState(1, true)) // a persistent leaf by a trunk: left as it is

	if n := unpersistCanopy(w); n != 1 {
		t.Fatalf("undid %d leaves, want only the lone canopy leaf", n)
	}
	if leafPersistent(w, 60, y, 60) {
		t.Error("the lone canopy leaf must decay again")
	}
	if !leafPersistent(w, 0, y, 0) || !leafPersistent(w, 21, y, 0) {
		t.Error("the hedge and the leaf by a trunk must stay persistent")
	}
}
