package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// WallBlock.isCovered reads the bottom face of the collision shape above:
// a bottom slab or a carpet covers every test square (tall sides, and a
// post on a low straight run), a top slab covers none, a torch raises the
// post only through #wall_post_override, and a fence above raises the post
// (its centre post covers the 2×2 test) but leaves the sides low.
func TestWallCoverFollowsTheShapeAbove(t *testing.T) {
	def, info := wallDefault(t)
	state := func(name string, props map[string]string) uint32 {
		lo, _, ok := worldgen.BlockRangeOK(name)
		if !ok {
			t.Fatalf("no %s", name)
		}
		bi, _ := worldgen.InfoForState(lo)
		s := lo
		for k, v := range props {
			s = worldgen.SetProperty(bi, s, k, v)
		}
		return s
	}
	straight := func(above uint32) uint32 {
		w := world.New(1)
		w.SetBlock(1, 200, 0, stone)
		w.SetBlock(-1, 200, 0, stone)
		w.SetBlock(0, 201, 0, above)
		return wallState(w, 0, 200, 0, info, def)
	}
	corner := func(above uint32) uint32 {
		w := world.New(1)
		w.SetBlock(1, 200, 0, stone)
		w.SetBlock(0, 200, -1, stone)
		w.SetBlock(0, 201, 0, above)
		return wallState(w, 0, 200, 0, info, def)
	}
	for _, c := range []struct {
		name       string
		above      uint32
		tall, post bool // on the straight run: sides tall, and (low runs only) the post
	}{
		{"bottom slab", state("stone_slab", map[string]string{"type": "bottom"}), true, false},
		{"top slab", state("stone_slab", map[string]string{"type": "top"}), false, false},
		{"carpet", state("white_carpet", nil), true, false},
		{"torch", state("torch", nil), false, true},
		{"fence post", state("oak_fence", map[string]string{"north": "false", "south": "false", "east": "false", "west": "false"}), false, true},
		{"air", worldgen.Air, false, false},
	} {
		st := straight(c.above)
		wantSide := "low"
		if c.tall {
			wantSide = "tall"
		}
		if got := sideOf(t, info, st, "east"); got != wantSide {
			t.Errorf("%s above a straight run: east %s, want %s", c.name, got, wantSide)
		}
		wantUp := "false"
		if c.post {
			wantUp = "true"
		}
		if got := sideOf(t, info, st, "up"); got != wantUp {
			t.Errorf("%s above a straight run: up %s, want %s", c.name, got, wantUp)
		}
	}
	// A fence arm above covers the wall side it runs along only.
	arm := state("oak_fence", map[string]string{"north": "false", "south": "false", "east": "true", "west": "false"})
	if st := corner(arm); sideOf(t, info, st, "east") != "tall" || sideOf(t, info, st, "north") != "low" {
		t.Errorf("a fence arm above: east %s north %s, want tall and low",
			sideOf(t, info, st, "east"), sideOf(t, info, st, "north"))
	}
}
