package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// shouldTryToChainAnotherHangingSign: holding a hanging sign, a click on a
// ceiling sign's underside, or on a wall sign anywhere but its two text
// faces, places a sign rather than opening the editor.
func TestHangingSignChains(t *testing.T) {
	item := itemByName["oak_hanging_sign"]
	ceil := worldgen.BlockBase("oak_hanging_sign")
	wall := withProp(worldgen.BlockBase("oak_wall_hanging_sign"), "facing", "north")
	ck, _ := signKind(ceil)
	wk, _ := signKind(wall)
	if ck != signHangingCeiling || wk != signHangingWall {
		t.Fatalf("sign kinds %d %d", ck, wk)
	}
	for _, tc := range []struct {
		name  string
		kind  int
		state uint32
		held  int32
		face  int32
		want  bool
	}{
		{"ceiling, underside", ck, ceil, item, 0, true},
		{"ceiling, its side", ck, ceil, item, 2, false},
		{"ceiling, underside, a plain sign in hand", ck, ceil, itemByName["oak_sign"], 0, false},
		{"wall facing north, its text face", wk, wall, item, 2, false},
		{"wall facing north, the back text face", wk, wall, item, 3, false},
		{"wall facing north, its end", wk, wall, item, 5, true},
		{"wall facing north, its underside", wk, wall, item, 0, true},
	} {
		if got := chainsHangingSign(tc.kind, tc.state, tc.held, tc.face); got != tc.want {
			t.Errorf("%s: chains=%v, want %v", tc.name, got, tc.want)
		}
	}

	// …and the chained sign hangs from the one above it.
	w := world.New(1)
	w.SetBlock(0, 182, 0, worldgen.Stone)
	w.SetBlock(0, 181, 0, ceil)
	w.SetBlock(0, 180, 0, ceil)
	if !supported(w, blockPos{0, 180, 0}, ceil) {
		t.Error("a hanging sign under a hanging sign fell")
	}
	w.SetBlock(1, 181, 0, wall)
	w.SetBlock(1, 180, 0, ceil)
	if !supported(w, blockPos{1, 180, 0}, ceil) {
		t.Error("a hanging sign under a wall hanging sign fell")
	}
	w.SetBlock(2, 181, 0, worldgen.BlockBase("oak_sign"))
	w.SetBlock(2, 180, 0, ceil)
	if supported(w, blockPos{2, 180, 0}, ceil) {
		t.Error("a hanging sign hangs from a standing sign")
	}
}
