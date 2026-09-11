package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The chorus fruit's teleport (TeleportRandomlyConsumeEffect, diameter 16):
// sixteen tries at a point within ±8 blocks of the eater on every axis; the
// point drops to the first block that blocks motion under it
// (LivingEntity.randomTeleport), and lands there if the eater fits and
// stands in no liquid. A hit plays the chorus teleport sound, wipes the
// fall and puts the fruit on a one-second cooldown.

const chorusTeleportDiameter = 16.0

func (h *hub) chorusTeleport(players map[int32]*tracked, t *tracked) bool {
	for i := 0; i < 16; i++ {
		x := t.x + (h.rng.Float64()-0.5)*chorusTeleportDiameter
		y := t.y + (h.rng.Float64()-0.5)*chorusTeleportDiameter
		z := t.z + (h.rng.Float64()-0.5)*chorusTeleportDiameter
		if ny, ok := h.randomTeleportY(t.dim, x, y, z); ok {
			h.teleportPlayer(players, t, x, ny, z)
			t.peakY = t.y // resetFallDistance
			h.playSoundDim(players, t.dim, "minecraft:item.chorus_fruit.teleport", sndPlayer, t.x, t.y, t.z, 1, 1)
			h.setCooldown(t, itemChorusFruit, 20)
			return true
		}
	}
	return false
}

// randomTeleportY is LivingEntity.randomTeleport's placement: from the
// point, down to the first cell whose block below blocks motion; the two
// cells of the standing space must be free of collision and liquid.
func (h *hub) randomTeleportY(dim int, x, y, z float64) (float64, bool) {
	w := h.worldFor(dim)
	bx, bz := int(math.Floor(x)), int(math.Floor(z))
	by := int(math.Floor(y))
	if !h.inWorldYIn(dim, by) {
		return 0, false
	}
	for by > worldgen.MinY && !worldgen.Collides(w.At(bx, by-1, bz)) {
		by--
	}
	if by <= worldgen.MinY {
		return 0, false
	}
	for _, dy := range []int{0, 1} {
		s := w.At(bx, by+dy, bz)
		if worldgen.Collides(s) || worldgen.IsFluid(s) {
			return 0, false
		}
	}
	return float64(by), true
}
