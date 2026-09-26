package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Bugs #40/#43: a hedge saved before placed leaves were persistent — edited,
// non-persistent, no trunk in reach — is made persistent; a leaf held by a
// trunk (a tree's) is left to decay as vanilla's would.
func TestRepairPersistsOrphanedPlacedLeaves(t *testing.T) {
	w := world.New(1)
	base := worldgen.BlockBase("flowering_azalea_leaves")
	info, _ := worldgen.InfoForState(base)
	tree := func(d int) uint32 { // a non-persistent leaf at distance d, dry
		s := worldgen.SetProperty(info, base, "persistent", "false")
		s = worldgen.SetProperty(info, s, "waterlogged", "false")
		return worldgen.SetProperty(info, s, "distance", string(rune('0'+d)))
	}
	const y = 200 // open sky: nothing generated nearby
	for x := 0; x < 3; x++ {
		w.SetBlock(x, y, 0, tree(7)) // a three-leaf hedge, no log anywhere
	}
	w.SetBlock(20, y, 0, worldgen.BlockBase("oak_log"))
	w.SetBlock(21, y, 0, tree(1)) // a tree's leaf beside its trunk

	if n := persistOrphanedLeaves(w); n != 3 {
		t.Fatalf("repaired %d leaves, want the 3 of the hedge", n)
	}
	for x := 0; x < 3; x++ {
		if _, _, p, _ := leafInfo(w.At(x, y, 0)); !p {
			t.Errorf("hedge leaf %d still not persistent", x)
		}
	}
	if _, _, p, _ := leafInfo(w.At(21, y, 0)); p {
		t.Error("a leaf held by a trunk must stay a tree's leaf (non-persistent)")
	}
	if n := persistOrphanedLeaves(w); n != 0 {
		t.Errorf("a second pass changed %d leaves; the repair must be idempotent", n)
	}
}
