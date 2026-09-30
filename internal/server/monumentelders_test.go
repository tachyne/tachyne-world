package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A player reaching a monument brings its three elder guardians in at the
// rooms' spawnElder cells (the penthouse and the two wing rooms).
func TestMonumentEldersAtRoomCells(t *testing.T) {
	h := newTestHub(world.New(1))
	g := h.world.Gen()
	var mn = g.MonumentIn(0, 0)
	for i := 0; i < 60 && !mn.Exists; i++ {
		for j := 0; j < 60 && !mn.Exists; j++ {
			mn = g.MonumentIn(i*448+224, j*448+224)
		}
	}
	if !mn.Exists {
		t.Skip("no monument for this seed")
	}
	spots := g.MonumentElders(mn)
	if len(spots) != 3 {
		t.Fatalf("%d elder cells, want 3", len(spots))
	}
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(mn.X), float64(mn.Y+30), float64(mn.Z)
	players := map[int32]*tracked{1: pl}
	h.populateMonuments(players)
	var got [][2]float64
	for _, m := range h.mobs {
		if m.etype == entityElderGuardian {
			got = append(got, [2]float64{m.x, m.z})
		}
	}
	if len(got) != 3 {
		t.Fatalf("%d elder guardians, want 3", len(got))
	}
	for _, s := range spots {
		found := false
		for _, p := range got {
			found = found || p[0] == float64(s[0])+0.5 && p[1] == float64(s[2])+0.5
		}
		if !found {
			t.Errorf("no elder at the cell %v (elders at %v)", s, got)
		}
	}
}
