package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Primed TNT moves (PrimedTnt.tick): lit, it hops up a fifth of a block in
// a random direction, then it falls under gravity, drags in the air, and
// on the ground loses most of its motion and bounces a little — and a
// blast pushes it like anything else (explodeHurt), which is what a TNT
// cannon is. It sat where it was lit before.

const (
	tntHopH          = 0.02 // PrimedTnt(): -sin(rot) × 0.02, -cos(rot) × 0.02
	tntHopV          = 0.2
	tntGravity       = 0.04 // Entity.applyGravity (default gravity)
	tntAirDrag       = 0.98 // getAirDrag
	tntGroundDragH   = 0.7  // onGround: multiply(0.7, -0.5, 0.7)
	tntGroundBounceV = -0.5
	tntHalfWidth     = 0.49 // EntityType.TNT size 0.98 × 0.98
	tntHeight        = 0.98
	tntBlastYOffset  = 0.0625 * tntHeight // explode(): getY(0.0625)
)

// tntStep is one tick of a primed charge's motion, broadcast when it moved.
func (h *hub) tntStep(players map[int32]*tracked, t *primedTNT) {
	w := h.worldFor(t.dim)
	if w == nil {
		return
	}
	t.vy -= tntGravity
	ox, oy, oz := t.x, t.y, t.z
	// Entity.move, one axis at a time against the blocks (Y first, as
	// vanilla resolves the vertical before sliding sideways).
	t.onGround = false
	if ny := t.y + t.vy; h.tntBoxBlocked(w, t.x, ny, t.z) {
		if t.vy < 0 {
			t.y = math.Ceil(ny) // onto the top of the block below
			t.onGround = true
		} else {
			t.y = math.Floor(ny+tntHeight) - tntHeight // under the block above
		}
		t.vy = 0
	} else {
		t.y = ny
	}
	if nx := t.x + t.vx; h.tntBoxBlocked(w, nx, t.y, t.z) {
		t.vx = 0
	} else {
		t.x = nx
	}
	if nz := t.z + t.vz; h.tntBoxBlocked(w, t.x, t.y, nz) {
		t.vz = 0
	} else {
		t.z = nz
	}
	t.vx, t.vy, t.vz = t.vx*tntAirDrag, t.vy*tntAirDrag, t.vz*tntAirDrag
	if t.onGround {
		t.vx, t.vy, t.vz = t.vx*tntGroundDragH, t.vy*tntGroundBounceV, t.vz*tntGroundDragH
	}
	if t.x != ox || t.y != oy || t.z != oz {
		h.toNearbyEv(players, t.dim, t.x, t.z, entMove(t.eid, t.x, t.y, t.z, 0, 0, t.onGround))
	}
}

// tntBoxBlocked reports whether the charge's box at (x,y,z) overlaps a
// colliding block.
func (h *hub) tntBoxBlocked(w *world.World, x, y, z float64) bool {
	x0, x1 := int(math.Floor(x-tntHalfWidth)), int(math.Floor(x+tntHalfWidth-1e-7))
	y0, y1 := int(math.Floor(y)), int(math.Floor(y+tntHeight-1e-7))
	z0, z1 := int(math.Floor(z-tntHalfWidth)), int(math.Floor(z+tntHalfWidth-1e-7))
	for bx := x0; bx <= x1; bx++ {
		for by := y0; by <= y1; by++ {
			for bz := z0; bz <= z1; bz++ {
				if worldgen.Collides(w.At(bx, by, bz)) {
					return true
				}
			}
		}
	}
	return false
}
