package server

import (
	"testing"
	"time"

	"github.com/tachyne/tachyne-world/internal/world"
)

// TestHalloweenPumpkinHeads: on the 31st of October a quarter of bare-headed
// zombies and skeletons spawn wearing a carved pumpkin (a jack o'lantern one
// in ten of those), and the head never drops.
func TestHalloweenPumpkinHeads(t *testing.T) {
	defer func(f func() time.Time) { spawnClock = f }(spawnClock)
	spawnClock = func() time.Time { return time.Date(2026, time.October, 31, 12, 0, 0, 0, time.Local) }
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	h.playersRef = players
	x, z := 10.5, 10.5
	y := float64(h.world.SurfaceFeet(10, 10))
	pumpkins, lanterns, total := 0, 0, 400
	var wearer *mob
	for i := 0; i < total; i++ {
		m := h.spawnHostileY(players, entitySkeleton, x, y, z)
		switch m.gear[0].item {
		case itemCarvedPumpkin:
			pumpkins++
			wearer = m
		case itemJackOLantern:
			lanterns++
			wearer = m
		}
		if !isPumpkinHead(m.gear[0].item) {
			delete(h.mobs, m.eid)
		}
	}
	if pumpkins+lanterns < 60 || pumpkins+lanterns > 140 {
		t.Fatalf("%d pumpkins + %d lanterns of %d, want about a quarter", pumpkins, lanterns, total)
	}
	if lanterns == 0 || lanterns > pumpkins {
		t.Fatalf("jack o'lanterns are one in ten of the heads: %d vs %d pumpkins", lanterns, pumpkins)
	}
	if !wearer.spawnGear {
		t.Fatal("the head is spawn gear")
	}
	wearer.dying = 1
	h.despawnMob(players, wearer)
	for _, it := range h.items {
		if isPumpkinHead(it.item) {
			t.Fatal("a Halloween head never drops")
		}
	}
	// Any other day: no pumpkins.
	spawnClock = func() time.Time { return time.Date(2026, time.November, 1, 12, 0, 0, 0, time.Local) }
	for i := 0; i < 100; i++ {
		if m := h.spawnHostileY(players, entityZombie, x, y, z); isPumpkinHead(m.gear[0].item) {
			t.Fatal("a pumpkin head on the 1st of November")
		}
	}
}
