package worldgen

import "sort"

// The fluid face test, vanilla's FlowingFluid.canPassThroughWall: fluid
// passes between two cells unless either holds a full-block collision shape,
// or the two faces they turn to each other (the source's face in the
// direction of travel, the target's opposite face) together cover the whole
// square (Shapes.mergedFaceOccludes). A waterlogged stair lets its water out
// of the open side of its step and keeps it in behind its back.
//
// The face shapes come from the running game (scripts/extract, field
// collisionFaces) via gen_fluidfaces.py.

// Face directions, in vanilla's Direction order.
const (
	FaceDown = iota
	FaceUp
	FaceNorth
	FaceSouth
	FaceWest
	FaceEast
)

// faceRect is one box of a face shape on the face's two axes: a0, b0, a1, b1.
type faceRect [4]float64

type faceRun struct {
	lo, hi uint32
	faces  [6]uint16
}

// faceShapeOf returns a partial-shape state's face shape in direction d, and
// whether the state has a partial shape at all.
func faceShapeOf(state uint32, d int) ([]faceRect, bool) {
	i := sort.Search(len(faceRuns), func(i int) bool { return faceRuns[i].hi >= state })
	if i < len(faceRuns) && faceRuns[i].lo <= state {
		return faceShapes[faceRuns[i].faces[d]], true
	}
	return nil, false
}

// OppositeFace is the direction pointing back.
func OppositeFace(d int) int { return d ^ 1 }

// FluidPassesFace is canPassThroughWall(d, source, target): whether fluid in
// the source cell's block may pass into the target cell's in direction d.
func FluidPassesFace(d int, source, target uint32) bool {
	tFace, tPartial := faceShapeOf(target, OppositeFace(d))
	if !tPartial && Collides(target) {
		return false // a full block
	}
	sFace, sPartial := faceShapeOf(source, d)
	if !sPartial && Collides(source) {
		return false
	}
	if !sPartial && !tPartial {
		return true // both empty
	}
	return !facesCover(sFace, tFace)
}

// facesCover reports whether two face shapes together cover the whole unit
// square: every cell of the grid their edges cut the square into is inside
// one of the boxes.
func facesCover(a, b []faceRect) bool {
	rects := append(append([]faceRect{}, a...), b...)
	if len(rects) == 0 {
		return false
	}
	as, bs := []float64{0, 1}, []float64{0, 1}
	for _, r := range rects {
		as = append(as, r[0], r[2])
		bs = append(bs, r[1], r[3])
	}
	sort.Float64s(as)
	sort.Float64s(bs)
	for i := 0; i+1 < len(as); i++ {
		if as[i+1]-as[i] < 1e-9 || as[i] < 0 || as[i+1] > 1 {
			continue
		}
		ca := (as[i] + as[i+1]) / 2
		for j := 0; j+1 < len(bs); j++ {
			if bs[j+1]-bs[j] < 1e-9 || bs[j] < 0 || bs[j+1] > 1 {
				continue
			}
			cb := (bs[j] + bs[j+1]) / 2
			covered := false
			for _, r := range rects {
				if ca > r[0] && ca < r[2] && cb > r[1] && cb < r[3] {
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
