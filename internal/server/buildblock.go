package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A block cannot be placed where it would overlap something standing there
// (BlockItem.canPlace → EntityGetter.isUnobstructed): every entity that
// blocksBuilding — the living (players too, but never a spectator), boats,
// minecarts — refuses a block whose collision shape meets its box. Without
// it a wall went up through a sheep, which then stood inside the wall.
//
// Placement runs on the session goroutine and the bodies belong to the hub,
// so the hub publishes a snapshot of them every tick for placements to read.

// bodyBox is one building-blocking entity's box: feet centre, half width,
// height.
type bodyBox struct {
	dim     int
	x, y, z float64
	hw, h   float64
}

// Vehicle boxes (EntityType sizes): a minecart is 0.98 × 0.7, a boat
// 1.375 × 0.5625.
const (
	cartBoxHalfWidth = 0.49
	cartBoxHeight    = 0.7
	boatBoxHalfWidth = 1.375 / 2
	boatBoxHeight    = 0.5625
)

// publishBodies stores this tick's building-blocking boxes.
func (h *hub) publishBodies(players map[int32]*tracked) {
	out := make([]bodyBox, 0, len(h.mobs)+len(players)+len(h.vehicles))
	for _, m := range h.mobs {
		if m.dying > 0 {
			continue
		}
		b := m.box()
		out = append(out, bodyBox{m.dim, m.x, m.y, m.z, b.w / 2, b.h})
	}
	for _, t := range players {
		if t.dead || t.gamemode == gmSpectator {
			continue
		}
		ht := psPlayerHeight
		if t.sneaking {
			ht = 1.5
		}
		out = append(out, bodyBox{t.dim, t.x, t.y, t.z, playerHalfWidth, ht})
	}
	for _, v := range h.vehicles {
		hw, ht := boatBoxHalfWidth, boatBoxHeight
		if !v.isBoat() {
			hw, ht = cartBoxHalfWidth, cartBoxHeight
		}
		out = append(out, bodyBox{v.dim, v.x, v.y, v.z, hw, ht})
	}
	h.bodies.Store(&out)
}

// placeObstructed reports whether putting state at (x, y, z) in dim would
// overlap a body. The block's shape is its cell — halved for a single slab —
// or nothing for a block without collision (a torch, a flower, a rail).
func (h *hub) placeObstructed(dim, x, y, z int, state uint32) bool {
	if !worldgen.Collides(state) {
		return false
	}
	bodies := h.bodies.Load()
	if bodies == nil {
		return false
	}
	lo, hi := float64(y), float64(y)+1
	if info, ok := worldgen.InfoForState(state); ok && isSlab(info) {
		switch worldgen.GetProperty(info, state, "type") {
		case "bottom":
			hi = lo + 0.5
		case "top":
			lo += 0.5
		}
	}
	const eps = 1e-7 // touching a face is not overlapping it
	fx, fz := float64(x), float64(z)
	for _, b := range *bodies {
		if b.dim != dim || math.Abs(b.x-fx-0.5) > 2 || math.Abs(b.z-fz-0.5) > 2 {
			continue
		}
		if b.x+b.hw > fx+eps && b.x-b.hw < fx+1-eps &&
			b.z+b.hw > fz+eps && b.z-b.hw < fz+1-eps &&
			b.y+b.h > lo+eps && b.y < hi-eps {
			return true
		}
	}
	return false
}
