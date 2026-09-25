package server

// Fluid current on mobs. Vanilla's EntityFluidInteraction runs for every
// entity standing in a fluid: every cell of the fluid its box overlaps (from
// the fluid's surface down to the box's floor) adds that cell's flow — scaled
// down by the depth while the entity is in less than 0.4 of it — the sum is
// NORMALISED (players are the exception — they keep the magnitude), scaled by
// 0.014 for water and, for lava, 0.007 where lava is fast (the Nether) or
// 0.0023 elsewhere, and added to the delta movement. It is what carries
// anything loose downstream.
//
// Water animals, axolotls, turtles, frogs and nautiluses are not pushed
// (isPushedByFluid). The mob step is an x/z walk, so the vertical part of
// the current — a waterfall pressing down — has nowhere to go and is dropped.

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// waterPushPerTick and the lava scales are vanilla's. The mob pass runs
// every mobMoveInterval ticks, so one application covers that many ticks —
// the same reasoning the enderman carry odds use.
const (
	waterPushPerTick    = 0.014
	lavaFastPushPerTick = 0.007
	lavaSlowPushPerTick = 0.0023333333333333335
)

// notPushedByFluid are the species whose isPushedByFluid is false.
var notPushedByFluid = entitySet("cod", "salmon", "pufferfish", "tropical_fish", "squid", "glow_squid",
	"dolphin", "axolotl", "turtle", "frog", "nautilus", "zombie_nautilus", "tadpole")

// applyFluidPush adds the currents a mob stands in to its shove for this step.
// It rides pushX/pushZ rather than the steering velocity because vanilla adds
// it after the AI has moved and outside the movement-speed clamp: a current is
// meant to be able to carry a mob faster than it walks.
func (h *hub) applyFluidPush(m *mob) {
	if m.dying > 0 || m.statik || notPushedByFluid[m.etype] {
		return
	}
	w := h.worldFor(m.dim)
	if w == nil {
		return
	}
	b := m.box()
	half := b.w/2 - 0.001
	minY := m.y + 0.001
	x0, x1 := floorInt(m.x-half), floorInt(m.x+half)
	y0, y1 := floorInt(minY), floorInt(m.y+b.h-0.001)
	z0, z1 := floorInt(m.z-half), floorInt(m.z+half)
	type acc struct {
		height     float64
		fx, fy, fz float64
		n          int
	}
	var water, lava acc
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				st := w.At(x, y, z)
				isLava := worldgen.IsLava(st)
				if !isLava && !worldgen.IsWater(st) {
					continue
				}
				top := float64(y) + fluidCellHeight(w.At(x, y+1, z), st, isLava)
				if top < minY {
					continue
				}
				a := &water
				if isLava {
					a = &lava
				}
				a.height = math.Max(a.height, top-m.y)
				fx, fy, fz, _ := h.fluidFlowOf(m.dim, blockPos{x, y, z}, isLava)
				if a.height < 0.4 {
					fx, fy, fz = fx*a.height, fy*a.height, fz*a.height
				}
				a.fx, a.fy, a.fz, a.n = a.fx+fx, a.fy+fy, a.fz+fz, a.n+1
			}
		}
	}
	apply := func(a acc, scale float64) {
		if a.n == 0 {
			return
		}
		l := math.Sqrt(a.fx*a.fx + a.fy*a.fy + a.fz*a.fz)
		if l*l < 1e-5 {
			return
		}
		step := scale * mobMoveInterval / l // normalised, then scaled
		m.pushX += a.fx * step
		m.pushZ += a.fz * step
	}
	apply(water, waterPushPerTick)
	if m.dim == dimNether { // FAST_LAVA
		apply(lava, lavaFastPushPerTick)
	} else {
		apply(lava, lavaSlowPushPerTick)
	}
}

// fluidCellHeight is FluidState.getHeight: a full block under the same fluid,
// else amount/9.
func fluidCellHeight(above, st uint32, lava bool) float64 {
	if lava && worldgen.IsLava(above) || !lava && worldgen.IsWater(above) {
		return 1
	}
	return fluidAmountHeight(st, lava)
}

func fluidAmountHeight(st uint32, lava bool) float64 {
	base := worldgen.WaterBase
	if lava {
		base = worldgen.LavaBase
	}
	lvl := worldgen.FluidLevel(st, base)
	amount := 8
	if lvl >= 1 && lvl <= 7 {
		amount = 8 - lvl
	}
	return float64(amount) / 9
}

// fluidFlowOf is FlowingFluid.getFlow for a water or lava cell (fluidFlow is
// the water form the item physics uses).
func (h *hub) fluidFlowOf(dim int, pos blockPos, lava bool) (x, y, z float64, ok bool) {
	if !lava {
		return h.fluidFlow(dim, pos)
	}
	w := h.worldFor(dim)
	st := w.At(pos.x, pos.y, pos.z)
	if !worldgen.IsLava(st) {
		return 0, 0, 0, false
	}
	own := fluidAmountHeight(st, true)
	var fx, fz float64
	for _, d := range horizNeighbors {
		nx, nz := pos.x+d.x, pos.z+d.z
		ns := w.At(nx, pos.y, nz)
		var dist float64
		switch {
		case worldgen.IsLava(ns):
			dist = own - fluidAmountHeight(ns, true)
		case worldgen.IsWater(ns):
			continue
		case !worldgen.Collides(ns):
			if bs := w.At(nx, pos.y-1, nz); worldgen.IsLava(bs) {
				dist = own - (fluidAmountHeight(bs, true) - 0.8888889)
			}
		}
		fx += float64(d.x) * dist
		fz += float64(d.z) * dist
	}
	var fy float64
	if worldgen.FluidLevel(st, worldgen.LavaBase) >= 8 { // falling beside a solid face: pulled down
		for _, d := range horizNeighbors {
			if worldgen.Collides(w.At(pos.x+d.x, pos.y, pos.z+d.z)) || worldgen.Collides(w.At(pos.x+d.x, pos.y+1, pos.z+d.z)) {
				if n := math.Hypot(fx, fz); n > 0 {
					fx, fz = fx/n, fz/n
				}
				fy = -6
				break
			}
		}
	}
	n := math.Sqrt(fx*fx + fy*fy + fz*fz)
	if n < 1e-9 {
		return 0, 0, 0, false
	}
	return fx / n, fy / n, fz / n, true
}
