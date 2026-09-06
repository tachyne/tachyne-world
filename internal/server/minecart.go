package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/world"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Minecart motion, rolled by the server every tick (vanilla's classic
// behaviour; the "improved" one sits behind a feature flag and is not what
// released clients run).
//
// Since 1.21.2 a minecart has no controlling passenger: even with a player
// aboard, the SERVER moves it and the client only sends its movement keys
// (player_input), which nudge a cart from rest. That is the opposite of a
// boat, whose rider's client drives it and reports the position. So a cart
// is simulated here whether ridden or empty — gravity, the rail line it is
// snapped to, slopes, powered-rail boosts and brakes, the "station start"
// off a solid block, activator rails ejecting the rider — and every viewer
// (the rider included) follows its entity moves.

const (
	cartGravity       = 0.04
	cartGravityWater  = 0.005
	cartMaxSpeed      = 0.4 // blocks per tick on land
	cartMaxSpeedWater = 0.2
	cartSlide         = 0.0078125 // gravity along a sloped rail
	cartAirDrag       = 0.95
	cartBoost         = 0.06 // powered rail, per tick, along the motion
	cartStationPush   = 0.02 // powered rail start-up away from a solid block
)

// railExits gives, per rail shape, the two neighbouring cells a rail leads
// into (dx, dy, dz): a sloped rail's low end is the cell below.
var railExits = [10][2][3]int{
	shapeNS:   {{0, 0, -1}, {0, 0, 1}},
	shapeEW:   {{-1, 0, 0}, {1, 0, 0}},
	shapeAscE: {{-1, -1, 0}, {1, 0, 0}},
	shapeAscW: {{-1, 0, 0}, {1, -1, 0}},
	shapeAscN: {{0, 0, -1}, {0, -1, 1}},
	shapeAscS: {{0, -1, -1}, {0, 0, 1}},
	shapeSE:   {{0, 0, 1}, {1, 0, 0}},
	shapeSW:   {{0, 0, 1}, {-1, 0, 0}},
	shapeNW:   {{0, 0, -1}, {-1, 0, 0}},
	shapeNE:   {{0, 0, -1}, {1, 0, 0}},
}

func isPoweredRail(s uint32) bool   { return s >= poweredRailMin && s <= poweredRailMax }
func isActivatorRail(s uint32) bool { return s >= activatorMin && s <= activatorMax }

// keyAxis folds an opposing key pair into -1/0/1 (vanilla's move intent).
func keyAxis(pos, neg bool) float64 {
	if pos == neg {
		return 0
	}
	if pos {
		return 1
	}
	return -1
}

// inputVector rotates a (left, forward) key intent into world x/z by the
// player's yaw (Entity.getInputVector with speed 1).
func inputVector(left, forward float64, yaw float32) (float64, float64) {
	l2 := left*left + forward*forward
	if l2 < 1e-7 {
		return 0, 0
	}
	if l2 > 1 {
		n := math.Sqrt(l2)
		left, forward = left/n, forward/n
	}
	s, c := math.Sincos(float64(yaw) * math.Pi / 180)
	return left*c - forward*s, forward*c + left*s
}

// cartSolid is the collision test a rolling cart uses: full cubes stop it
// and hold it up; rails, fluids, plants and thin blocks do not.
func cartSolid(s uint32) bool { return worldgen.IsSolidFull(s) }

// cartRailPos is getCurrentBlockPosOrRailBelow: the cell the cart is in, or
// the rail one below when it is riding just above one.
func cartRailPos(w *world.World, v *vehicle) blockPos {
	x, y, z := floorInt(v.x), floorInt(v.y), floorInt(v.z)
	if isAnyRail(w.At(x, y-1, z)) {
		y--
	}
	return blockPos{x, y, z}
}

