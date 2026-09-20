package world

import (
	"testing"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Sky light through translucent blocks. Vanilla's LayerLightEngine charges
// max(1, opacity) for a step, not 1 + opacity — charging both made every
// translucent block cost DOUBLE, so a pond or a canopy went dark at half the
// real depth, and took mob spawning with it.
func TestSkyLightCostsMaxOfOneAndOpacity(t *testing.T) {
	const x, z, top = 8, 8, 200
	for _, tc := range []struct {
		block string
		// what the column should read at the top block and each one below
		want []uint8
	}{
		// Glass has opacity 0: a step costs 1, but a straight-down sky column
		// through it is still full strength.
		{"glass", []uint8{15, 15, 15, 15, 15, 15}},
		// Water and leaves have opacity 1: one level a block, not two.
		{"water", []uint8{14, 13, 12, 11, 10, 9}},
		{"oak_leaves", []uint8{14, 13, 12, 11, 10, 9}},
	} {
		w := New(1)
		for dy := -8; dy <= 8; dy++ {
			w.SetBlock(x, top+dy, z, worldgen.Air)
		}
		// A solid lid either side so light can only come down the column,
		// otherwise it floods in sideways and the depth profile means nothing.
		for dx := -1; dx <= 1; dx++ {
			for dz := -1; dz <= 1; dz++ {
				if dx == 0 && dz == 0 {
					continue
				}
				for dy := -8; dy <= 0; dy++ {
					w.SetBlock(x+dx, top+dy, z+dz, worldgen.Stone)
				}
			}
		}
		for i := range tc.want {
			w.SetBlock(x, top-i, z, worldgen.BlockBase(tc.block))
		}
		l := w.Light(0, 0)
		at := func(wy int) uint8 {
			s := (wy - worldgen.MinY) / 16
			ly := (wy - worldgen.MinY) % 16
			return l.Sky[s][(ly*16+z%16)*16+x%16]
		}
		for i, want := range tc.want {
			if got := at(top - i); got != want {
				t.Errorf("%s %d block(s) down: sky light %d, want %d", tc.block, i, got, want)
			}
		}
	}
}
