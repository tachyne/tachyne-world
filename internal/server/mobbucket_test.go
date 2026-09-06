package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A water bucket on an axolotl scoops it: the bucket becomes an axolotl
// bucket, the mob is gone, and "The Cutest Predator" is granted.
func TestWaterBucketScoopsAxolotl(t *testing.T) {
	h := newHub(world.New(1))
	pl := riderAt(1, 30, 70, 30)
	pl.adv = advState{}
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	m := h.spawnSpecies(players, entityAxolotl, 0, 30.9, 70, 30.9)
	give(pl, itemBucketH2O)

	if !h.tryBucketMob(players, pl, m) {
		t.Fatal("a water bucket should pick up an axolotl")
	}
	if got := pl.inv.slots[0].item; got != itemByName["axolotl_bucket"] {
		t.Fatalf("held item %d, want axolotl_bucket", got)
	}
	if h.mobs[m.eid] != nil {
		t.Error("the bucketed axolotl should be discarded")
	}
	if !pl.adv.done(advByID["minecraft:husbandry/axolotl_in_a_bucket"]) {
		t.Error("axolotl_in_a_bucket should be granted")
	}
	// Only a WATER bucket scoops, and only bucketable species.
	cow := h.spawnSpecies(players, entityCow, 0, 30.9, 70, 30.9)
	give(pl, itemBucketH2O)
	if h.tryBucketMob(players, pl, cow) {
		t.Error("a cow is not bucketable")
	}
	fish := h.spawnSpecies(players, entityCod, 0, 30.9, 70, 30.9)
	give(pl, itemBucket)
	if h.tryBucketMob(players, pl, fish) {
		t.Error("an empty bucket must not scoop a fish")
	}
}

// Pouring a tadpole bucket places the water and releases a persistent
// tadpole in the cell; the bucket is left empty.
func TestMobBucketReleasesMob(t *testing.T) {
	h := newHub(world.New(1))
	pl := riderAt(1, 30, 70, 30)
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	give(pl, itemByName["tadpole_bucket"])
	h.world.SetBlock(32, 70, 30, worldgen.Air)

	h.bucketEmpty(players, pl, 0, 32, 70, 30)

	if got := h.world.At(32, 70, 30); got != worldgen.WaterBase {
		t.Fatalf("cell after pouring is %d, want a water source", got)
	}
	if pl.inv.slots[0].item != itemBucket {
		t.Errorf("held item %d, want an empty bucket", pl.inv.slots[0].item)
	}
	var found *mob
	for _, m := range h.mobs {
		if m.etype == entityTadpole {
			found = m
		}
	}
	if found == nil {
		t.Fatal("no tadpole was released")
	}
	if !found.fromBucket {
		t.Error("a released mob must be flagged fromBucket (persistent)")
	}
	if found.x != 32.5 || found.z != 30.5 {
		t.Errorf("tadpole at (%.1f, %.1f), want the cell centre (32.5, 30.5)", found.x, found.z)
	}
	// It survives the despawn sweep with nobody near it.
	found.x, found.z = 5000, 5000
	h.despawnSweep(players)
	if h.mobs[found.eid] == nil {
		t.Error("a bucket-released mob despawned; it must be persistent")
	}
}
