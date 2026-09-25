package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// flatFloor lays a stone slab of radius r at y-1 with clear air above.
func flatFloor(w *world.World, x, y, z, r int) {
	for dx := -r; dx <= r; dx++ {
		for dz := -r; dz <= r; dz++ {
			w.SetBlock(x+dx, y-1, z+dz, worldgen.Stone)
			for dy := 0; dy <= 3; dy++ {
				w.SetBlock(x+dx, y+dy, z+dz, worldgen.Air)
			}
		}
	}
}

// A pushed item slides, ground friction stops it within a fraction of a
// block, and it never leaves the floor.
func TestItemSlidesAndStops(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 1000, 180, 1000
	flatFloor(h.world, x, y, z, 4)
	it := h.spawnItemAt(players, 0, itemByName["cobblestone"], 1, float64(x)+0.5, float64(y), float64(z)+0.5, 0.1, 0, 0)
	for i := 0; i < 60; i++ {
		h.tickItems(players)
		if it.y != float64(y) {
			t.Fatalf("a sliding item stays on its floor, y=%v at tick %d", it.y, i)
		}
	}
	if it.vx != 0 || it.x <= float64(x)+0.55 || it.x > float64(x)+1.0 {
		t.Fatalf("item should have slid a fraction of a block and stopped: x=%v vx=%v", it.x, it.vx)
	}
	// Into a wall: the push dies at the wall.
	h.world.SetBlock(x+2, y, z, worldgen.Stone)
	it.vx = 0.5
	for i := 0; i < 10; i++ {
		h.tickItems(players)
	}
	if it.x >= float64(x+2) {
		t.Fatalf("item went through a wall: x=%v", it.x)
	}
}

// An item pushed off a ledge falls to the floor below; an item whose floor
// is removed falls too.
func TestItemFallsOffLedgeAndWhenFloorGoes(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 1020, 180, 1020
	flatFloor(h.world, x, y, z, 4)
	for dx := 1; dx <= 4; dx++ { // a drop to the east: floor two lower
		h.world.SetBlock(x+dx, y-1, z, worldgen.Air)
		h.world.SetBlock(x+dx, y-2, z, worldgen.Air)
		h.world.SetBlock(x+dx, y-3, z, worldgen.Stone)
	}
	it := h.spawnItemAt(players, 0, itemByName["cobblestone"], 1, float64(x)+0.5, float64(y), float64(z)+0.5, 0.3, 0, 0)
	for i := 0; i < 80; i++ {
		h.tickItems(players)
	}
	if it.y != float64(y-2) || it.x < float64(x+1) {
		t.Fatalf("item should have tumbled off the ledge to the lower floor, got (%v,%v)", it.x, it.y)
	}
	// Its floor vanishes: it drops onto the next.
	h.world.SetBlock(int(math.Floor(it.x)), y-3, z, worldgen.Air)
	h.world.SetBlock(int(math.Floor(it.x)), y-4, z, worldgen.Air)
	h.world.SetBlock(int(math.Floor(it.x)), y-5, z, worldgen.Stone)
	for i := 0; i < 40; i++ {
		h.tickItems(players)
	}
	if it.y != float64(y-4) {
		t.Fatalf("item should fall when its floor is removed, y=%v", it.y)
	}
}

// Flowing water carries an item downstream; still water does not.
func TestWaterCurrentCarriesItem(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 1040, 180, 1040
	h.world.ForceLoad(x, z, 1)
	flatFloor(h.world, x, y, z, 8)
	for dx := -1; dx <= 8; dx++ { // a walled channel running east
		h.world.SetBlock(x+dx, y, z-1, worldgen.Stone)
		h.world.SetBlock(x+dx, y, z+1, worldgen.Stone)
	}
	h.world.SetBlock(x-1, y, z, worldgen.Stone) // closed west end
	h.world.SetBlock(x, y, z, worldgen.WaterBase)
	h.scheduleIn(0, blockPos{x, y, z}, 1)
	runTicks(h, players, 1, 80) // let the stream run out
	if !worldgen.IsWater(h.world.At(x+5, y, z)) {
		t.Fatalf("the channel should have filled: %d at +5", h.world.At(x+5, y, z))
	}
	it := h.spawnItemAt(players, 0, itemByName["stick"], 1, float64(x)+1.5, float64(y), float64(z)+0.5, 0, 0, 0)
	still := h.spawnItemAt(players, 0, itemByName["stick"], 1, float64(x)+0.5, float64(y)+2, float64(z)+0.5, 0, 0, 0)
	h.world.SetBlock(x, y+2, z, worldgen.WaterBase) // a lone source above the closed end: still
	h.world.SetBlock(x, y+3, z, worldgen.Stone)
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if dx != 0 || dz != 0 {
				h.world.SetBlock(x+dx, y+2, z+dz, worldgen.Stone)
			}
		}
	}
	for i := 0; i < 120; i++ {
		h.tickItems(players)
	}
	if it.x < float64(x)+4 {
		t.Fatalf("the stream should have carried the item east: x=%v (start %v)", it.x, float64(x)+1.5)
	}
	if math.Abs(still.x-(float64(x)+0.5)) > 0.01 || math.Abs(still.z-(float64(z)+0.5)) > 0.01 {
		t.Fatalf("still water must not move an item: (%v,%v)", still.x, still.z)
	}
}

