package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Dropped items in fluid (ItemEntity.tick: setUnderwaterMovement +
// Entity.onInsideBubbleColumn + applyGravity): an item under water rises
// slowly to the surface and floats there, a bubble column carries it up
// (an updraft lifts it clear of the water and it drops back in, over and
// over — the item elevator) or pins it to the bottom (a whirlpool). Items
// on dry ground never move: this runs only while there is fluid or a
// column at or under the item.

const (
	itemFluidLift    = 5.0e-4 // setFluidMovement: +0.0005 while vy < 0.06
	itemFluidLiftCap = 0.06
	itemFluidDrag    = 0.99 // horizontal in water (unused: no horizontal motion)
	itemGravity      = 0.04 // applyGravity
	itemAirDrag      = 0.98
	columnUpStep     = 0.06 // onInsideBubbleColumn: min(0.7, vy + 0.06)
	columnUpCap      = 0.7
	columnDownStep   = 0.03 // max(-0.3, vy - 0.03)
	columnDownCap    = -0.3
)

// floatItems is the per-tick vertical physics for items in or over fluid.
func (h *hub) floatItems(players map[int32]*tracked) {
	for _, it := range h.items {
		w := h.worldFor(it.dim)
		if w == nil {
			continue
		}
		fx, fy, fz := int(math.Floor(it.x)), int(math.Floor(it.y)), int(math.Floor(it.z))
		cell := w.At(fx, fy, fz)
		below := w.At(fx, fy-1, fz)
		switch {
		case cell == worldgen.BubbleColumnUp:
			it.vy = math.Min(columnUpCap, it.vy+columnUpStep)
		case cell == worldgen.BubbleColumnDrag:
			it.vy = math.Max(columnDownCap, it.vy-columnDownStep)
		case worldgen.IsWater(cell):
			if it.vy < itemFluidLiftCap {
				it.vy += itemFluidLift
			}
		case worldgen.IsWater(below) && !worldgen.IsBubbleColumn(below):
			// Afloat at the surface: it settles onto the water's top face.
			if it.vy != 0 || it.y != float64(fy) {
				it.vy, it.y = 0, float64(fy)
				h.toNearbyEv(players, it.dim, it.x, it.z, entMove(it.eid, it.x, it.y, it.z, 0, 0, true))
			}
			continue
		case worldgen.IsBubbleColumn(below) || (it.vy != 0 && !worldgen.Collides(below)):
			// Thrown clear by an updraft, or still falling: gravity.
			it.vy = (it.vy - itemGravity) * itemAirDrag
		default:
			it.vy = 0
			continue
		}
		if it.vy == 0 {
			continue
		}
		ny := it.y + it.vy
		nfy := int(math.Floor(ny))
		if it.vy > 0 && nfy != fy && worldgen.Collides(w.At(fx, nfy, fz)) {
			ny, it.vy = float64(nfy)-1e-3, 0 // a ceiling
		} else if it.vy < 0 && nfy != fy && worldgen.Collides(w.At(fx, nfy, fz)) {
			ny, it.vy = float64(nfy+1), 0 // landed
		}
		if ny == it.y {
			continue
		}
		it.y = ny
		h.toNearbyEv(players, it.dim, it.x, it.z, entMove(it.eid, it.x, it.y, it.z, 0, 0, it.vy == 0))
	}
}
