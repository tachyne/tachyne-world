package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Hit one zombified piglin and the pack comes — every one inside a box
// thirty-five across and ten high, and they come for YOU rather than just
// seething where they stand.
func TestHittingOneZombifiedPiglinAlertsThePack(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	pl.x, pl.y, pl.z = 0.5, 70, 0.5

	hit := h.spawnMobIn(players, entityZombifiedPiglin, 0, 2.5, 70, 0.5)
	near := h.spawnMobIn(players, entityZombifiedPiglin, 0, 25.5, 70, 0.5) // inside 35
	high := h.spawnMobIn(players, entityZombifiedPiglin, 0, 4.5, 85, 0.5)  // 15 up: outside 10
	far := h.spawnMobIn(players, entityZombifiedPiglin, 0, 60.5, 70, 0.5)  // outside 35
	for _, m := range []*mob{hit, near, high, far} {
		if m == nil {
			t.Fatal("the piglins should have spawned")
		}
	}
	h.alertZombifiedPiglins(hit, pl)

	if near.targetEID != pl.p.eid {
		t.Fatalf("a piglin inside the box should hunt the attacker, target=%d", near.targetEID)
	}
	if high.targetEID == pl.p.eid {
		t.Fatal("one fifteen blocks up is outside the ten-high box")
	}
	if far.targetEID == pl.p.eid {
		t.Fatal("one sixty blocks away is outside the box")
	}
}

// Opening a chest a piglin considers its own turns it on you — the rule that
// makes looting a bastion a decision rather than a stroll.
func TestOpeningAGuardedChestAngersPiglins(t *testing.T) {
	h := newHub(world.New(1))
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	x, y, z := 70, 70, 70
	for dx := -3; dx <= 3; dx++ { // clear line of sight
		for dz := -3; dz <= 3; dz++ {
			h.world.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			for dy := 0; dy < 3; dy++ {
				h.world.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
	pl.x, pl.y, pl.z = float64(x)+0.5, float64(y), float64(z)+1.5
	near := h.spawnMobIn(players, entityPiglin, 0, float64(x)+2.5, float64(y), float64(z)+1.5)
	far := h.spawnMobIn(players, entityPiglin, 0, float64(x)+40.5, float64(y), float64(z)+1.5)
	if near == nil || far == nil {
		t.Fatal("the piglins should have spawned")
	}

	h.world.SetBlock(x, y, z, worldgen.BlockBase("chest"))
	h.openChest(pl, x, y, z)

	if near.targetEID != pl.p.eid {
		t.Fatalf("a piglin watching should have turned, target=%d", near.targetEID)
	}
	if far.targetEID == pl.p.eid {
		t.Fatal("one forty blocks away should not have noticed")
	}
	// A block it has no claim on leaves it alone.
	other := h.spawnMobIn(players, entityPiglin, 0, float64(x)+2.5, float64(y), float64(z)-1.5)
	h.world.SetBlock(x+1, y, z, worldgen.BlockBase("furnace"))
	h.openChest(pl, x+1, y, z)
	if other != nil && other.targetEID == pl.p.eid {
		t.Fatal("a furnace is nobody's gold")
	}
}
