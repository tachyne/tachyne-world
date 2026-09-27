package worldgen

import "strings"

// IsFaceSturdy is BlockState.isFaceSturdy(level, pos, direction) with the
// FULL support type: whether the block's support shape covers the whole
// square on face d (FaceDown … FaceEast). It is what WallBlock.connectsTo,
// FenceBlock.connectsTo and IronBarsBlock.attachsTo ask of a neighbour — so
// a wall joins the back of a stair, the hinge side of an open trapdoor and
// a pane of glass, and not the side of a bottom slab.
//
// The support shape is the collision shape (collisionFaces from the running
// game, see fluidfaces.go) except where a block overrides
// getBlockSupportShape: leaves offer none, soul sand and mud a full block,
// a snow layer its own height rather than one layer less, and a chorus
// flower a 14-wide column that fills no face. (A fence gate's support shape
// fills no face either way, as its collision shape does not; a shulker
// box's is a full block while shut.)
func IsFaceSturdy(state uint32, d int) bool {
	if state == Air {
		return false
	}
	if n, ok := StateName(state); ok {
		switch {
		case strings.HasSuffix(n, "_leaves"), n == "chorus_flower":
			return false
		case n == "soul_sand", n == "mud":
			return true
		case n == "snow":
			info, _ := InfoForState(state)
			return d == FaceDown || GetProperty(info, state, "layers") == "8"
		}
	}
	if face, partial := faceShapeOf(state, d); partial {
		return facesCover(face, nil)
	}
	return IsFullCube(state)
}