// cartRailPoint projects a position onto the rail line through its cell
// (vanilla getPos): the point a cart on that rail sits at, sloped rails
// included. ok is false off the rails.
func cartRailPoint(w *world.World, x, y, z float64) (float64, float64, float64, bool) {
	xt, yt, zt := floorInt(x), floorInt(y), floorInt(z)
	if isAnyRail(w.At(xt, yt-1, zt)) {
		yt--
	}
	state := w.At(xt, yt, zt)
	if !isAnyRail(state) {
		return 0, 0, 0, false
	}
	ex := railExits[railShape(state)]
	x0 := float64(xt) + 0.5 + float64(ex[0][0])*0.5
	y0 := float64(yt) + 0.0625 + float64(ex[0][1])*0.5
	z0 := float64(zt) + 0.5 + float64(ex[0][2])*0.5
	x1 := float64(xt) + 0.5 + float64(ex[1][0])*0.5
	y1 := float64(yt) + 0.0625 + float64(ex[1][1])*0.5
	z1 := float64(zt) + 0.5 + float64(ex[1][2])*0.5
	xD, yD, zD := x1-x0, (y1-y0)*2, z1-z0
	var progress float64
	switch {
	case xD == 0:
		progress = z - float64(zt)
	case zD == 0:
		progress = x - float64(xt)
	default:
		progress = ((x-x0)*xD + (z-z0)*zD) * 2
	}
	x, y, z = x0+xD*progress, y0+yD*progress, z0+zD*progress
	if yD < 0 {
		y++
	} else if yD > 0 {
		y += 0.5
	}
	return x, y, z, true
}

// tickMinecart is one tick of AbstractMinecart.tick for the classic
// behaviour: gravity, on-rail or free motion, the facing/flip bookkeeping,
// then the rider and the viewers follow.
func (h *hub) tickMinecart(players map[int32]*tracked, v *vehicle) {
	w := h.worldFor(v.dim)
	if w == nil {
		return
	}
	xo, zo := v.x, v.z
	inWater := worldgen.IsWater(w.At(floorInt(v.x), floorInt(v.y), floorInt(v.z)))
	if inWater {
		v.vy -= cartGravityWater
	} else {
		v.vy -= cartGravity
	}
	pos := cartRailPos(w, v)
	state := w.At(pos.x, pos.y, pos.z)
	if isAnyRail(state) {
		h.cartMoveAlongTrack(players, w, v, pos, state, inWater)
		if isActivatorRail(state) && railPowered(state) && v.rider != 0 {
			if t := players[v.rider]; t != nil { // a live activator rail throws the rider off
				h.dismount(players, t)
			}
		}
	} else {
		h.cartComeOffTrack(w, v, inWater)
	}
	// Facing follows the motion; the flip flag keeps the model from spinning
	// through 180° when the cart reverses.
	yawO := v.yawO
	if xDiff, zDiff := xo-v.x, zo-v.z; xDiff*xDiff+zDiff*zDiff > 0.001 {
		v.yaw = float32(math.Atan2(zDiff, xDiff) * 180 / math.Pi)
		if v.flipped {
			v.yaw += 180
		}
	}
	if d := wrapDegrees(float64(v.yaw - yawO)); d < -170 || d >= 170 {
		v.yaw += 180
		v.flipped = !v.flipped
	}
	v.yaw = float32(math.Mod(float64(v.yaw), 360))
	v.yawO = v.yaw
	if v.rider != 0 {
		if t := players[v.rider]; t != nil {
			t.x, t.y, t.z = v.x, v.y+0.6, v.z // the rider's hub position drives streaming + interest
		}
	}
	if v.x != v.sx || v.y != v.sy || v.z != v.sz || v.yaw != v.syaw {
		h.toNearbyEv(players, v.dim, v.x, v.z, entMove(v.eid, v.x, v.y, v.z, v.yaw, 0, v.onGround))
		v.sx, v.sy, v.sz, v.syaw = v.x, v.y, v.z, v.yaw
	}
}

