package server

import (
	"math"
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
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

// The monument's eight guardians come in inside its rooms, each in a room's
// water (the monster spawn override's IN_WATER placement), not at fixed
// offsets about the centre where the room walls and the core now stand.
func TestMonumentGuardiansInRoomWater(t *testing.T) {
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
	rooms := g.MonumentRooms(mn)
	if len(rooms) < 8 {
		t.Fatalf("%d rooms in the plan", len(rooms))
	}
	h.world.ForceLoad(mn.X, mn.Z, 3) // the monument's chunks, which the guardians' water is read from
	pl := testTracked()
	pl.x, pl.y, pl.z = float64(mn.X), float64(mn.Y+30), float64(mn.Z)
	players := map[int32]*tracked{1: pl}
	h.populateMonuments(players)
	n := 0
	for _, m := range h.mobs {
		if m.etype != entityGuardian {
			continue
		}
		n++
		x, y, z := int(math.Floor(m.x)), int(math.Floor(m.y)), int(math.Floor(m.z))
		in := false
		for _, r := range rooms {
			in = in || x >= r[0] && x <= r[3] && y >= r[1] && y <= r[4] && z >= r[2] && z <= r[5]
		}
		if !in {
			t.Errorf("a guardian at %d,%d,%d is in no room", x, y, z)
		}
		if s := h.world.At(x, y, z); !worldgen.IsWater(s) {
			t.Errorf("a guardian at %d,%d,%d stands in %d, not water", x, y, z, s)
		}
	}
	if n == 0 {
		t.Fatal("no guardians came in")
	}
}
