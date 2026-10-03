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

// supportsBigDripleaf is #supports_big_dripleaf with #supports_small_dripleaf
// flattened in: the ground a big dripleaf roots in.
var supportsBigDripleaf = func() map[uint32]bool {
	out := map[uint32]bool{}
	for _, n := range []string{"clay", "moss_block", "dirt", "grass_block", "podzol", "coarse_dirt", "mycelium",
		"rooted_dirt", "mud", "muddy_mangrove_roots", "farmland"} {
		lo, hi := worldgen.BlockRange(n)
		for s := lo; s <= hi; s++ {
			out[s] = true
		}
	}
	return out
}()

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

// dripleafPowered is hasNeighborSignal, read in the leaf's own dimension (a
// dripleaf planted in the Nether or the End answers to redstone there).
func (h *hub) dripleafPowered(dim int, pos blockPos) bool {
	powered := false
	h.inDim(dim, func() { powered = h.hasNeighborSignal(pos.x, pos.y, pos.z) })
	return powered
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
// if one goes with it, and ask for the next stage's tick — which a tick
// already pending keeps (Level.scheduleTick), so a projectile knocking an
// unstable leaf flat leaves it on the unstable stage's clock.
func (h *hub) setDripleafTilt(players map[int32]*tracked, dim int, pos blockPos, s uint32, tilt, sound string) {
	h.writeDripleafTilt(players, dim, pos, s, tilt)
	if sound != "" {
		h.playDripleafTilt(players, dim, pos, sound)
	}
	if delay := dripleafDelay[tilt]; delay > 0 {
		h.scheduleBlockTickIn(dim, pos, delay)
	}
}

// writeDripleafTilt is setTilt: the new tilt, and a BLOCK_CHANGE when it
// changed to a tilt that causesVibration (every one but UNSTABLE).
func (h *hub) writeDripleafTilt(players map[int32]*tracked, dim int, pos blockPos, s uint32, tilt string) {
	if ns := withDripleafTilt(s, tilt); ns != s {
		h.setBlockAt(players, dim, pos, ns)
		if tilt != "unstable" {
			h.vib(dim, freqBlockChange, pos.x, pos.y, pos.z, 0)
		}
	}
}

func (h *hub) playDripleafTilt(players map[int32]*tracked, dim int, pos blockPos, sound string) {
	h.playSoundDim(players, dim, sound, sndBlock, float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 0.8+h.rng.Float32()*0.4)
}

// resetDripleafTilt is resetTilt: the leaf flattens, with the spring-back
// sound if it was tilted at all. It schedules nothing, and a tick still
// pending runs on a flat leaf and does nothing.
func (h *hub) resetDripleafTilt(players map[int32]*tracked, dim int, pos blockPos, s uint32) {
	h.writeDripleafTilt(players, dim, pos, s, "none")
	if dripleafTilt(s) != "none" {
		h.playDripleafTilt(players, dim, pos, "minecraft:block.big_dripleaf.tilt_up")
	}
}

// dripleafNeighborChanged is BigDripleafBlock.neighborChanged: a signal
// arriving pins the leaf flat. A tilted leaf with no tick pending (one from
// before a restart: vanilla saves the tick with the chunk) gets its stage's
// tick again. Returns whether the block was a dripleaf.
func (h *hub) dripleafNeighborChanged(players map[int32]*tracked, dim int, pos blockPos, s uint32) bool {
	if !isBigDripleaf(s) {
		return false
	}
	if h.dripleafPowered(dim, pos) {
		h.resetDripleafTilt(players, dim, pos, s)
		return true
	}
	h.dripleafRearm(dim, pos, s)
	return true
}

// dripleafRearm books a tilted leaf's stage tick if it has none.
func (h *hub) dripleafRearm(dim int, pos blockPos, s uint32) {
	if delay := dripleafDelay[dripleafTilt(s)]; delay > 0 && !h.hasBlockTickIn(dim, pos, s) {
		h.scheduleBlockTickIn(dim, pos, delay)
	}
}

// tickDripleaf is BigDripleafBlock.tick, the leaf's scheduled tick: a
// signal flattens it; otherwise it moves on a stage.
func (h *hub) tickDripleaf(players map[int32]*tracked, dim int, pos blockPos, s uint32) {
	if h.dripleafPowered(dim, pos) {
		h.resetDripleafTilt(players, dim, pos, s)
		return
	}
	switch dripleafTilt(s) {
	case "unstable":
		h.setDripleafTilt(players, dim, pos, s, "partial", "minecraft:block.big_dripleaf.tilt_down")
	case "partial":
		h.setDripleafTilt(players, dim, pos, s, "full", "minecraft:block.big_dripleaf.tilt_down")
	case "full":
		h.resetDripleafTilt(players, dim, pos, s)
	}
}

// dripleafShot is onProjectileHit: any projectile tips the leaf all the way.
func (h *hub) dripleafShot(players map[int32]*tracked, dim int, pos blockPos, s uint32) {
	h.setDripleafTilt(players, dim, pos, s, "full", "minecraft:block.big_dripleaf.tilt_down")
}