// cartMoveAlongTrack is OldMinecartBehavior.moveAlongTrack.
func (h *hub) cartMoveAlongTrack(players map[int32]*tracked, w *world.World, v *vehicle, pos blockPos, state uint32, inWater bool) {
	x, y, z := v.x, v.y, v.z
	_, oldY, _, oldOK := cartRailPoint(w, x, y, z)
	y = float64(pos.y)
	powerTrack, haltTrack := false, false
	if isPoweredRail(state) {
		powerTrack = railPowered(state)
		haltTrack = !powerTrack
	}
	slide := cartSlide
	if inWater {
		slide *= 0.2
	}
	shape := railShape(state)
	switch shape {
	case shapeAscE:
		v.vx -= slide
		y++
	case shapeAscW:
		v.vx += slide
		y++
	case shapeAscN:
		v.vz += slide
		y++
	case shapeAscS:
		v.vz -= slide
		y++
	}
	ex := railExits[shape]
	xD, zD := float64(ex[1][0]-ex[0][0]), float64(ex[1][2]-ex[0][2])
	length := math.Sqrt(xD*xD + zD*zD)
	if v.vx*xD+v.vz*zD < 0 {
		xD, zD = -xD, -zD
	}
	pow := math.Min(2, math.Hypot(v.vx, v.vz))
	v.vx, v.vz = pow*xD/length, pow*zD/length
	// A rider's keys only get a resting cart going (0.001 per tick of
	// intent); once it rolls, the rails decide.
	if v.rider != 0 {
		if t := players[v.rider]; t != nil && (t.inLeft != 0 || t.inForward != 0) {
			ix, iz := inputVector(t.inLeft, t.inForward, t.yaw)
			if v.vx*v.vx+v.vz*v.vz < 0.01 {
				v.vx += ix * 0.001
				v.vz += iz * 0.001
				haltTrack = false
			}
		}
	}
	if haltTrack {
		if math.Hypot(v.vx, v.vz) < 0.03 {
			v.vx, v.vy, v.vz = 0, 0, 0
		} else {
			v.vx, v.vy, v.vz = v.vx*0.5, 0, v.vz*0.5
		}
	}
	// Snap onto the rail line through this cell.
	x0 := float64(pos.x) + 0.5 + float64(ex[0][0])*0.5
	z0 := float64(pos.z) + 0.5 + float64(ex[0][2])*0.5
	x1 := float64(pos.x) + 0.5 + float64(ex[1][0])*0.5
	z1 := float64(pos.z) + 0.5 + float64(ex[1][2])*0.5
	xD, zD = x1-x0, z1-z0
	var progress float64
	switch {
	case xD == 0:
		progress = z - float64(pos.z)
	case zD == 0:
		progress = x - float64(pos.x)
	default:
		progress = ((x-x0)*xD + (z-z0)*zD) * 2
	}
	v.x, v.y, v.z = x0+xD*progress, y, z0+zD*progress
	scale := 1.0
	if v.rider != 0 {
		scale = 0.75
	}
	maxSpeed := cartMaxSpeed
	if inWater {
		maxSpeed = cartMaxSpeedWater
	}
	h.cartMove(w, v, clampF(scale*v.vx, -maxSpeed, maxSpeed), 0, clampF(scale*v.vz, -maxSpeed, maxSpeed))
	if ex[0][1] != 0 && floorInt(v.x)-pos.x == ex[0][0] && floorInt(v.z)-pos.z == ex[0][2] {
		v.y += float64(ex[0][1])
	} else if ex[1][1] != 0 && floorInt(v.x)-pos.x == ex[1][0] && floorInt(v.z)-pos.z == ex[1][2] {
		v.y += float64(ex[1][1])
	}
	// Natural slowdown: an empty cart bleeds 4% a tick, a ridden one 0.3%.
	f := 0.96
	if v.rider != 0 {
		f = 0.997
	}
	v.vx, v.vy, v.vz = v.vx*f, 0, v.vz*f
	if inWater {
		v.vx, v.vz = v.vx*0.95, v.vz*0.95
	}
	if _, newY, _, ok := cartRailPoint(w, v.x, v.y, v.z); ok && oldOK {
		// Height lost along the rail becomes speed (and height gained costs it).
		speed := (oldY - newY) * 0.05
		if other := math.Hypot(v.vx, v.vz); other > 0 {
			k := (other + speed) / other
			v.vx, v.vz = v.vx*k, v.vz*k
		}
		v.y = newY
	}
	if xn, zn := floorInt(v.x), floorInt(v.z); xn != pos.x || zn != pos.z {
		other := math.Hypot(v.vx, v.vz)
		v.vx, v.vz = other*float64(xn-pos.x), other*float64(zn-pos.z)
	}
	if powerTrack {
		if sl := math.Hypot(v.vx, v.vz); sl > 0.01 {
			v.vx += v.vx / sl * cartBoost
			v.vz += v.vz / sl * cartBoost
		} else {
			// A resting cart on a live powered rail with a solid block at one
			// end sets off the other way — the classic station.
			dx, dz := v.vx, v.vz
			switch shape {
			case shapeEW:
				if conducts(w.At(pos.x-1, pos.y, pos.z)) {
					dx = cartStationPush
				} else if conducts(w.At(pos.x+1, pos.y, pos.z)) {
					dx = -cartStationPush
				}
			case shapeNS:
				if conducts(w.At(pos.x, pos.y, pos.z-1)) {
					dz = cartStationPush
				} else if conducts(w.At(pos.x, pos.y, pos.z+1)) {
					dz = -cartStationPush
				}
			default:
				return
			}
			v.vx, v.vz = dx, dz
		}
	}
}

