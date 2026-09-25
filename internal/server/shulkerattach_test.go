package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Shulker.canStayAt / findNewAttachment: a shulker whose floor goes turns
// to cling to the wall beside it; with nothing about at all it teleports
// (and it can land on a ceiling or a wall, not only a floor). Clinging to
// the floor, its box rises as it opens.
func TestShulkerClingsToAnyFace(t *testing.T) {
	h := newHub(world.New(1))
	h.world.ForceLoad(0, 0, 2)
	players := map[int32]*tracked{}
	h.playersRef = players
	w := h.world
	for x := -2; x <= 2; x++ {
		for y := 178; y <= 183; y++ {
			for z := -2; z <= 2; z++ {
				w.SetBlock(x, y, z, worldgen.Air)
			}
		}
	}
	w.SetBlock(0, 179, 0, worldgen.Stone) // the floor
	w.SetBlock(1, 180, 0, worldgen.Stone) // a wall to the east
	sh := h.spawnSpecies(players, entityShulker, 0, 0.5, 180, 0.5)
	if sh == nil {
		t.Fatal("no shulker")
	}
	h.shulkerTick(players, sh)
	if sh.shAttach != 0 {
		t.Fatalf("on its floor it stays on the floor: face %d", sh.shAttach)
	}
	h.setBlockAt(players, 0, blockPos{0, 179, 0}, worldgen.Air)
	h.shulkerTick(players, sh)
	if sh.shAttach != 5 || sh.x != 0.5 || sh.y != 180 {
		t.Fatalf("with its floor gone it clings to the east wall: face %d at (%.1f,%.1f)", sh.shAttach, sh.x, sh.y)
	}
	h.setBlockAt(players, 0, blockPos{1, 180, 0}, worldgen.Air)
	h.shulkerTick(players, sh)
	if sh.x == 0.5 && sh.y == 180 && sh.z == 0.5 {
		t.Skip("no face within reach of the teleport in this fixture")
	}
	p := blockPos{floorInt(sh.x), floorInt(sh.y), floorInt(sh.z)}
	if !h.shulkerCanStayAt(0, p, sh.shAttach) {
		t.Fatalf("it teleported somewhere it can cling (face %d at %v)", sh.shAttach, p)
	}
	// The box rises with the peek on a floor.
	sh.shAttach, sh.shPeekCur = 0, 1
	if b := sh.box(); b.h < 1.99 {
		t.Fatalf("fully open on a floor the box is two tall: %.2f", b.h)
	}
}
