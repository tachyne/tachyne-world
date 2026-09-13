package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TestRabbitRaidsCarrots: a hungry rabbit hops to a grown carrot, takes a
// stage off it, and is full for a while; a first-stage carrot is eaten
// whole; mobGriefing off stops it.
func TestRabbitRaidsCarrots(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	w := h.worldFor(0)
	for x := -2; x <= 8; x++ {
		for z := -2; z <= 2; z++ {
			w.SetBlock(x, 179, z, worldgen.Stone)
			w.SetBlock(x, 180, z, worldgen.Air)
		}
	}
	w.SetBlock(6, 179, 0, farmlandMin)
	w.SetBlock(6, 180, 0, carrotHi) // grown carrots
	r := h.spawnAnimal(players, entityRabbit, 0, 0)
	r.x, r.y, r.z = 0.5, 180, 0.5
	if !h.rabbitStep(players, r) || r.vx <= 0 || r.raidTarget != (blockPos{6, 179, 0}) {
		t.Fatalf("the rabbit should head for the carrots: vx %.3f target %+v", r.vx, r.raidTarget)
	}
	r.x = 5.8
	h.rabbitStep(players, r)
	if w.At(6, 180, 0) != carrotHi-1 || r.carrotTicks != rabbitFullTicks {
		t.Fatalf("a bite takes a stage: state %d (want %d), full %d", w.At(6, 180, 0), carrotHi-1, r.carrotTicks)
	}
	for i := 0; i < 60 && r.carrotTicks > 0; i++ {
		h.rabbitStep(players, r)
	}
	if r.carrotTicks != 0 {
		t.Fatal("hunger should return")
	}
	// A first-stage carrot goes entirely.
	w.SetBlock(6, 180, 0, carrotHi)
	r.raidRest = 0
	h.rabbitStep(players, r) // (not grown enough? it is: carrotHi again) — bite
	w.SetBlock(6, 180, 0, carrotLo)
	r.carrotTicks, r.raidRest = 0, 0
	// The scan only wants grown carrots, so seed the target directly.
	r.raidTarget = blockPos{6, 179, 0}
	h.rabbitStep(players, r)
	if w.At(6, 180, 0) != worldgen.Air {
		t.Fatalf("a first-stage carrot is eaten whole: %d", w.At(6, 180, 0))
	}
	// mobGriefing off: no raiding.
	w.SetBlock(6, 180, 0, carrotHi)
	r.carrotTicks, r.raidRest = 0, 0
	h.rules.MobGriefing = false
	if h.rabbitStep(players, r) {
		t.Fatal("mobGriefing off stops the raid")
	}
}
