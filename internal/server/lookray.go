package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// lookRay walks the cells along a player's line of sight, out to reach, and
// returns the first one `hit` accepts. It is the engine's stand-in for
// Item.getPlayerPOVHitResult, which items use when the client sends a plain
// use with no block attached — aiming a bottle at a pond, a boat at open
// water, a bucket at a spring.
//
// The step is a tenth of a block: fine enough that a cell cannot be skipped at
// any angle, and the loop stops at the first cell either way.
func (h *hub) lookRay(t *tracked, reach float64, hit func(pos blockPos, state uint32) bool) (blockPos, bool) {
	dx, dy, dz := lookVector(t.yaw, t.pitch)
	ox, oy, oz := t.x, t.y+t.eyeHeight(), t.z
	w := h.worldFor(t.dim)
	if w == nil {
		return blockPos{}, false
	}
	last := blockPos{math.MinInt, math.MinInt, math.MinInt}
	for d := 0.0; d <= reach; d += 0.1 {
		p := blockPos{floorInt(ox + dx*d), floorInt(oy + dy*d), floorInt(oz + dz*d)}
		if p == last {
			continue
		}
		last = p
		if st := w.At(p.x, p.y, p.z); hit(p, st) {
			return p, true
		}
	}
	return blockPos{}, false
}

// rayStopsAt is the ClipContext both fluid modes share once you strip the
// fluids out: anything with a collision box ends the ray.
func rayStopsAt(state uint32) bool { return worldgen.Collides(state) }
