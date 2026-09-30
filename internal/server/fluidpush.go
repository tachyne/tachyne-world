package server

// Fluid current (Entity.updateFluidInteraction over EntityFluidInteraction).
// For an entity in a fluid, every cell of the fluid its box overlaps (from
// the fluid's surface down to the box's floor) adds that cell's flow —
// scaled down by the depth while the entity is in less than 0.4 of it — and
// the sum is NORMALISED (players are the exception: they take the average),
// scaled by 0.014 for water and, for lava, 0.007 where lava is fast (the
// Nether) or 0.0023 elsewhere, and added to the delta movement. An entity
// that is all but still (under 0.003 a tick on both horizontal axes) is
// given at least 0.0045 of it, so a slow current still starts it moving.
// It is what carries anything loose downstream.
//
// Water animals, axolotls, turtles, frogs and nautiluses are not pushed
// (isPushedByFluid), nor is a drowned while it swims. A walking mob's step
// is an x/z walk that seats it on the floor, so the vertical part of the
// current reaches only a mob that moves vertically (a swimmer); items and
// primed TNT carry all three components.
//
// Players are not pushed here: a player's movement is the client's, and
// the client runs this same current on itself.

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
	fluidPushStillSpeed = 0.003                 // CurrentAccumulator.applyTo: "all but still"
	fluidPushMinImpulse = 0.0045000000000000005 // …and the least push it then gets
)

// notPushedByFluid are the species whose isPushedByFluid is false.
var notPushedByFluid = entitySet("cod", "salmon", "pufferfish", "tropical_fish", "squid", "glow_squid",
	"dolphin", "axolotl", "turtle", "frog", "nautilus", "zombie_nautilus", "tadpole")

// fluidAcc is one fluid's CurrentAccumulator (plus its Tracker's height).
type fluidAcc struct {
	height     float64 // how deep the entity is in this fluid, from its box floor
	fx, fy, fz float64 // the summed flow
	n          int     // cells summed
}

// fluidCurrents is EntityFluidInteraction.update for a box: bx0..bx1 etc.
// are the entity's bounding box, which it deflates by 0.001 as
// getFluidInteractionBox does. It returns the water and the lava
// accumulators.
func (h *hub) fluidCurrents(dim int, bx0, by0, bz0, bx1, by1, bz1 float64) (water, lava fluidAcc) {
	w := h.worldFor(dim)
	if w == nil {
		return
	}
	const margin = 0.001
	entityY := by0 // getBoundingBox().minY, not deflated
	bx0, by0, bz0 = bx0+margin, by0+margin, bz0+margin
	bx1, by1, bz1 = bx1-margin, by1-margin, bz1-margin
	x0, y0, z0 := floorInt(bx0), floorInt(by0), floorInt(bz0)
	x1, y1, z1 := int(math.Ceil(bx1))-1, int(math.Ceil(by1))-1, int(math.Ceil(bz1))-1
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				st := w.At(x, y, z)
				isLava := worldgen.IsLava(st)
				if !isLava && !worldgen.IsWater(st) {
					continue
				}
				top := float64(y) + fluidCellHeight(w.At(x, y+1, z), st, isLava)
				if top < by0 {
					continue
				}
				a := &water
				if isLava {
					a = &lava
				}
				a.height = math.Max(a.height, top-entityY)
				fx, fy, fz, _ := h.fluidFlowOf(dim, blockPos{x, y, z}, isLava)
				if a.height < 0.4 {
					fx, fy, fz = fx*a.height, fy*a.height, fz*a.height
				}
				a.fx, a.fy, a.fz, a.n = a.fx+fx, a.fy+fy, a.fz+fz, a.n+1
			}
		}
	}
	return water, lava
}

// impulse is CurrentAccumulator.applyTo: the push one tick of this current
// gives an entity whose delta movement is (oldX, _, oldZ). ok=false means
// none (not in the fluid, or a current too weak to count).
func (a fluidAcc) impulse(scale float64, player bool, oldX, oldZ float64) (x, y, z float64, ok bool) {
	if a.n == 0 {
		return 0, 0, 0, false
	}
	l2 := a.fx*a.fx + a.fy*a.fy + a.fz*a.fz
	if l2 < 1e-5 {
		return 0, 0, 0, false
	}
	if player {
		k := 1 / float64(a.n)
		x, y, z = a.fx*k, a.fy*k, a.fz*k
	} else {
		l := math.Sqrt(l2)
		x, y, z = a.fx/l, a.fy/l, a.fz/l
	}
	x, y, z = x*scale, y*scale, z*scale
	if math.Abs(oldX) < fluidPushStillSpeed && math.Abs(oldZ) < fluidPushStillSpeed {
		if l := math.Sqrt(x*x + y*y + z*z); l < fluidPushMinImpulse && l > 0 {
			k := fluidPushMinImpulse / l
			x, y, z = x*k, y*k, z*k
		}
	}
	return x, y, z, true
}

// lavaPushScale is the lava current's scale in a dimension (FAST_LAVA).
func lavaPushScale(dim int) float64 {
	if dimType(dim).FastLava {
		return lavaFastPushPerTick
	}
	return lavaSlowPushPerTick
}

// fluidPush is the whole current on a non-player entity for one tick:
// water's then lava's impulse, summed.
func (h *hub) fluidPush(dim int, bx0, by0, bz0, bx1, by1, bz1, oldX, oldZ float64) (x, y, z float64) {
	water, lava := h.fluidCurrents(dim, bx0, by0, bz0, bx1, by1, bz1)
	if ix, iy, iz, ok := water.impulse(waterPushPerTick, false, oldX, oldZ); ok {
		x, y, z = x+ix, y+iy, z+iz
		oldX, oldZ = oldX+ix, oldZ+iz // addDeltaMovement lands before lava's turn
	}
	if ix, iy, iz, ok := lava.impulse(lavaPushScale(dim), false, oldX, oldZ); ok {
		x, y, z = x+ix, y+iy, z+iz
	}
	return x, y, z
}

// applyFluidPush adds the currents a mob stands in to its shove for this step.
// It rides pushX/pushZ rather than the steering velocity because vanilla adds
// it after the AI has moved and outside the movement-speed clamp: a current is
// meant to be able to carry a mob faster than it walks. A swimmer, which
// moves vertically, takes the vertical part too.
func (h *hub) applyFluidPush(m *mob) {
	if m.dying > 0 || m.statik || notPushedByFluid[m.etype] {
		return
	}
	if h.worldFor(m.dim) == nil {
		return
	}
	amphibiousSwim := isAmphibious(m.etype) && h.inWater(m.dim, m.x, m.y, m.z)
	if m.etype == entityDrowned && amphibiousSwim {
		return // Drowned.isPushedByFluid: not while swimming
	}
	b := m.box()
	half := b.w / 2
	// The step's velocity per tick is what applyTo compares with its
	// "all but still" threshold.
	oldX := (m.vx + m.pushX) / mobMoveInterval
	oldZ := (m.vz + m.pushZ) / mobMoveInterval
	px, py, pz := h.fluidPush(m.dim, m.x-half, m.y, m.z-half, m.x+half, m.y+b.h, m.z+half, oldX, oldZ)
	m.pushX += px * mobMoveInterval
	m.pushZ += pz * mobMoveInterval
	if m.swims || amphibiousSwim {
		m.vy += py * mobMoveInterval
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
