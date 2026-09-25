package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// An unsteered boat (AbstractBoat.tick with nobody at the front who can
// row: empty, or a mob in the front seat) is the server's to move, and it
// is not still. floatBoat reads the water it sits in — IN_WATER bobs it to
// the surface, UNDER_WATER lifts it slowly, UNDER_FLOWING_WATER holds it
// down, ON_LAND grips by the blocks' friction, IN_AIR lets it fall — and the
// flowing water it floats in carries it (Entity's fluid pushing, 0.014).
// Sixty ticks under water throws its passengers out.

const (
	boatBoxHalfW     = 0.6875 // EntityType sized(1.375, 0.5625)
	boatBoxH         = 0.5625
	boatGravity      = 0.04 // AbstractBoat.getDefaultGravity
	boatOutOfControl = 60
)

const (
	boatInWater = iota
	boatUnderWater
	boatUnderFlowingWater
	boatOnLand
	boatInAir
)

// fluidHeightAt is FluidState.getHeight for water: amount/9, or the full
// block when the same fluid lies above; waterlogged blocks are sources.
func fluidHeightAt(w *world.World, x, y, z int) (h float64, source, ok bool) {
	st := w.At(x, y, z)
	if !worldgen.HoldsWater(st) {
		return 0, false, false
	}
	if worldgen.HoldsWater(w.At(x, y+1, z)) {
		h = 1
	} else if worldgen.IsWater(st) {
		h = flowHeight(st)
	} else {
		h = 8.0 / 9
	}
	source = !worldgen.IsWater(st) || worldgen.FluidLevel(st, worldgen.WaterBase) == 0
	return h, source, true
}

// boatStatus is AbstractBoat.getStatus, with the water level it found.
func boatStatus(w *world.World, v *vehicle) (status int, waterLevel, landFriction float64) {
	minX, maxX := v.x-boatBoxHalfW, v.x+boatBoxHalfW
	minZ, maxZ := v.z-boatBoxHalfW, v.z+boatBoxHalfW
	minY, maxY := v.y, v.y+boatBoxH
	x0, x1 := floorInt(minX), int(math.Ceil(maxX))
	z0, z1 := floorInt(minZ), int(math.Ceil(maxZ))
	// isUnderwater: water over the box's top.
	under := false
	for x := x0; x < x1; x++ {
		for y := floorInt(maxY); y < int(math.Ceil(maxY+0.001)); y++ {
			for z := z0; z < z1; z++ {
				if h, src, ok := fluidHeightAt(w, x, y, z); ok && maxY+0.001 < float64(y)+h {
					if !src {
						return boatUnderFlowingWater, maxY, 0
					}
					under = true
				}
			}
		}
	}
	if under {
		return boatUnderWater, maxY, 0
	}
	// checkInWater: water about the bottom.
	level, in := -math.MaxFloat64, false
	for x := x0; x < x1; x++ {
		for y := floorInt(minY); y < int(math.Ceil(minY+0.001)); y++ {
			for z := z0; z < z1; z++ {
				if h, _, ok := fluidHeightAt(w, x, y, z); ok {
					top := float64(y) + h
					level = math.Max(level, top)
					in = in || minY < top
				}
			}
		}
	}
	if in {
		return boatInWater, level, 0
	}
	// getGroundFriction: the blocks under the bottom, lily pads aside.
	sum, n := 0.0, 0
	gy := floorInt(minY - 0.001)
	for x := x0; x < x1; x++ {
		for z := z0; z < z1; z++ {
			if st := w.At(x, gy, z); worldgen.Collides(st) && !isLilyPad(st) {
				sum += blockFriction(st)
				n++
			}
		}
	}
	if n > 0 {
		return boatOnLand, level, sum / float64(n)
	}
	return boatInAir, level, 0
}

// boatWaterLevelAbove is getWaterLevelAbove: the surface over a boat that
// fell into water.
func boatWaterLevelAbove(w *world.World, v *vehicle) float64 {
	maxY := v.y + boatBoxH
	x0, x1 := floorInt(v.x-boatBoxHalfW), int(math.Ceil(v.x+boatBoxHalfW))
	z0, z1 := floorInt(v.z-boatBoxHalfW), int(math.Ceil(v.z+boatBoxHalfW))
	top := int(math.Ceil(maxY - v.lastYd))
	for y := floorInt(maxY); y < top; y++ {
		best := 0.0
		for x := x0; x < x1 && best < 1; x++ {
			for z := z0; z < z1 && best < 1; z++ {
				if h, _, ok := fluidHeightAt(w, x, y, z); ok {
					best = math.Max(best, h)
				}
			}
		}
		if best < 1 {
			return float64(y) + best
		}
	}
	return float64(top + 1)
}

