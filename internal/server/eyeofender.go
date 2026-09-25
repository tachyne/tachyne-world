package server

import (
	"encoding/binary"
	"math"
)

// EyeOfEnder: not a projectile but a signal. signalTo aims it at the
// structure's locate position — or, when that is more than twelve blocks
// off across the ground, at a point twelve blocks along the way and eight
// up. Each tick it moves by its motion and eases toward the point
// (updateDeltaMovement: the horizontal speed creeps toward the distance,
// the climb toward ±1). It passes through blocks and hits nothing. Eighty
// ticks on it is gone with its death sound: four times in five it drops
// back as an item, else it shatters (level event 2003).

type eyeEntity struct {
	eid        int32
	uuid       [16]byte
	dim        int
	x, y, z    float64
	vx, vy, vz float64
	tx, ty, tz float64
	life       int
	survive    bool
	sx, sy, sz float64
}

const (
	eyeTooFar            = 12.0 // TOO_FAR_DISTANCE
	eyeTooFarLift        = 8.0  // TOO_FAR_SIGNAL_HEIGHT
	eyeLifeTicks         = 80
	levelEventEyeShatter = 2003 // EyeOfEnder: the shatter burst
)

// spawnEye is EnderEyeItem.use's new EyeOfEnder, signalled to (x, 0, z).
func (h *hub) spawnEye(players map[int32]*tracked, dim int, x, y, z float64, tx, ty, tz float64) *eyeEntity {
	e := &eyeEntity{eid: h.allocEID(), dim: dim, x: x, y: y, z: z, sx: x, sy: y, sz: z}
	binary.BigEndian.PutUint32(e.uuid[12:], uint32(e.eid))
	// signalTo.
	dx, dz := tx-x, tz-z
	if hd := math.Hypot(dx, dz); hd > eyeTooFar {
		e.tx, e.ty, e.tz = x+dx/hd*eyeTooFar, y+eyeTooFarLift, z+dz/hd*eyeTooFar
	} else {
		e.tx, e.ty, e.tz = tx, ty, tz
	}
	e.survive = h.rng.Intn(5) > 0
	if h.eyes == nil {
		h.eyes = map[int32]*eyeEntity{}
	}
	h.eyes[e.eid] = e
	h.toNearbyEv(players, dim, x, z, entAdd(e.eid, entityEyeProj, e.uuid, x, y, z, 0, 0))
	return e
}

// updateEyes ticks every eye in flight.
func (h *hub) updateEyes(players map[int32]*tracked) {
	for id, e := range h.eyes {
		nx, ny, nz := e.x+e.vx, e.y+e.vy, e.z+e.vz
		e.vx, e.vy, e.vz = eyeDeltaMovement(e.vx, e.vy, e.vz, nx, ny, nz, e.tx, e.ty, e.tz)
		e.x, e.y, e.z = nx, ny, nz
		if e.life++; e.life > eyeLifeTicks {
			delete(h.eyes, id)
			h.playSoundDim(players, e.dim, "minecraft:entity.ender_eye.death", sndNeutral, e.x, e.y, e.z, 1, 1)
			h.entityGone(players, e.dim, e.eid)
			if e.survive {
				h.spawnItemIn(players, e.dim, int32(itemEnderEye), 1, e.x, e.y, e.z)
			} else {
				h.levelEvent(players, e.dim, levelEventEyeShatter, floorInt(e.x), floorInt(e.y), floorInt(e.z), 0)
			}
			continue
		}
		if e.x != e.sx || e.y != e.sy || e.z != e.sz {
			h.toTracking(players, e.eid, e.dim, e.x, e.z, entMove(e.eid, e.x, e.y, e.z, 0, 0, false))
			e.sx, e.sy, e.sz = e.x, e.y, e.z
		}
	}
}

// eyeDeltaMovement is EyeOfEnder.updateDeltaMovement.
func eyeDeltaMovement(vx, vy, vz, px, py, pz, tx, ty, tz float64) (float64, float64, float64) {
	hx, hz := tx-px, tz-pz
	hl := math.Hypot(hx, hz)
	speed := math.Hypot(vx, vz) + 0.0025*(hl-math.Hypot(vx, vz))
	my := vy
	if hl < 1 {
		speed *= 0.8
		my *= 0.8
	}
	want := -1.0
	if py-vy < ty {
		want = 1
	}
	ny := my + (want-my)*0.015
	if hl < 1e-9 {
		return 0, ny, 0
	}
	return hx * speed / hl, ny, hz * speed / hl
}
