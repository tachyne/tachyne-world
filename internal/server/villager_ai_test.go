package server

import (
	"testing"

	"tachyne/internal/world"
	"tachyne/internal/worldgen"
)

// placeClosedDoor stamps a closed oak door standing on the natural surface at
// (x,z) with clear headroom, and returns the door's lower-half y (the mob-feet
// cell). Placing it on real ground keeps MobFeet landing ON the door.
func placeClosedDoor(w *world.World, x, z int) int {
	fy := w.MobFeet(x, z) // the actual feet cell — the door's lower half sits here
	for y := fy; y < fy+4; y++ {
		w.SetBlock(x, y, z, worldgen.Air) // headroom so nothing perches above
	}
	w.SetBlock(x, fy, z, (worldgen.BlockBase("oak_door") + 27))   // oak_door lower, closed
	w.SetBlock(x, fy+1, z, (worldgen.BlockBase("oak_door") + 19)) // oak_door upper, closed
	return fy
}

// TestVillagerOpensAndClosesDoor: a villager beside a closed wooden door opens
// it (both halves), and once it walks away the hub shuts it after the grace.
func TestVillagerOpensAndClosesDoor(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}

	const dx, dz = 6, 5
	dy := placeClosedDoor(w, dx, dz)

	// Villager one cell from the door, at the door's feet height.
	m := h.spawnMob(players, entityVillager, 5.5, float64(dy), 5.5)
	m.usesDoors = true
	m.behavior = villagerBehavior{}
	m.home = blockPos{5, dy, 5}

	h.villagerDoors(players, m)
	if worldgen.IsClosedDoor(w.At(dx, dy, dz)) {
		t.Fatal("villager beside the door should have opened its lower half")
	}
	if worldgen.IsClosedDoor(w.At(dx, dy+1, dz)) {
		t.Fatal("both door halves should open together")
	}
	if _, ok := h.openDoors[blockPos{dx, dy, dz}]; !ok {
		t.Fatal("opened door should be recorded for auto-close")
	}

	// Walk the villager well away; the door must NOT close before the grace.
	m.x, m.z = 200, 200
	h.updateOpenDoors(players)
	if !boolProp(w.At(dx, dy, dz), "open") {
		t.Fatal("door closed before the grace window elapsed")
	}
	// Past the grace, with no villager near, it shuts.
	h.tick.Store(doorCloseGrace + 1)
	h.updateOpenDoors(players)
	if boolProp(w.At(dx, dy, dz), "open") {
		t.Fatal("door should auto-close once the villager left and grace passed")
	}
	if boolProp(w.At(dx, dy+1, dz), "open") {
		t.Fatal("upper half should close with the lower")
	}
}

// TestDoorPatherRoutesThroughClosedDoor: A* refuses a closed wooden door for a
// normal mob (it's a wall) but the door-aware pather treats it as passable, so a
// door-using villager can plan a route out of a doored room.
func TestDoorPatherRoutesThroughClosedDoor(t *testing.T) {
	w := world.New(1)
	const dx, dz = 5, 5
	dy := placeClosedDoor(w, dx, dz)

	dp := doorPather{w}
	if !w.TallObstacle(dx, dz) {
		t.Fatal("a closed door should read as a wall to the plain pather")
	}
	if dp.TallObstacle(dx, dz) {
		t.Fatal("the door-aware pather must treat a closed wooden door as passable")
	}
	// Iron door stays a wall even for the door pather.
	w.SetBlock(dx, dy, dz, (worldgen.BlockBase("iron_door") + 10)) // iron_door lower, closed
	if !dp.TallObstacle(dx, dz) {
		t.Fatal("iron doors are not villager-openable — must stay a wall")
	}
}

// TestVillagerSegment: the day clock maps to the right schedule segment.
func TestVillagerSegment(t *testing.T) {
	cases := []struct {
		t    uint64
		want int
	}{
		{0, vsRoam},       // sunrise — up and about
		{3000, vsWork},    // morning
		{10000, vsGather}, // midday
		{11500, vsRoam},   // afternoon
		{13000, vsSleep},  // night
		{23000, vsSleep},  // deep night
	}
	for _, c := range cases {
		if got := villagerSegment(c.t); got != c.want {
			t.Errorf("villagerSegment(%d)=%d, want %d", c.t, got, c.want)
		}
	}
}

