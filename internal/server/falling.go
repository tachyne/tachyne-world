package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Falling anvils. A falling anvil hurts whatever it lands on (AnvilBlock.
// falling: 2 per block fallen after the first, at most 40, the falling_anvil
// source, creative and spectator players spared) and wears with the fall:
// a chance of 5% plus 5% per block of the counted fall turns an anvil
// chipped, a chipped one damaged, and a damaged one breaks outright with no
// drop (FallingBlockEntity.causeFallDamage → AnvilBlock.damage).

const (
	anvilHurtPerBlock = 2.0
	anvilHurtMax      = 40.0
)

// anvilFallDamage is the damage dealt after `fallen` cells: floor(n × 2)
// capped at 40, with n = the fall less one.
func anvilFallDamage(fallen int) float64 {
	n := fallen - 1
	if n < 0 {
		return 0
	}
	return math.Min(math.Floor(float64(n)*anvilHurtPerBlock), anvilHurtMax)
}

// anvilAfterFall is the wear roll: the state the anvil lands as (0 = it
// broke), given the fall and the roll in [0,1).
func anvilAfterFall(state uint32, fallen int, roll float64) uint32 {
	n := fallen - 1
	if anvilFallDamage(fallen) <= 0 || roll >= 0.05+float64(n)*0.05 {
		return state
	}
	return worldgen.AnvilDamaged(state)
}

// anvilLanded applies the landing to the entities in the cell and to the
// anvil itself.
func (h *hub) anvilLanded(players map[int32]*tracked, dim int, pos blockPos, state uint32, fallen int) {
	if dmg := anvilFallDamage(fallen); dmg > 0 {
		inCell := func(x, y, z float64) bool {
			return int(math.Floor(x)) == pos.x && int(math.Floor(z)) == pos.z &&
				y > float64(pos.y)-1 && y < float64(pos.y)+1
		}
		for _, t := range players {
			if t.dim != dim || t.dead || t.gamemode == gmCreative || t.gamemode == gmSpectator {
				continue
			}
			if inCell(t.x, t.y, t.z) {
				h.damageOf(players, t, float32(dmg), dtFallingAnvil)
			}
		}
		for _, m := range h.mobs {
			if m.dim != dim || m.dying > 0 {
				continue
			}
			if inCell(m.x, m.y, m.z) {
				h.hurtMobOf(players, m, dmg, dtFallingAnvil)
			}
		}
	}
	h.playSoundDim(players, dim, "minecraft:block.anvil.land", sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 0.3, 1)
	if worn := anvilAfterFall(state, fallen, h.rng.Float64()); worn != state {
		h.setBlockAt(players, dim, pos, worn) // 0 = the anvil broke, leaving nothing
	}
}
