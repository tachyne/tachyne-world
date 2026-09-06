package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func lodeHub(t *testing.T) (*hub, *tracked, map[int32]*tracked) {
	t.Helper()
	h := newHub(world.New(1))
	pl := riderAt(1, 11.5, 70, 10.5)
	pl.adv = advState{}
	players := map[int32]*tracked{1: pl}
	h.playersRef = players
	h.world.SetBlock(10, 70, 10, lodestoneBlock)
	return h, pl, players
}

// A lone survival compass on a lodestone becomes a lodestone compass in
// place, pointing at the block; the advancement is granted.
func TestCompassLocksOntoLodestone(t *testing.T) {
	h, pl, players := lodeHub(t)
	give(pl, itemCompass)

	h.onUseLodestone(players, evUseLodestone{eid: 1, x: 10, y: 70, z: 10, slot: 0})

	got := pl.inv.slots[0]
	want := lodeTracker{has: true, target: true, x: 10, y: 70, z: 10, dim: 0}
	if got.item != itemCompass || got.count != 1 || got.lode != want {
		t.Fatalf("held stack %+v, want one compass tracking %+v", got, want)
	}
	if !pl.adv.done(advByID["minecraft:adventure/use_lodestone"]) {
		t.Error("use_lodestone should be granted")
	}
	// Not a lodestone: nothing happens.
	give(pl, itemCompass)
	h.world.SetBlock(10, 70, 10, worldgen.Stone)
	h.onUseLodestone(players, evUseLodestone{eid: 1, x: 10, y: 70, z: 10, slot: 0})
	if pl.inv.slots[0].lode.has {
		t.Error("a compass on stone must not gain a tracker")
	}
}

// A STACK of compasses yields one lodestone compass and pays one plain one.
func TestCompassStackSplitsOffOneLodestoneCompass(t *testing.T) {
	h, pl, players := lodeHub(t)
	give(pl, itemCompass)
	pl.inv.slots[0].count = 5

	h.onUseLodestone(players, evUseLodestone{eid: 1, x: 10, y: 70, z: 10, slot: 0})

	if pl.inv.slots[0].count != 4 || pl.inv.slots[0].lode.has {
		t.Fatalf("source stack %+v, want 4 plain compasses", pl.inv.slots[0])
	}
	found := false
	for _, s := range pl.inv.slots {
		if s.item == itemCompass && s.count == 1 && s.lode.has && s.lode.target {
			found = true
		}
	}
	if !found {
		t.Error("no single lodestone compass landed in the inventory")
	}
}

// Once the lodestone is gone the compass forgets its target within a second
// (the component stays: it is still a lodestone compass, needle spinning).
func TestCompassForgetsRemovedLodestone(t *testing.T) {
	h, pl, players := lodeHub(t)
	give(pl, itemCompass)
	h.onUseLodestone(players, evUseLodestone{eid: 1, x: 10, y: 70, z: 10, slot: 0})

	h.lodestoneTick(players)
	if !pl.inv.slots[0].lode.target {
		t.Fatal("the target was dropped while the lodestone still stands")
	}
	h.world.SetBlock(10, 70, 10, worldgen.Air)
	h.lodestoneTick(players)
	got := pl.inv.slots[0].lode
	if got.target || !got.has {
		t.Fatalf("after removal: %+v, want has=true target=false", got)
	}
	// A compass pointing into ANOTHER dimension is left alone (tick only
	// checks the level it is in).
	pl.inv.slots[0].lode = lodeTracker{has: true, target: true, x: 1, y: 2, z: 3, dim: dimNether}
	h.lodestoneTick(players)
	if !pl.inv.slots[0].lode.target {
		t.Error("a nether target must not be verified from the overworld")
	}
}

// The tracker rides the persisted row, the wire component, and a drop.
func TestLodestoneTrackerRoundTrips(t *testing.T) {
	st := invStack{item: itemCompass, count: 1, lode: lodeTracker{has: true, target: true, x: -5, y: 64, z: 9, dim: dimEnd}}
	if back := unpackStack(packStack(st)); back != st {
		t.Errorf("stackRow round trip %+v, want %+v", back, st)
	}
	spinning := invStack{item: itemCompass, count: 1, lode: lodeTracker{has: true}}
	if back := unpackStack(packStack(spinning)); back != spinning {
		t.Errorf("targetless round trip %+v, want %+v", back, spinning)
	}
	if unpackLode(packLode(lodeTracker{})) != (lodeTracker{}) {
		t.Error("a plain compass must pack to nothing")
	}
	// Two compasses with different targets never merge into one stack.
	other := st
	other.lode.x = 6
	if st.sameExtras(other) {
		t.Error("different lodestone targets must not be mergeable")
	}
	// The component reaches the wire: lodestone_tracker with the end's key.
	comps := stackComponents(st)
	if len(comps) == 0 || comps[0] != 1 {
		t.Fatalf("expected exactly one component, got %v", comps)
	}
	if !containsBytes(comps, []byte("minecraft:the_end")) {
		t.Errorf("component bytes lack the dimension key: %x", comps)
	}
	// Dropped and picked back up, the tracker survives.
	h := newHub(world.New(1))
	it := h.spawnItemIn(map[int32]*tracked{}, 0, st.item, 1, 0, 70, 0)
	it.lode = st.lode
	if it.stack() != st {
		t.Errorf("dropped item stack %+v, want %+v", it.stack(), st)
	}
}

func containsBytes(hay, needle []byte) bool {
	for i := 0; i+len(needle) <= len(hay); i++ {
		if string(hay[i:i+len(needle)]) == string(needle) {
			return true
		}
	}
	return false
}