// TestVillagerScheduleGoals: the villager aims at its workstation during work
// hours and its bed at night (pathSteer records the goal it planned toward).
func TestVillagerScheduleGoals(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	m := &mob{etype: entityVillager, speed: 0.135, usesDoors: true, x: 20, z: 20,
		home: blockPos{20, 70, 20},
		bed:  blockPos{10, 70, 10}, work: blockPos{30, 70, 30}, meet: blockPos{50, 70, 50}}

	h.dayTime.Store(3000) // work hours
	villagerBehavior{}.steer(h, m)
	if m.pathGoal != [2]int{30, 30} {
		t.Errorf("work hours: planned toward %v, want the workstation {30,30}", m.pathGoal)
	}

	m.path = nil           // force a replan
	h.dayTime.Store(13000) // night
	villagerBehavior{}.steer(h, m)
	if m.pathGoal != [2]int{10, 10} {
		t.Errorf("night: planned toward %v, want the bed {10,10}", m.pathGoal)
	}

	m.path = nil
	h.dayTime.Store(10000) // midday gather
	villagerBehavior{}.steer(h, m)
	if m.pathGoal != [2]int{50, 50} {
		t.Errorf("midday: planned toward %v, want the meeting point {50,50}", m.pathGoal)
	}
}

// TestVillagerSleepsAtNight: a villager at its bed lies down at night (held
// still) and wakes at first light.
func TestVillagerSleepsAtNight(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	bed := blockPos{15, 70, 15}
	m := h.spawnMob(players, entityVillager, float64(bed.x)+0.5, float64(bed.y), float64(bed.z)+0.5)
	m.usesDoors, m.bed = true, bed

	h.dayTime.Store(14000) // night
	if !h.villagerSleep(players, m) || !m.sleeping {
		t.Fatal("a villager at its bed should fall asleep at night")
	}
	// Snapped onto the bed surface.
	if m.y != float64(bed.y)+bedSurface {
		t.Fatalf("sleeper should sit on the bed surface, y=%.3f", m.y)
	}
	h.dayTime.Store(200) // first light
	if h.villagerSleep(players, m) || m.sleeping {
		t.Fatal("a villager should wake at dawn")
	}
}

// TestVillagerFarFromBedStaysAwake: night alone doesn't sleep a villager still
// walking home — it must actually reach the bed first.
func TestVillagerFarFromBedStaysAwake(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}
	m := h.spawnMob(players, entityVillager, 100, 70, 100)
	m.usesDoors, m.bed = true, blockPos{15, 70, 15}
	h.dayTime.Store(14000)
	if h.villagerSleep(players, m) || m.sleeping {
		t.Fatal("a villager far from its bed must not teleport-sleep")
	}
}

// TestVillagerEscapesDooredRoom is the reported bug end-to-end: a villager boxed
// in a stone room with a single wooden door used to bounce forever (random
// wander never threaded the doorway). It must now path to the door, open it, and
// walk out — no player digging required.
func TestVillagerEscapesDooredRoom(t *testing.T) {
	w := world.New(1)
	h := newHub(w)
	players := map[int32]*tracked{}

	fy := w.MobFeet(3, 3)
	// Flat floor + cleared 2-high space over a 7×5 patch (room + exit corridor).
	for x := 0; x <= 8; x++ {
		for z := 1; z <= 5; z++ {
			w.SetBlock(x, fy-1, z, worldgen.Stone)
			w.SetBlock(x, fy, z, worldgen.Air)
			w.SetBlock(x, fy+1, z, worldgen.Air)
			w.SetBlock(x, fy+2, z, worldgen.Air)
		}
	}
	// Two-high stone walls boxing the interior x∈[2,4], z∈[2,4], with one door
	// on the east wall at (5,3). Everything x≥6 is the open exit corridor.
	wall := func(x, z int) {
		w.SetBlock(x, fy, z, worldgen.Stone)
		w.SetBlock(x, fy+1, z, worldgen.Stone)
	}
	for x := 1; x <= 5; x++ {
		wall(x, 1)
		wall(x, 5)
	}
	for z := 1; z <= 5; z++ {
		wall(1, z)
		wall(5, z)
	}
	w.SetBlock(5, fy, 3, (worldgen.BlockBase("oak_door") + 27))   // oak_door lower, closed — the only way out
	w.SetBlock(5, fy+1, 3, (worldgen.BlockBase("oak_door") + 19)) // oak_door upper, closed

	m := h.spawnMob(players, entityVillager, 3.5, float64(fy), 3.5)
	m.usesDoors = true
	m.behavior = villagerBehavior{}
	m.home = blockPos{7, fy, 3} // "home" is out in the corridor, pulling it out

	opened := false
	for i := 0; i < 800; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		h.updateOpenDoors(players)
		if boolProp(w.At(5, fy, 3), "open") {
			opened = true
		}
		if m.x >= 6 { // cleared the doorway into the corridor
			break
		}
	}
	if !opened {
		t.Fatal("the villager never opened the door")
	}
	if m.x < 6 {
		t.Fatalf("villager still boxed in at x=%.1f — it should have walked out through the door", m.x)
	}
}