// cartComeOffTrack is AbstractMinecart.comeOffTrack: a cart off the rails
// falls, skids to a halt on the ground and drifts in the air.
func (h *hub) cartComeOffTrack(w *world.World, v *vehicle, inWater bool) {
	maxSpeed := cartMaxSpeed
	if inWater {
		maxSpeed = cartMaxSpeedWater
	}
	v.vx, v.vz = clampF(v.vx, -maxSpeed, maxSpeed), clampF(v.vz, -maxSpeed, maxSpeed)
	if v.onGround {
		v.vx, v.vy, v.vz = v.vx*0.5, v.vy*0.5, v.vz*0.5
	}
	h.cartMove(w, v, v.vx, v.vy, v.vz)
	if !v.onGround {
		v.vx, v.vy, v.vz = v.vx*cartAirDrag, v.vy*cartAirDrag, v.vz*cartAirDrag
	}
}

// cartMove is the cart's Entity.move: each axis in turn against full-cube
// collision, zeroing the velocity into a wall and landing on a floor.
func (h *hub) cartMove(w *world.World, v *vehicle, dx, dy, dz float64) {
	if dx != 0 {
		nx := v.x + dx
		edge := nx + math.Copysign(0.49, dx)
		if cartSolid(w.At(floorInt(edge), floorInt(v.y), floorInt(v.z))) {
			v.vx = 0
		} else {
			v.x = nx
		}
	}
	if dz != 0 {
		nz := v.z + dz
		edge := nz + math.Copysign(0.49, dz)
		if cartSolid(w.At(floorInt(v.x), floorInt(v.y), floorInt(edge))) {
			v.vz = 0
		} else {
			v.z = nz
		}
	}
	ny := v.y + dy
	if dy < 0 && cartSolid(w.At(floorInt(v.x), floorInt(ny), floorInt(v.z))) {
		v.y = float64(floorInt(ny) + 1) // landed on the block below
		v.vy = 0
		v.onGround = true
		return
	}
	v.y = ny
	v.onGround = dy <= 0 && cartSolid(w.At(floorInt(v.x), floorInt(v.y-0.001), floorInt(v.z)))
}
