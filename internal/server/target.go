package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Target block: a projectile strike energises it with a redstone signal whose
// strength rises the closer the hit lands to the block's centre, held for 20
// ticks (arrows) or 8 (everything else) before decaying to 0. Reimplemented
// from the vanilla TargetBlock (OUTPUT_POWER state 0..15).

var (
	targetMin = worldgen.BlockBase("target") // power 0..15
	targetMax = worldgen.BlockBase("target") + 15
)

func isTarget(s uint32) bool   { return s >= targetMin && s <= targetMax }
func targetPower(s uint32) int { return int(s - targetMin) }
func targetWithPower(s uint32, p int) uint32 {
	if p < 0 {
		p = 0
	} else if p > 15 {
		p = 15
	}
	return targetMin + uint32(p)
}

// targetStrength maps a hit point to a redstone level: 1 at the rim, up to 15
// dead centre (TargetBlock.getRedstoneStrength).
func targetStrength(hx, hy, hz float64) int {
	dx := math.Abs(hx - math.Floor(hx) - 0.5)
	dy := math.Abs(hy - math.Floor(hy) - 0.5)
	dz := math.Abs(hz - math.Floor(hz) - 0.5)
	d := math.Sqrt(dx*dx + dy*dy + dz*dz)
	s := int(math.Ceil(15.0 * clampF((0.5-d)/0.5, 0, 1)))
	if s < 1 {
		s = 1
	}
	return s
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// hitTarget is TargetBlock.onProjectileHit: a target with no tick pending
// takes the hit's signal and schedules the tick that resets it (8 ticks, an
// arrow's 20); one still holding a signal keeps it, and its reset stays
// where it was. The hit is credited either way. Runs in the target's own
// dimension.
func (h *hub) hitTarget(players map[int32]*tracked, dim int, pos blockPos, state uint32, hx, hy, hz float64, arrow bool, a *arrowEntity) {
	strength := targetStrength(hx, hy, hz)
	ticks := uint64(8)
	if arrow {
		ticks = 20
	}
	if !h.hasBlockTickIn(dim, pos, state) {
		// setOutputPower: setBlockAndUpdate, then the reset tick.
		h.setBlockAt(players, dim, pos, targetWithPower(state, strength))
		h.scheduleBlockTickIn(dim, pos, ticks)
		h.inDim(dim, func() { h.scheduleSignalAround(players, pos) }) // neighbours read the new signal at once
	}
	if a != nil && a.playerShot {
		if s := players[a.shooter]; s != nil {
			h.incCustom(s, "target_hit", 1)
			h.advance(players, s, "target_hit", advMatch{signal: strength, distH: math.Hypot(a.x-a.ox, a.z-a.oz)})
		}
	}
}

// tickTarget is TargetBlock.tick, the reset: a target still giving a
// signal drops to 0. A neighbour's change does nothing to a target.
func (h *hub) tickTarget(players map[int32]*tracked, pos blockPos, state uint32) {
	if targetPower(state) > 0 {
		h.rsSet(players, pos, targetWithPower(state, 0))
		h.scheduleSignalAround(players, pos)
	}
}

// targetRearm books the reset of a target found giving a signal with no
// reset pending — one from before a restart: vanilla saves the tick with
// the chunk, this engine keeps ticks in memory.
func (h *hub) targetRearm(dim int, pos blockPos, state uint32) {
	if isTarget(state) && targetPower(state) > 0 && !h.hasBlockTickIn(dim, pos, state) {
		h.scheduleBlockTickIn(dim, pos, 8)
	}
}
