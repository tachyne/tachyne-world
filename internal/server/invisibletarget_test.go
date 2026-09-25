package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A zombie picks a visible player out at twelve blocks, but not an invisible
// one (0.07 of its 35-block follow range is 2.45), nor one wearing a zombie
// head at twenty blocks (half the range) — and the invisible player in full
// armour is back in reach (0.7 × 1).
func TestInvisibilityHidesFromMobs(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.world.ForceLoad(0, 0, 3)
	for x := -4; x <= 28; x++ {
		for z := -3; z <= 3; z++ {
			h.world.SetBlock(x, 179, z, worldgen.Stone)
			for y := 180; y < 184; y++ {
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	pl.x, pl.y, pl.z = 12.5, 180, 0.5
	h.allocEID()
	z := h.spawnMob(players, entityZombie, 0.5, 180, 0.5)
	reach := 35.0
	if got := h.huntTarget(players, z, reach); got != pl {
		t.Fatal("a zombie did not see a visible player twelve blocks off")
	}
	z.targetEID = 0
	h.applyEffect(players, pl, effInvisibility, 0, 60)
	if got := h.huntTarget(players, z, reach); got != nil {
		t.Fatal("a zombie picked out an invisible, unarmoured player at twelve blocks")
	}
	for i := range pl.armor {
		pl.armor[i] = invStack{item: itemByName["iron_helmet"], count: 1}
	}
	if got := h.huntTarget(players, z, reach); got != pl {
		t.Fatal("an invisible player in full armour should be seen at twelve blocks")
	}
	z.targetEID = 0
	h.removeEffect(pl, effInvisibility)
	pl.armor = [4]invStack{{item: itemByName["zombie_head"], count: 1}}
	pl.x = 20.5
	if got := h.huntTarget(players, z, reach); got != nil {
		t.Fatal("a zombie head did not halve the zombie's range")
	}
}
