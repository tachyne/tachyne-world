package server

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// BigDripleafBlock / BigDripleafStemBlock.canSurvive: a leaf stands on its
// own stem (which has no collision, so is no floor), a stem needs the
// plant's ground below and the rest of the plant above, and neither roots
// in plain stone.
func TestBigDripleafSurvival(t *testing.T) {
	w := world.New(1)
	w.ForceLoad(0, 0, 1)
	leaf, stem := worldgen.BlockBase("big_dripleaf"), worldgen.BlockBase("big_dripleaf_stem")
	clay, stone := worldgen.BlockBase("clay"), worldgen.Stone
	set := func(y int, s uint32) { w.SetBlock(0, y, 0, s) }
	for _, tc := range []struct {
		name            string
		ground, mid, up uint32
		cell            int // the y whose survival is asked: 181 or 182
		want            bool
	}{
		{"leaf on its stem", clay, stem, leaf, 182, true},
		{"stem on clay under a leaf", clay, stem, leaf, 181, true},
		{"leaf on clay", worldgen.Air, clay, leaf, 182, true},
		{"leaf on a leaf", clay, leaf, leaf, 182, true},
		{"leaf on stone", worldgen.Air, stone, leaf, 182, false},
		{"stem on stone under a leaf", stone, stem, leaf, 181, false},
		{"stem with nothing above", clay, stem, worldgen.Air, 181, false},
		{"stem standing on a leaf", leaf, stem, leaf, 181, false},
	} {
		set(180, tc.ground)
		set(181, tc.mid)
		set(182, tc.up)
		set(183, worldgen.Air)
		if got := supported(w, blockPos{0, tc.cell, 0}, w.At(0, tc.cell, 0)); got != tc.want {
			t.Errorf("%s: survives=%v, want %v", tc.name, got, tc.want)
		}
	}
}