// Two blocks' drops, popped side by side, come to rest close enough to
// merge into one stack.
func TestNeighbouringBlockDropsMerge(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 1060, 180, 1060
	flatFloor(h.world, x, y, z, 4)
	merged := 0
	for trial := 0; trial < 20; trial++ {
		for eid := range h.items {
			delete(h.items, eid)
		}
		h.spawnBlockDrop(players, 0, itemByName["cobblestone"], 1, x, y, z)
		h.spawnBlockDrop(players, 0, itemByName["cobblestone"], 1, x+1, y, z)
		for i := 0; i < 60; i++ {
			h.tickItems(players)
			h.updateItems(players)
		}
		if len(h.items) == 1 {
			merged++
		}
	}
	if merged < 5 {
		t.Fatalf("adjacent drops should often land close enough to merge: %d of 20 trials", merged)
	}
}

// A toss leaves from the eyes along the look and lands a block or two out.
func TestTossArcsForward(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	x, y, z := 1080, 180, 1080
	flatFloor(h.world, x, y, z, 6)
	pl.x, pl.y, pl.z, pl.yaw, pl.pitch = float64(x)+0.5, float64(y), float64(z)+0.5, 0, 0 // looking south
	pl.dim = 0
	h.tossItem(players, pl, invStack{item: itemByName["stick"], count: 1})
	var it *itemEntity
	for _, i := range h.items {
		it = i
	}
	if it == nil || it.y <= float64(y)+1 {
		t.Fatalf("toss should start near the eyes, got %+v", it)
	}
	for i := 0; i < 80; i++ {
		h.tickItems(players)
	}
	if it.y != float64(y) || it.z < float64(z)+1.5 || it.z > float64(z)+4 {
		t.Fatalf("toss should land a block or two ahead on the floor, got (%v,%v,%v)", it.x, it.y, it.z)
	}
}

// ItemEntity.setUnderLavaMovement: a fire-resistant drop in lava is lifted
// as in water (it does not sink to the bottom) and rides the surface.
func TestNetheriteFloatsUpThroughLava(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	x, y, z := 1000, 180, 1000
	flatFloor(h.world, x, y, z, 2)
	for dy := 0; dy < 3; dy++ {
		h.world.SetBlock(x, y+dy, z, worldgen.LavaBase)
	}
	it := h.spawnItemAt(players, 0, itemByName["netherite_ingot"], 1, float64(x)+0.5, float64(y)+1.5, float64(z)+0.5, 0, 0, 0)
	for i := 0; i < 600; i++ {
		h.tickItems(players)
	}
	if h.items[it.eid] == nil {
		t.Fatal("netherite does not burn")
	}
	if it.y < float64(y)+3-1e-9 {
		t.Fatalf("netherite in lava rises to the surface: y=%.3f (surface %d)", it.y, y+3)
	}
}

// ItemEntity.age: a drop's age is its own and is saved, so a restart does
// not give it a fresh five minutes; merging keeps the younger age.
func TestDroppedItemAgeSurvivesRestart(t *testing.T) {
	h := newHub(world.New(1))
	none := map[int32]*tracked{}
	it := h.spawnItemIn(none, 0, itemByName["cobblestone"], 1, 10, 70, 10)
	it.age = 5900
	saved := h.snapshotItems()
	h2 := newHub(world.New(1))
	h2.restoreItems(saved)
	var back *itemEntity
	for _, e := range h2.items {
		back = e
	}
	if back == nil || back.age != 5900 {
		t.Fatalf("the restored drop keeps its age: %+v", back)
	}
	back.age = itemDespawnTicks
	h2.updateItems(none)
	if len(h2.items) != 0 {
		t.Fatal("a drop past its six thousand ticks despawns")
	}
	// Merging: the younger age wins.
	a := h.spawnItemIn(none, 0, itemByName["dirt"], 1, 30.5, 70, 30.5)
	b := h.spawnItemIn(none, 0, itemByName["dirt"], 1, 30.6, 70, 30.5)
	a.y, b.y = 70, 70
	a.age, b.age = 4000, 100
	h.updateItems(none)
	var left *itemEntity
	for _, e := range h.items {
		if e.item == int32(itemByName["dirt"]) {
			left = e
		}
	}
	if left == nil || left.count != 2 || left.age != 100 {
		t.Fatalf("merged drop: %+v", left)
	}
}

// GiveCommand: what does not fit is dropped at once for the receiver alone
// (ItemEntity.target); another player cannot pick it up.
func TestGiveOverflowIsTheReceiversAlone(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 1)
	flatFloor(h.world, 0, 180, 0, 3)
	a, b := survPlayer(h), survPlayer(h)
	a.p.eid, b.p.eid = 101, 102
	a.p.uuid[15], b.p.uuid[15] = 1, 2
	a.x, a.y, a.z = 0.5, 180, 0.5
	b.x, b.y, b.z = 0.5, 180, 0.5
	players := map[int32]*tracked{a.p.eid: a, b.p.eid: b}
	stone := int32(itemByName["stone"])
	for i := range a.inv.slots[:36] {
		a.inv.slots[i] = invStack{item: int32(itemByName["dirt"]), count: 64}
	}
	h.giveTo(players, a, stone, 5)
	var drop *itemEntity
	for _, it := range h.items {
		drop = it
	}
	if drop == nil || drop.owner != a.p.uuid {
		t.Fatalf("the overflow is dropped for the receiver: %+v", drop)
	}
	h.pickupItems(map[int32]*tracked{b.p.eid: b})
	if h.items[drop.eid] == nil {
		t.Fatal("another player picked up the receiver's overflow")
	}
	a.inv.slots[0] = invStack{}
	h.pickupItems(players)
	if h.items[drop.eid] != nil {
		t.Fatal("the receiver picks it up at once")
	}
}
