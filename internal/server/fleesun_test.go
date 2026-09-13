package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestSkeletonFleesSun: a burning, idle, bare-headed skeleton in daylight
// heads for a roofed dim spot nearby; one with a target, or in the dark,
// stays put.
func TestSkeletonFleesSun(t *testing.T) {
	h := newHub(world.New(1))
	pl := survPlayer(h)
	players := map[int32]*tracked{pl.p.eid: pl}
	h.playersRef = players
	w := h.worldFor(0)
	// A stone slab at y=179 with a sealed dark room to the east (x 4–15),
	// its only opening a doorway on the west wall at z=0.
	for x := -4; x <= 16; x++ {
		for z := -8; z <= 8; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
		}
	}
	for x := 4; x <= 15; x++ {
		for z := -7; z <= 7; z++ {
			w.SetBlock(x, 182, z, worldgen.Stone)
			if x == 4 || x == 15 || z == -7 || z == 7 {
				if x == 4 && z == 0 {
					continue // the doorway
				}
				w.SetBlock(x, 180, z, worldgen.Stone)
				w.SetBlock(x, 181, z, worldgen.Stone)
			}
		}
	}
	h.dayTime.Store(6000)
	sk := h.spawnMob(players, entitySkeleton, 2.5, 180, 0.5)
	sk.hasTarget = false
	sk.burning = true
	if !h.skyExposedAt(2, 180, 0) || h.skyExposedAt(8, 180, 0) {
		t.Fatalf("fixture: open at the spawn, roofed at the pocket")
	}
	moved := false
	for i := 0; i < 200 && !moved; i++ {
		if h.fleeSunStep(players, sk) {
			moved = sk.vx > 0 && sk.hidePos.x >= 5 && sk.hidePos.x <= 14
		}
	}
	if !moved {
		t.Fatalf("the skeleton should head for the pocket: hide %+v v %.2f", sk.hidePos, sk.vx)
	}
	sk.hasTarget = true
	if h.fleeSunStep(players, sk) || sk.hidePos != (blockPos{}) {
		t.Fatal("a skeleton with a target keeps shooting")
	}
	sk.hasTarget = false
	h.dayTime.Store(15000)
	if h.fleeSunStep(players, sk) {
		t.Fatal("no fleeing at night")
	}
}
