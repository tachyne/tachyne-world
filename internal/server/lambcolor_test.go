package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
)

// A lamb wears the dye its parents' dyes would craft: red and yellow give
// orange, blue and red purple, white and black gray; a pair with no mix
// takes one parent's colour.
func TestLambColorMixes(t *testing.T) {
	h := newHub(world.New(1))
	col := func(name string) int8 {
		for i, c := range dyeColors {
			if c == name {
				return int8(i)
			}
		}
		t.Fatalf("no colour %q", name)
		return -1
	}
	for _, c := range []struct{ a, b, want string }{
		{"red", "yellow", "orange"},
		{"yellow", "red", "orange"},
		{"blue", "red", "purple"},
		{"white", "black", "gray"},
		{"white", "blue", "light_blue"},
		{"red", "white", "pink"},
	} {
		if got := h.mixedFleeceColor(col(c.a), col(c.b)); got != col(c.want) {
			t.Errorf("%s + %s -> %s (%d), want %s", c.a, c.b, dyeColors[got], got, c.want)
		}
	}
	brown, green := col("brown"), col("green")
	seen := map[int8]int{}
	for i := 0; i < 200; i++ {
		got := h.mixedFleeceColor(brown, green)
		if got != brown && got != green {
			t.Fatalf("brown + green -> %s, want a parent's colour", dyeColors[got])
		}
		seen[got]++
	}
	if seen[brown] == 0 || seen[green] == 0 {
		t.Errorf("brown + green never varied: %v", seen)
	}
	// The same colour twice is a mix of nothing: the lamb is that colour.
	if got := h.mixedFleeceColor(col("lime"), col("lime")); got != col("lime") {
		t.Errorf("lime + lime -> %s", dyeColors[got])
	}
}

// Breeding two sheep applies the mix to the lamb and announces its fleece.
func TestLambColorOnBreed(t *testing.T) {
	h := newHub(world.New(1))
	players := map[int32]*tracked{}
	a := h.spawnMobIn(players, entitySheep, 0, 0, 70, 0)
	b := h.spawnMobIn(players, entitySheep, 0, 0, 70, 1)
	if a == nil || b == nil {
		t.Fatal("spawn returned nil")
	}
	a.color, b.color = 14, 4 // red, yellow
	baby := h.spawnMobIn(players, entitySheep, 0, 0, 70, 2)
	if baby == nil {
		t.Fatal("lamb spawn returned nil")
	}
	h.inheritVariant(baby, a, b)
	if baby.color != 1 {
		t.Errorf("lamb colour %d, want orange (1)", baby.color)
	}
}
