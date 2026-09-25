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

// RestrictSunGoal: by day a bare-headed skeleton in shade will not path out
// into the sun, even after a player standing in it; a helmet lifts that.
func TestSkeletonKeepsToTheShade(t *testing.T) {
	run := func(helmet bool) float64 {
		h := newHub(world.New(1))
		h.world.ForceLoad(0, 0, 2)
		w := h.worldFor(0)
		for x := -8; x <= 16; x++ {
			for z := -8; z <= 8; z++ {
				w.SetBlock(x, 179, z, worldgen.Stone)
			}
		}
		for x := -3; x <= 3; x++ { // a roof on stilts over the skeleton
			for z := -3; z <= 3; z++ {
				w.SetBlock(x, 183, z, worldgen.Stone)
			}
		}
		pl := survPlayer(h)
		pl.x, pl.y, pl.z = 10.5, 180, 0.5
		players := map[int32]*tracked{pl.p.eid: pl}
		h.playersRef = players
		h.dayTime.Store(6000)
		sk := h.spawnHostileYIn(players, entitySkeleton, 0, 0.5, 180, 0.5)
		sk.held = int32(itemByName["stone_sword"]) // a sword-armed skeleton closes to melee
		h.reassessWeapon(sk)
		if helmet {
			sk.gear[0] = invStack{item: int32(itemByName["iron_helmet"]), count: 1}
		}
		far := sk.x
		for i := 0; i < 150; i++ {
			h.tick.Add(mobMoveInterval)
			pl.health, pl.dead = 20, false
			h.updateMobs(players)
			if sk.x > far {
				far = sk.x
			}
		}
		return far
	}
	if far := run(false); far > 4.2 {
		t.Fatalf("a bare-headed skeleton walked out into the sun to x=%.2f", far)
	}
	if far := run(true); far <= 4.2 {
		t.Fatalf("a helmeted skeleton may leave the shade (reached x=%.2f)", far)
	}
}