// tickBoatDrift runs one tick of an unsteered boat.
func (h *hub) tickBoatDrift(players map[int32]*tracked, v *vehicle) {
	if h.boatController(v) != 0 {
		v.boatVX, v.boatVY, v.boatVZ = 0, 0, 0 // the rower's client moves it
		v.boatStatus = boatInWater
		return
	}
	w := h.worldFor(v.dim)
	if w == nil || !w.Ticking(int32(floorInt(v.x)>>4), int32(floorInt(v.z)>>4)) {
		return
	}
	old := v.boatStatus
	status, level, land := boatStatus(w, v)
	v.boatStatus = status
	if status == boatUnderWater || status == boatUnderFlowingWater {
		if v.boatOutOfControl++; v.boatOutOfControl >= boatOutOfControl {
			h.ejectBoat(players, v)
		}
	} else {
		v.boatOutOfControl = 0
	}
	h.boatFluidPush(w, v)
	// floatBoat.
	switch {
	case old == boatInAir && status != boatInAir && status != boatOnLand:
		target := boatWaterLevelAbove(w, v) - boatBoxH + 0.101
		if !h.boatBoxCollides(w, v.x, target, v.z) {
			v.y = target
			v.boatVY, v.lastYd = 0, 0
		}
		v.boatStatus = boatInWater
	default:
		vspeed, buoyancy, inv := -boatGravity, 0.0, 0.05
		switch status {
		case boatInWater:
			buoyancy, inv = (level-v.y)/boatBoxH, 0.9
		case boatUnderFlowingWater:
			vspeed, inv = -7.0e-4, 0.9
		case boatUnderWater:
			buoyancy, inv = 0.01, 0.45
		case boatInAir:
			inv = 0.9
		case boatOnLand:
			inv = land
		}
		v.boatVX, v.boatVY, v.boatVZ = v.boatVX*inv, v.boatVY+vspeed, v.boatVZ*inv
		if buoyancy > 0 {
			v.boatVY = (v.boatVY + buoyancy*(boatGravity/0.65)) * 0.75
		}
	}
	ox, oy, oz := v.x, v.y, v.z
	h.boatMove(w, v)
	v.lastYd = v.y - oy
	if v.x == ox && v.y == oy && v.z == oz {
		return
	}
	for _, id := range []int32{v.rider, v.rider2} {
		if t := players[id]; t != nil {
			t.x, t.y, t.z = v.x, v.y+0.6, v.z
		}
	}
	if math.Abs(v.x-v.sx) > 1e-4 || math.Abs(v.y-v.sy) > 1e-4 || math.Abs(v.z-v.sz) > 1e-4 {
		v.sx, v.sy, v.sz = v.x, v.y, v.z
		h.toTracking(players, v.eid, v.dim, v.x, v.z, entMove(v.eid, v.x, v.y, v.z, v.yaw, 0, v.boatStatus == boatOnLand))
	}
}

// ejectBoat is ejectPassengers after sixty ticks under water.
func (h *hub) ejectBoat(players map[int32]*tracked, v *vehicle) {
	h.ejectPlayers(players, v)
	h.releaseCartMob(players, v)
	h.toTracking(players, v.eid, v.dim, v.x, v.z, passengersBody(v.eid, v.passengers()...))
}

// boatFluidPush is EntityFluidInteraction's water current: the flow of
// every water cell reaching the boat's bottom (scaled down while the water
// stands under 0.4 above its feet), summed, normalised and scaled by 0.014.
func (h *hub) boatFluidPush(w *world.World, v *vehicle) {
	var fx, fy, fz, height float64
	for x := floorInt(v.x - boatBoxHalfW + 0.001); x <= floorInt(v.x+boatBoxHalfW-0.001); x++ {
		for y := floorInt(v.y + 0.001); y <= floorInt(v.y+boatBoxH-0.001); y++ {
			for z := floorInt(v.z - boatBoxHalfW + 0.001); z <= floorInt(v.z+boatBoxHalfW-0.001); z++ {
				hgt, _, ok := fluidHeightAt(w, x, y, z)
				if !ok || float64(y)+hgt < v.y+0.001 {
					continue
				}
				height = math.Max(height, float64(y)+hgt-v.y)
				cx, cy, cz, flows := h.fluidFlow(v.dim, blockPos{x, y, z})
				if !flows {
					continue
				}
				if height < 0.4 {
					cx, cy, cz = cx*height, cy*height, cz*height
				}
				fx, fy, fz = fx+cx, fy+cy, fz+cz
			}
		}
	}
	l2 := fx*fx + fy*fy + fz*fz
	if l2 < 1e-5 {
		return
	}
	l := math.Sqrt(l2)
	v.boatVX += fx / l * waterFlowScale
	v.boatVY += fy / l * waterFlowScale
	v.boatVZ += fz / l * waterFlowScale
}

// boatBoxCollides reports a solid block in the boat's box at (x, y, z).
func (h *hub) boatBoxCollides(w *world.World, x, y, z float64) bool {
	for bx := floorInt(x - boatBoxHalfW + 1e-7); bx <= floorInt(x+boatBoxHalfW-1e-7); bx++ {
		for by := floorInt(y + 1e-7); by <= floorInt(y+boatBoxH-1e-7); by++ {
			for bz := floorInt(z - boatBoxHalfW + 1e-7); bz <= floorInt(z+boatBoxHalfW-1e-7); bz++ {
				if st := w.At(bx, by, bz); worldgen.Collides(st) && !isLilyPad(st) {
					return true
				}
			}
		}
	}
	return false
}

// boatMove is Entity.move for the boat's box, an axis at a time (y, then x
// and z): a blocked axis stops there and loses its speed.
func (h *hub) boatMove(w *world.World, v *vehicle) {
	if dy := v.boatVY; dy != 0 {
		if ny := v.y + dy; !h.boatBoxCollides(w, v.x, ny, v.z) {
			v.y = ny
		} else {
			if dy < 0 { // land on the top of what is under it
				v.y = math.Max(float64(floorInt(ny))+1, ny)
				for h.boatBoxCollides(w, v.x, v.y, v.z) && v.y < ny+1 {
					v.y = float64(floorInt(v.y) + 1)
				}
			}
			v.boatVY = 0
		}
	}
	if dx := v.boatVX; dx != 0 {
		if nx := v.x + dx; !h.boatBoxCollides(w, nx, v.y, v.z) {
			v.x = nx
		} else {
			v.boatVX = 0
		}
	}
	if dz := v.boatVZ; dz != 0 {
		if nz := v.z + dz; !h.boatBoxCollides(w, v.x, v.y, nz) {
			v.z = nz
		} else {
			v.boatVZ = 0
		}
	}
}
