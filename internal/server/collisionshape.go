package server

// Block collision shapes as boxes (collisionboxes_gen.go, from the running
// game via scripts/extract and gen_collisionboxes.py): what the tests that
// ask about a box — Level.noCollision, Shapes.joinIsNotEmpty — need beyond
// the one-bit full-or-nothing collision and the vertical bounds.

// cbox is one box of a shape, [minX, minY, minZ, maxX, maxY, maxZ] within
// the block's cell.
type cbox [6]float64

// cshapeRun is a run of states sharing one partial shape.
type cshapeRun struct {
	lo, hi uint32
	shape  uint16
}

// unitCube is the one box of a full block.
var unitCube = []cbox{{0, 0, 0, 1, 1, 1}}

// collisionShape is BlockState.getCollisionShape under an empty context as
// its boxes: nil for an empty shape, one unit box for a full block.
func collisionShape(s uint32) []cbox {
	i, j := 0, len(collisionShapeRuns)
	for i < j {
		m := (i + j) / 2
		if collisionShapeRuns[m].hi < s {
			i = m + 1
		} else {
			j = m
		}
	}
	if i < len(collisionShapeRuns) && collisionShapeRuns[i].lo <= s {
		return collisionShapes[collisionShapeRuns[i].shape]
	}
	if _, _, ok := collisionBounds(s); !ok {
		return nil
	}
	return unitCube
}

// shapeMeetsBox reports whether a state's collision shape at cell (x, y, z)
// overlaps the world box [lo, hi] (open intervals: touching faces do not
// count, as Shapes.joinIsNotEmpty with AND does not).
func shapeMeetsBox(s uint32, x, y, z int, lo, hi [3]float64) bool {
	for _, b := range collisionShape(s) {
		if b[0]+float64(x) < hi[0] && b[3]+float64(x) > lo[0] &&
			b[1]+float64(y) < hi[1] && b[4]+float64(y) > lo[1] &&
			b[2]+float64(z) < hi[2] && b[5]+float64(z) > lo[2] {
			return true
		}
	}
	return false
}
