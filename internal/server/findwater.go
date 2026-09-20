package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// TryFindWaterGoal and StriderGoToLavaGoal — the two "get back in your
// element" goals. A dolphin, squid or fish stranded on the ground heads for
// water; a strider caught on solid ground heads for lava, which is why one
// you lead out of a lava lake turns straight back to it.

const (
	findWaterSearch = 12.0 // TryFindWaterGoal's random position (getPos 8/4 around)
	striderLavaFind = 8.0  // StriderGoToLavaGoal's MoveToBlockGoal(…, 8, 2)
)

// findsWater is the set that runs TryFindWaterGoal — the water animals that
// suffocate on land.
var findsWater = func() map[int]bool {
	out := map[int]bool{}
	for _, n := range []string{"dolphin", "squid", "glow_squid", "cod", "salmon",
		"tropical_fish", "pufferfish", "tadpole", "axolotl"} {
		if id, ok := entityByName[n]; ok {
			out[id] = true
		}
	}
	return out
}()

// findWaterStep walks a stranded water animal back to the water, and a
// strider off solid ground back to the lava. Reports whether it took the
// step.
func (h *hub) findWaterStep(m *mob) bool {
	if m.dying != 0 || m.panic > 0 {
		return false
	}
	switch {
	case findsWater[m.etype]:
		if h.inWater(m.dim, m.x, m.y+0.2, m.z) {
			return false
		}
		if x, z, ok := h.nearestWaterColumn(m, findWaterSearch); ok {
			return h.stepToward(m, x, z, 1.0)
		}
	case m.etype == entityStrider:
		if worldgen.IsLava(h.worldFor(m.dim).At(floorInt(m.x), floorInt(m.y), floorInt(m.z))) {
			return false
		}
		if x, z, ok := h.nearestLavaColumn(m, striderLavaFind); ok {
			return h.stepToward(m, x, z, 1.0)
		}
	}
	return false
}

// stepToward aims a mob at a point at a speed modifier, reporting whether it
// actually moved (it is already there if not).
func (h *hub) stepToward(m *mob, x, z, speed float64) bool {
	dx, dz := x-m.x, z-m.z
	d := math.Hypot(dx, dz)
	if d < 1 {
		return false
	}
	sp := m.moveSpeed() * speed
	m.vx, m.vz = dx/d*sp, dz/d*sp
	m.rest = 0
	return true
}

// nearestLavaColumn is the lava twin of nearestWaterColumn.
func (h *hub) nearestLavaColumn(m *mob, r float64) (float64, float64, bool) {
	w := h.worldFor(m.dim)
	bx, by, bz := floorInt(m.x), floorInt(m.y), floorInt(m.z)
	best, bx2, bz2, found := r*r, 0.0, 0.0, false
	ri := int(r)
	for dx := -ri; dx <= ri; dx++ {
		for dz := -ri; dz <= ri; dz++ {
			d2 := float64(dx*dx + dz*dz)
			if d2 > best {
				continue
			}
			for dy := -2; dy <= 2; dy++ {
				if worldgen.IsLava(w.At(bx+dx, by+dy, bz+dz)) {
					best, bx2, bz2, found = d2, float64(bx+dx)+0.5, float64(bz+dz)+0.5, true
					break
				}
			}
		}
	}
	return bx2, bz2, found
}
