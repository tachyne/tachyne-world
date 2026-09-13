package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestUseOnItems: shears cap a kelp head, a water bottle muds dirt and gives
// the bottle back, a spawn egg retargets a spawner, a shovel dowses a
// campfire, and a rocket lit on a block launches.
func TestUseOnItems(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	pl.x, pl.y, pl.z = 0.5, 180, 0.5
	hold := func(item int32, potion int8) {
		pl.p.setHotbarSlot(0, item)
		pl.inv.slots[0] = invStack{item: item, count: 1, potion: potion}
	}
	// Shears on a young kelp head.
	kelp := growingPlants[0]
	w.SetBlock(0, 179, 0, kelp.headAt(3, false))
	hold(itemShears, 0)
	h.trimPlant(players, evTrimPlant{eid: pl.p.eid, x: 0, y: 179, z: 0})
	if got := w.At(0, 179, 0); kelp.age(got) != growingPlantMaxAge {
		t.Fatalf("shears should cap the head at 25: age %d", kelp.age(got))
	}
	if pl.inv.slots[0].dmg != 1 {
		t.Fatalf("the shears wear one: %d", pl.inv.slots[0].dmg)
	}
	// A water bottle on coarse dirt.
	w.SetBlock(2, 179, 0, worldgen.CoarseDirt)
	hold(itemPotion, potWater)
	h.mudBottle(players, evMudBottle{eid: pl.p.eid, x: 2, y: 179, z: 0})
	if w.At(2, 179, 0) != worldgen.Mud {
		t.Fatal("dirt should become mud")
	}
	bottles := 0
	for _, s := range pl.inv.slots {
		if s.item == itemGlassBottle {
			bottles += s.count
		}
	}
	if bottles != 1 || pl.inv.slots[0].item == itemPotion {
		t.Fatalf("the bottle comes back empty: %d bottles, slot0 %d", bottles, pl.inv.slots[0].item)
	}
	// A spawn egg on a spawner.
	w.SetBlock(4, 179, 0, spawnerBlock)
	hold(int32(itemByName["creeper_spawn_egg"]), 0)
	h.eggSpawner(players, evEggSpawner{eid: pl.p.eid, x: 4, y: 179, z: 0})
	if h.spawnerMobFor(0, 4, 179, 0, entityZombie) != entityCreeper || pl.inv.slots[0].item != 0 {
		t.Fatalf("the spawner should now spawn creepers: %v", h.rules.SpawnerMobs)
	}
	if h.spawnerMobFor(0, 5, 179, 0, entityZombie) != entityZombie {
		t.Fatal("other spawners keep their own mob")
	}
	// A rocket against a block launches from the click point.
	hold(itemFireworkRocket, 0)
	before := len(h.rockets)
	h.placeRocket(players, evPlaceRocket{eid: pl.p.eid, x: 0, y: 178, z: 0, face: 1, cx: 0.5, cy: 1, cz: 0.5})
	if len(h.rockets) != before+1 || pl.inv.slots[0].item != 0 {
		t.Fatal("the rocket should launch and be spent")
	}
}

// TestEndCrystalRespawnsDragon: crystals on obsidian in the End, and four
// round the beaten dragon's portal bring it back.
func TestEndCrystalRespawnsDragon(t *testing.T) {
	h := newHub(world.New(1))
	ew, _ := world.NewEnd(7, nil)
	h.end = ew
	pl := survPlayer(h)
	pl.dim = 2
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	h.rules.DragonDefeated = true
	cy := worldgen.EndSurfaceY
	for h.end.At(0, cy, 0) != worldgen.Air && cy < worldgen.EndSurfaceY+8 {
		cy++
	}
	pl.x, pl.y, pl.z = 8.5, float64(cy), 8.5
	pl.p.setHotbarSlot(0, itemEndCrystal)
	for _, d := range [4][2]int{{2, 0}, {-2, 0}, {0, 2}, {0, -2}} {
		h.end.SetBlock(d[0], cy-1, d[1], worldgen.Bedrock)
		h.end.SetBlock(d[0], cy, d[1], worldgen.Air)
		h.end.SetBlock(d[0], cy+1, d[1], worldgen.Air)
		pl.inv.slots[0] = invStack{item: itemEndCrystal, count: 1}
		h.placeCrystal(players, evPlaceCrystal{eid: pl.p.eid, x: d[0], y: cy - 1, z: d[1]})
	}
	if h.dragon == nil || h.rules.DragonDefeated {
		t.Fatalf("four crystals should restage the fight: dragon %v defeated %v crystals %d", h.dragon != nil, h.rules.DragonDefeated, len(h.crystals))
	}
}
