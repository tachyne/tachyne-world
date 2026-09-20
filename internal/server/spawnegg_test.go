package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// SpawnEggItem.useOn: right-clicking a block with a spawn egg puts the mob on
// the face that was clicked. This did nothing at all — eggs worked only on a
// spawner or out of a dispenser — which a creative player notices at once.
func TestSpawnEggPlacesItsMob(t *testing.T) {
	w := world.New(61)
	h := newHub(w)
	pl := testTracked()
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	pl.gamemode = gmCreative
	pl.dim, pl.x, pl.y, pl.z = 0, 0.5, 180, 0.5

	stone := worldgen.BlockBase("stone")
	w.SetBlock(2, 179, 0, stone)

	count := func(et int) int {
		n := 0
		for _, m := range h.mobs {
			if m.etype == et {
				n++
			}
		}
		return n
	}

	// A cat egg on the top face of a stone block: the cat stands on it.
	pl.inv.slots[0] = invStack{item: itemByName["cat_spawn_egg"], count: 1}
	pl.p.setHotbarSlot(0, int32(itemByName["cat_spawn_egg"]))
	h.useSpawnEgg(players, evSpawnEgg{eid: pl.p.eid, x: 2, y: 179, z: 0, face: 1})
	if count(entityCat) != 1 {
		t.Fatalf("a cat spawn egg on stone made %d cats, want 1", count(entityCat))
	}
	var cat *mob
	for _, m := range h.mobs {
		if m.etype == entityCat {
			cat = m
		}
	}
	if int(cat.y) != 180 {
		t.Errorf("the cat stands at y=%v, want on top of the block at 180", cat.y)
	}
	if !cat.persistent {
		t.Error("an egg-spawned mob is placed, not natural: it must not despawn")
	}
	// Creative keeps the egg.
	if pl.inv.slots[0].count != 1 {
		t.Errorf("creative must not spend the egg, count %d", pl.inv.slots[0].count)
	}

	// Clicking a replaceable block spawns INSIDE it rather than on its face.
	w.SetBlock(5, 180, 0, worldgen.BlockBase("short_grass"))
	pl.inv.slots[0] = invStack{item: itemByName["pig_spawn_egg"], count: 1}
	pl.p.setHotbarSlot(0, int32(itemByName["pig_spawn_egg"]))
	h.useSpawnEgg(players, evSpawnEgg{eid: pl.p.eid, x: 5, y: 180, z: 0, face: 1})
	var pig *mob
	for _, m := range h.mobs {
		if m.etype == entityPig {
			pig = m
		}
	}
	if pig == nil {
		t.Fatal("a pig spawn egg on grass made no pig")
	}
	if int(pig.y) != 180 {
		t.Errorf("the pig stands at y=%v, want inside the grass at 180", pig.y)
	}

	// A survival player pays for it.
	pl.gamemode = gmSurvival
	pl.inv.slots[0] = invStack{item: itemByName["cow_spawn_egg"], count: 3}
	pl.p.setHotbarSlot(0, int32(itemByName["cow_spawn_egg"]))
	h.useSpawnEgg(players, evSpawnEgg{eid: pl.p.eid, x: 2, y: 179, z: 0, face: 1})
	if count(entityCow) != 1 {
		t.Fatalf("a cow spawn egg made %d cows, want 1", count(entityCow))
	}
	if pl.inv.slots[0].count != 2 {
		t.Errorf("survival spends one egg, %d left of 3", pl.inv.slots[0].count)
	}
}
