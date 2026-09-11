package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Big dripleaf tilt (BigDripleafBlock). Something standing on the leaf sets
// it UNSTABLE; ten ticks later it tips to PARTIAL, ten more to FULL — when it
// no longer carries anything — and a hundred ticks after that it springs
// back to NONE. A redstone signal on any side pins it flat (and resets a
// tilted one), and a projectile knocks it straight to FULL.

var bigDripleafMin, bigDripleafMax = worldgen.BlockRange("big_dripleaf")

func isBigDripleaf(s uint32) bool { return s >= bigDripleafMin && s <= bigDripleafMax }

// dripleafDelay is DELAY_UNTIL_NEXT_TILT_STATE: ticks each tilt lasts.
var dripleafDelay = map[string]uint64{"unstable": 10, "partial": 10, "full": 100}

func dripleafTilt(s uint32) string {
	info, ok := worldgen.InfoForState(s)
	if !ok {
		return "none"
	}
	return worldgen.GetProperty(info, s, "tilt")
}

func withDripleafTilt(s uint32, tilt string) uint32 {
	info, ok := worldgen.InfoForState(s)
	if !ok {
		return s
	}
	return worldgen.SetProperty(info, s, "tilt", tilt)
}

// dripleafPowered is hasNeighborSignal; the redstone graph is overworld-only,
// which is the only place a dripleaf grows.
func (h *hub) dripleafPowered(dim int, pos blockPos) bool {
	return dim == 0 && h.hasNeighborSignal(pos.x, pos.y, pos.z)
}

// dripleafStepped is the entityInside half: a grounded entity on a flat,
// unpowered leaf starts it tipping.
func (h *hub) dripleafStepped(players map[int32]*tracked, dim int, pos blockPos, s uint32) {
	if dripleafTilt(s) != "none" || h.dripleafPowered(dim, pos) {
		return
	}
	h.setDripleafTilt(players, dim, pos, s, "unstable", "")
}

// setDripleafTilt is setTiltAndScheduleTick: write the tilt, play the sound
// if one goes with it, and queue the next stage.
func (h *hub) setDripleafTilt(players map[int32]*tracked, dim int, pos blockPos, s uint32, tilt, sound string) {
	if ns := withDripleafTilt(s, tilt); ns != s {
		h.setBlockAt(players, dim, pos, ns)
	}
	if sound != "" {
		h.playSoundDim(players, dim, sound, sndBlock, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 0.8+h.rng.Float32()*0.4)
	}
	if delay := dripleafDelay[tilt]; delay > 0 {
		if h.dripleafDue == nil {
			h.dripleafDue = map[simPos]uint64{}
		}
		h.dripleafDue[simPos{dim: dim, blockPos: pos}] = h.tick.Load() + delay
		h.scheduleIn(dim, pos, delay)
	}
}

// resetDripleafTilt flattens the leaf, with the spring-back sound if it was
// tilted at all.
func (h *hub) resetDripleafTilt(players map[int32]*tracked, dim int, pos blockPos, s uint32) {
	delete(h.dripleafDue, simPos{dim: dim, blockPos: pos})
	sound := ""
	if dripleafTilt(s) != "none" {
		sound = "minecraft:block.big_dripleaf.tilt_up"
	}
	h.setDripleafTilt(players, dim, pos, s, "none", sound)
}

// tickDripleaf handles a scheduled update on a leaf: its own tilt clock, or
// a neighbour change (a signal arriving pins it flat). Returns whether the
// block was a dripleaf.
func (h *hub) tickDripleaf(players map[int32]*tracked, dim int, pos blockPos, s uint32) bool {
	if !isBigDripleaf(s) {
		return false
	}
	if h.dripleafPowered(dim, pos) {
		h.resetDripleafTilt(players, dim, pos, s)
		return true
	}
	key := simPos{dim: dim, blockPos: pos}
	if due, ok := h.dripleafDue[key]; !ok || h.tick.Load() < due {
		return true // a neighbour's update, not this leaf's clock
	}
	delete(h.dripleafDue, key)
	switch dripleafTilt(s) {
	case "unstable":
		h.setDripleafTilt(players, dim, pos, s, "partial", "minecraft:block.big_dripleaf.tilt_down")
	case "partial":
		h.setDripleafTilt(players, dim, pos, s, "full", "minecraft:block.big_dripleaf.tilt_down")
	case "full":
		h.resetDripleafTilt(players, dim, pos, s)
	}
	return true
}

// dripleafShot is onProjectileHit: any projectile tips the leaf all the way.
func (h *hub) dripleafShot(players map[int32]*tracked, dim int, pos blockPos, s uint32) {
	h.setDripleafTilt(players, dim, pos, s, "full", "minecraft:block.big_dripleaf.tilt_down")
}
