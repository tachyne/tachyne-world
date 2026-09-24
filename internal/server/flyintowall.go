package server

// Flying into a wall. Entity.move prices a glider's crash: while fall-flying,
// the movement the entity INTENDED this tick is compared against what it
// actually travelled, and the difference — times ten, less three — is dealt as
// fly_into_wall damage. It is the reason an elytra flight into a cliff is
// fatal and a landing is not.
//
// The engine sees positions, not the client's intended movement, so the
// PREVIOUS tick's horizontal travel stands in for the intent: a glider that
// was moving and is suddenly not has hit something. That inference alone would
// misfire on a dropped or batched move packet, so the damage is only dealt
// when there is really a wall in the direction of travel.

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

const (
	// flyIntoWallMinDrop is the smallest one-tick loss of speed worth looking
	// at. Below it the formula would price the hit at nothing anyway (a drop
	// of 0.3 gives exactly zero damage), so it is also the point where a
	// glider merely easing off costs nothing.
	flyIntoWallMinDrop = 0.3
	// flyIntoWallReach is how far ahead a wall has to be to have been the
	// thing that stopped the flight.
	flyIntoWallReach = 1.5
)

// flyIntoWall deals the kinetic damage of a glide that ended against
// something, and keeps the speed record the next tick compares against.
func (h *hub) flyIntoWall(players map[int32]*tracked, t *tracked, e evMove) {
	if !t.fallFlying || e.teleport {
		t.glideVX, t.glideVZ = 0, 0
		return
	}
	prevX, prevZ := t.glideVX, t.glideVZ
	t.glideVX, t.glideVZ = e.x-t.x, e.z-t.z
	prev := math.Hypot(prevX, prevZ)
	cur := math.Hypot(t.glideVX, t.glideVZ)
	drop := prev - cur
	if drop < flyIntoWallMinDrop {
		return
	}
	if !h.wallAhead(t, prevX, prevZ, prev) {
		return // a lost packet or an eased-off glide, not a collision
	}
	dmg := float32(drop*10 - 3)
	if dmg <= 0 {
		return
	}
	// Entity.move plays the generic hurt fall alongside the damage.
	h.playSoundDim(players, t.dim, "minecraft:entity.player.hurt", sndPlayer, t.x, t.y, t.z, 1, 1)
	h.damageOf(players, t, dmg, dtFlyIntoWall)
}

// wallAhead reports whether a solid block sits within flyIntoWallReach of the
// player along the direction they were travelling, at their feet or their
// chest — the obstacle that would have stopped them.
func (h *hub) wallAhead(t *tracked, dx, dz, speed float64) bool {
	if speed <= 0 {
		return false
	}
	w := h.worldFor(t.dim)
	if w == nil {
		return false
	}
	ux, uz := dx/speed*flyIntoWallReach, dz/speed*flyIntoWallReach
	for _, dy := range []float64{0.2, 1.2} {
		x, y, z := floorInt(t.x+ux), floorInt(t.y+dy), floorInt(t.z+uz)
		if worldgen.IsSolid(w.At(x, y, z)) {
			return true
		}
	}
	return false
}
