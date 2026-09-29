package worldgen

import "sort"

// FaceCovers reports whether a state's collision shape, cut on face d
// (VoxelShape.getFaceShape), covers the rectangle r on that face's two axes
// ([a0, b0, a1, b1], each 0..1; x and z for the up and down faces) — the
// Shapes.joinIsNotEmpty(test, face, ONLY_FIRST) == false that WallBlock's
// isCovered asks of the block above a wall. A full block covers
// everything; a block without collision covers nothing.
func FaceCovers(state uint32, d int, r [4]float64) bool {
	face, partial := faceShapeOf(state, d)
	if !partial {
		return Collides(state)
	}
	as, bs := []float64{r[0], r[2]}, []float64{r[1], r[3]}
	for _, f := range face {
		as = append(as, f[0], f[2])
		bs = append(bs, f[1], f[3])
	}
	sort.Float64s(as)
	sort.Float64s(bs)
	for i := 0; i+1 < len(as); i++ {
		if as[i+1]-as[i] < 1e-9 || as[i] < r[0] || as[i+1] > r[2] {
			continue
		}
		ca := (as[i] + as[i+1]) / 2
		for j := 0; j+1 < len(bs); j++ {
			if bs[j+1]-bs[j] < 1e-9 || bs[j] < r[1] || bs[j+1] > r[3] {
				continue
			}
			cb := (bs[j] + bs[j+1]) / 2
			covered := false
			for _, f := range face {
				if ca > f[0] && ca < f[2] && cb > f[1] && cb < f[3] {
					covered = true
					break
				}
			}
			if !covered {
				return false
			}
		}
	}
	return true
}
