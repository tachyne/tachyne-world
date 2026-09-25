package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// piglinWalk is a floating stone walk, fifty blocks long, with air above.
func piglinWalk(t *testing.T) (*hub, *world.World, map[int32]*tracked, int, int, int) {
	t.Helper()
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.world.ForceLoad(0, 0, 3)
	for x := -24; x <= 24; x++ {
		for z := -8; z <= 8; z++ {
			h.world.SetBlock(x, 179, z, worldgen.BlockBase("stone"))
			for y := 180; y < 320; y++ { // nothing overhead: the walk heights read the column top
				h.world.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	return h, h.world, players, 0, 180, 0
}

// A brute remembers where it was made and, idle, walks back to it.
func TestPiglinBruteKeepsHome(t *testing.T) {
	// Level ground in seed 1's terrain (x 80..128, z -328..-312): the walk
	// is planned over the real surface.
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	h.world.ForceLoad(104, -320, 3)
	feet := func(x int) float64 { return float64(h.world.MobFeet(x, -320)) }
	b := h.spawnHostileY(players, entityPiglinBrute, 120.5, feet(120), -319.5)
	if b == nil {
		t.Fatal("no brute")
	}
	b.immuneZombify = true
	if b.home != (blockPos{120, int(feet(120)), -320}) {
		t.Fatalf("the brute's home is %v, want where it spawned", b.home)
	}
	b.x, b.y = 88.5, feet(88) // carried off, thirty-two blocks
	closest := math.Inf(1)
	for i := 0; i < 600; i++ {
		h.tick.Add(mobMoveInterval)
		h.updateMobs(players)
		closest = math.Min(closest, math.Hypot(b.x-120.5, b.z+319.5))
	}
	if closest > 4 {
		t.Fatalf("the brute never made it home (closest %.1f blocks)", closest)
	}
}

// Piglins and brutes open the wooden doors in their way.
func TestPiglinsOpenDoors(t *testing.T) {
	for _, et := range []int{entityPiglin, entityPiglinBrute} {
		h, nw, players, x, y, z := piglinWalk(t)
		lower, upper := worldgen.BlockBase("oak_door")+27, worldgen.BlockBase("oak_door")+19
		nw.SetBlock(x+1, y, z, lower)
		nw.SetBlock(x+1, y+1, z, upper)
		m := h.spawnSpecies(players, et, 0, float64(x)+0.5, float64(y), float64(z)+0.5)
		if m == nil {
			t.Fatal("no mob")
		}
		for i := 0; i < 2; i++ {
			h.tick.Add(mobMoveInterval)
			h.updateMobs(players)
		}
		if worldgen.IsClosedDoor(nw.At(x+1, y, z)) {
			t.Errorf("%s: the door beside it stayed shut", speciesTable[et].name)
		}
	}
}
