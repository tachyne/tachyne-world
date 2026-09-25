package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

func tntFloor(w *world.World, y int) {
	for x := -12; x <= 12; x++ {
		for z := -12; z <= 12; z++ {
			w.SetBlock(x, y, z, worldgen.Stone)
		}
	}
}

// Lit TNT hops, falls and comes to rest on the ground with its fuse still
// burning; one lit over a drop lands on the floor below.
func TestPrimedTNTFalls(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	tntFloor(w, 179)
	players := map[int32]*tracked{}
	h.primeTNT(players, 0, 186, 0, 80)
	h.rules.TNTExplodes = false // the fuse may burn out harmlessly
	if len(h.tnt) != 1 || h.tnt[0].vy != tntHopV {
		t.Fatalf("lit TNT should hop: %+v", h.tnt)
	}
	pt := h.tnt[0]
	rose := false
	for i := 0; i < 60; i++ {
		h.updateTNT(players)
		if pt.y > 186 {
			rose = true
		}
	}
	if !rose {
		t.Error("the hop should lift it above where it was lit")
	}
	if !pt.onGround || math.Abs(pt.y-180) > 1e-9 || pt.vy != 0 {
		t.Fatalf("after a second it should rest on the floor at 180: y=%v vy=%v ground=%v", pt.y, pt.vy, pt.onGround)
	}
	if pt.fuse != 20 {
		t.Errorf("the fuse should have burned 60 ticks: %d", pt.fuse)
	}
}

// A blast throws primed TNT that stands beside it — the TNT cannon — the
// charge shoved away from the blast and sliding on down the field.
func TestBlastPushesPrimedTNT(t *testing.T) {
	h := newHub(world.New(1))
	w := h.world
	tntFloor(w, 179)
	players := map[int32]*tracked{}
	h.rules.TNTExplodes = false
	h.tnt = append(h.tnt, &primedTNT{eid: h.allocEID(), dim: 0, x: 1.5, y: 180, z: 0.5, fuse: 80, onGround: true})
	pt := h.tnt[0]
	h.explodeIn(players, 0, 0.5, 180, 0.5, 0, 4, blastTNT)
	if pt.vx <= 0.5 {
		t.Fatalf("a power-4 blast a block off should throw the charge east hard: vx=%v", pt.vx)
	}
	for i := 0; i < 20; i++ {
		h.updateTNT(players)
	}
	if pt.x < 3 {
		t.Fatalf("a second later the charge should have slid down the field: x=%v", pt.x)
	}
}
