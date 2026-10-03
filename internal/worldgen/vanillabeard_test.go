package worldgen

import (
	"math"
	"testing"
)

// The Beardifier against the 26.3 server's: fixed pieces of every
// adjustment and two jigsaw junctions (a Java oracle building the server's
// Beardifier over the same), hashed over a lattice of points and over a
// volume.
func TestVanillaBeardifierMatchesServer(t *testing.T) {
	b := newVTBeardifier([]VanillaBeardPiece{
		{0, 60, 0, 10, 70, 8, AdjustBeardThin, 1},
		{20, 40, -5, 30, 52, 9, AdjustBeardBox, 0},
		{-20, -10, -20, -5, 5, -8, AdjustBury, 0},
		{5, -30, 20, 25, -15, 40, AdjustEncapsulate, 0},
	}, []VanillaJunction{{12, 64, 4}, {-3, 66, 10}})
	h := uint64(vtFNVBasis)
	for x := -50; x <= 60; x += 3 {
		for z := -50; z <= 70; z += 4 {
			for y := -60; y <= 100; y += 2 {
				h = vtFNV(h, uint64(math.Float32bits(b.value(nil, x, y, z))))
			}
		}
	}
	if h != 1523705632375676073 {
		t.Errorf("point hash %d, the server's 1523705632375676073", h)
	}
	v := vtVol(16, 200, 16, 0, -64, 0)
	buf := vdBuf(v.size())
	b.volume(nil, buf, v)
	h = vtFNVBasis
	for _, f := range buf {
		h = vtFNV(h, uint64(math.Float32bits(f)))
	}
	if h != 6255629254812900365 {
		t.Errorf("volume hash %d, the server's 6255629254812900365", h)
	}
}
