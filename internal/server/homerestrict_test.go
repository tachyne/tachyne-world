package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// checkRestriction: a tamed nautilus left alone takes a home of 32 where it
// is and swims back when it strays past it; one taken further than 32+8
// (ridden or led off) re-anchors there. An untamed one keeps no home, and a
// happy ghast keeps one of 64 whether tamed or not.
func TestNautilusAndGhastHomes(t *testing.T) {
	h := newHub(world.New(1))
	nautilusSea(h)
	players := map[int32]*tracked{}
	n := h.spawnSpecies(players, entityNautilus, 0, 0.5, 184, 0.5)
	if h.restrictionHomeStep(n) || n.homeR != 0 {
		t.Fatal("an untamed nautilus keeps no home")
	}
	n.tamed = true
	h.restrictionHomeStep(n)
	if n.homeR != 32 || n.homePos != (blockPos{0, 184, 0}) {
		t.Fatalf("a tamed nautilus anchors a home of 32 where it is: %v r%d", n.homePos, n.homeR)
	}
	n.x = 35.5
	if !h.restrictionHomeStep(n) || n.vx >= 0 {
		t.Fatalf("outside its home it heads back: vx %v", n.vx)
	}
	n.x = 45.5
	if h.restrictionHomeStep(n) || n.homePos.x != 45 {
		t.Fatalf("taken past 40 it re-anchors: home %v", n.homePos)
	}
	n.saddled = true
	h.restrictionHomeStep(n)
	if n.homeR != 16 {
		t.Fatalf("a saddled nautilus's home is 16, got %d", n.homeR)
	}
	g := h.spawnSpecies(players, entityHappyGhast, 0, 0.5, 200, 0.5)
	h.restrictionHomeStep(g)
	if g.homeR != 64 {
		t.Fatalf("a happy ghast's home is 64, got %d", g.homeR)
	}
	g.x = 70.5
	if !h.restrictionHomeStep(g) || g.vx >= 0 {
		t.Fatal("a happy ghast 70 out drifts back home")
	}
}
