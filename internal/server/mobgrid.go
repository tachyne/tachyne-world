package server

import "math"

// A spatial index over every live mob, so "who is within r of here" is a
// handful of cells rather than a walk of h.mobs. Seven sites did that walk —
// the herd steer for EVERY herd mob EVERY mob tick, breeding partner search,
// pack aggro, the mace shockwave, the golem's target and melee scans — and
// each was O(mobs) per caller, O(mobs²) per tick in the herd case.
//
// The grid is built lazily, at most once per tick, and thrown away whenever a
// mob is added to or removed from h.mobs (the eight sites that touch the map
// call gridDirty). Within a tick mobs move, so a query scans one cell ring
// wider than the radius strictly needs and then checks exact CURRENT
// positions — the cells are a candidate filter, never the answer.
//
// pushMobs keeps its own filtered cells (pushable mobs only, exactly vanilla's
// getPushableEntities) and is not changed; this grid holds everything alive.

type mobGrid struct {
	cells   map[[3]int][]*mob // [dim, cellX, cellZ]
	builtAt uint64            // tick the grid reflects
	valid   bool
}

// grid returns the current tick's index, building it on first use.
func (h *hub) grid() *mobGrid {
	now := h.tick.Load()
	if h.mgrid.valid && h.mgrid.builtAt == now {
		return &h.mgrid
	}
	cells := h.mgrid.cells
	if cells == nil {
		cells = make(map[[3]int][]*mob, len(h.mobs)/2+1)
	} else {
		clear(cells)
	}
	for _, m := range h.mobs {
		if m.dying > 0 && m.health <= 0 {
			continue // a corpse playing its death animation is not a neighbour
		}
		k := [3]int{m.dim, pushCellOf(m.x), pushCellOf(m.z)}
		cells[k] = append(cells[k], m)
	}
	h.mgrid = mobGrid{cells: cells, builtAt: now, valid: true}
	return &h.mgrid
}

// gridDirty forgets the index. Called wherever h.mobs gains or loses a key.
func (h *hub) gridDirty() { h.mgrid.valid = false }

// nearby calls fn for every live mob in dim within r blocks (horizontal) of
// (x,z), in no particular order. Callers apply their own further filters
// (species, hostility, 3-D distance) inside fn.
func (g *mobGrid) nearby(dim int, x, z, r float64, fn func(o *mob)) {
	cr := int(math.Ceil(r/pushCell)) + 1 // +1: positions drift within the tick
	cx, cz := pushCellOf(x), pushCellOf(z)
	r2 := r * r
	for dx := -cr; dx <= cr; dx++ {
		for dz := -cr; dz <= cr; dz++ {
			for _, o := range g.cells[[3]int{dim, cx + dx, cz + dz}] {
				if o.dim != dim {
					continue // moved dimension since the build
				}
				if ddx, ddz := o.x-x, o.z-z; ddx*ddx+ddz*ddz <= r2 {
					fn(o)
				}
			}
		}
	}
}
