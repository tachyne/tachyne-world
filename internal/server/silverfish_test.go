package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestSilverfishFriendsAndStone: a hurt silverfish breaks nearby infested
// stone open, freeing more; an idle one burrows into the stone beside it.
func TestSilverfishFriendsAndStone(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	w.SetBlock(3, 180, 0, worldgen.BlockBase("infested_stone"))
	s := h.spawnMob(players, entitySilverfish, 0.5, 180, 0.5)
	s.hurtKind(1, dtMobAttack)
	if !s.silverHurt {
		t.Fatal("a blow should be recorded for the wake-up goal")
	}
	before := len(h.mobs)
	for i := 0; i < 12 && w.At(3, 180, 0) != worldgen.Air; i++ {
		h.silverfishStep(players, s)
	}
	if w.At(3, 180, 0) != worldgen.Air || len(h.mobs) != before+1 {
		t.Fatalf("the infested block should break open with a silverfish: block %d mobs %d→%d", w.At(3, 180, 0), before, len(h.mobs))
	}
	// Merge: stone to the east, no target; the roll is one in five updates.
	w.SetBlock(1, 180, 0, worldgen.Stone)
	s.hasTarget = false
	merged := false
	for i := 0; i < 400 && !merged; i++ {
		h.silverfishStep(players, s)
		merged = h.mobs[s.eid] == nil
	}
	if !merged {
		t.Fatal("an idle silverfish burrows into stone")
	}
	infested := 0
	for _, d := range [][3]int{{1, 180, 0}, {-1, 180, 0}, {0, 180, 1}, {0, 180, -1}, {0, 179, 0}} {
		if isInfested(w.At(d[0], d[1], d[2])) {
			infested++
		}
	}
	if infested != 1 {
		t.Fatalf("exactly one block becomes infested: %d", infested)
	}
}
