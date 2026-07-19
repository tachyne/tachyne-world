package worldgen

import "testing"

// TestTransformPos pins the vanilla pivot-0 mirror-then-rotate convention.
func TestTransformPos(t *testing.T) {
	cases := []struct {
		x, y, z, rot, mir int
		wx, wy, wz        int
	}{
		{1, 0, 0, 0, mirNone, 1, 0, 0},  // identity
		{1, 0, 0, 1, mirNone, 0, 0, 1},  // CW90: (-z,y,x)
		{1, 0, 0, 2, mirNone, -1, 0, 0}, // CW180
		{1, 0, 0, 3, mirNone, 0, 0, -1}, // CCW90: (z,y,-x)
		{1, 0, 0, 0, mirFB, -1, 0, 0},   // FRONT_BACK flips x
		{0, 0, 1, 0, mirLR, 0, 0, -1},   // LEFT_RIGHT flips z
		{2, 5, 3, 1, mirLR, 3, 5, 2},    // mirror then CW90: z=-3 -> (3,5,2)
	}
	for _, c := range cases {
		x, y, z := transformPos(c.x, c.y, c.z, c.rot, c.mir)
		if x != c.wx || y != c.wy || z != c.wz {
			t.Errorf("transformPos(%d,%d,%d,rot%d,mir%d) = (%d,%d,%d), want (%d,%d,%d)",
				c.x, c.y, c.z, c.rot, c.mir, x, y, z, c.wx, c.wy, c.wz)
		}
	}
}
