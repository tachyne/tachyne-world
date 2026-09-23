package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// catPad lays a loaded stone pad high in open air and returns its surface y.
func catPad(h *hub, cx, cz int) int {
	const fy = 180
	h.world.ForceLoad(cx, cz, 2)
	for x := cx - 20; x <= cx+20; x++ {
		for z := cz - 20; z <= cz+20; z++ {
			h.world.SetBlock(x, fy-1, z, worldgen.Stone)
			h.world.SetBlock(x, fy, z, worldgen.Air)
			h.world.SetBlock(x, fy+1, z, worldgen.Air)
		}
	}
	return fy
}

// villagersWithBeds puts n villagers on the pad, each holding a bed.
func villagersWithBeds(h *hub, players map[int32]*tracked, cx, cz, y, n int) {
	for i := 0; i < n; i++ {
		m := h.spawnMob(players, entityVillager, float64(cx+i)+0.5, float64(y), float64(cz)+0.5)
		m.bed, m.home = blockPos{cx + i, y, cz + 3}, blockPos{cx + i, y, cz + 3}
	}
}

// CatSpawner.spawnInVillage: near a village, more than four occupied homes
// within 48, and fewer than five cats in the box.
func TestVillageCatsNeedHomesAndStopAtFive(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	cx, cz := 4000, 4000
	y := catPad(h, cx, cz)

	villagersWithBeds(h, players, cx, cz, y, catHomesNeeded) // four homes
	if h.catSpawnAt(players, cx+8, y, cz+8) {
		t.Fatal("a cat spawned with only four occupied homes")
	}
	villagersWithBeds(h, players, cx, cz+6, y, 1) // a fifth
	if !h.catSpawnAt(players, cx+8, y, cz+8) {
		t.Fatal("no cat with five occupied homes and none about")
	}
	for i := 0; i < 20; i++ {
		h.catSpawnAt(players, cx+8, y, cz+8)
	}
	if n := h.countCatsInBox(cx+8, y, cz+8, catVillageRadius); n != catVillageMax {
		t.Fatalf("%d cats in the box, want the cap of %d", n, catVillageMax)
	}
}

// No village about (no claimed beds, workstations or bells), no cats; and
// nothing spawns where the chunks are not loaded.
func TestNoCatsInTheWilderness(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	cx, cz := 6000, 6000
	y := catPad(h, cx, cz)
	if h.catSpawnAt(players, cx, y, cz) {
		t.Error("a cat spawned with no village near")
	}
	villagersWithBeds(h, players, 9000, 9000, y, 6) // homes, but in unloaded land
	if h.catSpawnAt(players, 9000, y, 9004) {
		t.Error("a cat spawned in unloaded chunks")
	}
}

// CatSpawner.spawnInHut: inside a swamp hut with no cat within 16, one
// persistent cat — and not a second.
func TestSwampHutGetsOneCat(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	g := h.world.Gen()
	var hut worldgen.SwampHut
	for x := -20000; x <= 20000 && !hut.Exists; x += 512 {
		for z := -20000; z <= 20000 && !hut.Exists; z += 512 {
			hut = g.SwampHutIn(x, z)
		}
	}
	if !hut.Exists {
		t.Skip("no swamp hut on this seed in reach")
	}
	x, y, z := hut.Home()
	h.world.ForceLoad(x, z, 2)
	if !hut.Contains(x, y, z) {
		t.Fatalf("the hut's home %d,%d,%d is not inside its piece", x, y, z)
	}
	if !h.catSpawnAt(players, x, y, z) {
		t.Fatal("no cat spawned in an empty swamp hut")
	}
	var cat *mob
	for _, m := range h.mobs {
		if m.etype == entityCat {
			cat = m
		}
	}
	if cat == nil || !cat.persistent {
		t.Fatal("the hut's cat should be persistent")
	}
	if h.catSpawnAt(players, x, y, z) {
		t.Error("a hut that has its cat got a second")
	}
}
