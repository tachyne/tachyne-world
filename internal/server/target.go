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

// hitTarget energises a target block struck at (hx,hy,hz). arrow selects the
// longer 20-tick hold. Runs in the target's own dimension: the write, the
// reset record and both schedules used to be the overworld's whatever the
// projectile had actually hit.
func (h *hub) hitTarget(players map[int32]*tracked, dim int, pos blockPos, state uint32, hx, hy, hz float64, arrow bool, a *arrowEntity) {
	h.setBlockAt(players, dim, pos, targetWithPower(state, targetStrength(hx, hy, hz)))
	ticks := uint64(8)
	if arrow {
		ticks = 20
	}
	h.targetDue[simPos{dim: dim, blockPos: pos}] = h.tick.Load() + ticks
	if a != nil && a.playerShot {
		if s := players[a.shooter]; s != nil {
			h.incCustom(s, "target_hit", 1)
			h.advance(players, s, "target_hit", advMatch{signal: targetStrength(hx, hy, hz), distH: math.Hypot(a.x-a.ox, a.z-a.oz)})
		}
	}
	h.scheduleIn(dim, pos, ticks)
	h.scheduleAroundIn(dim, pos, 1) // let neighbours read the new signal
}

// updateTarget decays a fired target back to 0 once its hold has elapsed. A
// neighbour update before then leaves it holding.
func (h *hub) updateTarget(players map[int32]*tracked, pos blockPos, state uint32) {
	key := simPos{dim: h.rsDim, blockPos: pos}
	due, ok := h.targetDue[key]
	if !ok || h.tick.Load() < due {
		return
	}
	delete(h.targetDue, key)
	if targetPower(state) > 0 {
		h.rsSet(players, pos, targetWithPower(state, 0))
		h.scheduleAroundIn(h.rsDim, pos, 1)
	}
}
