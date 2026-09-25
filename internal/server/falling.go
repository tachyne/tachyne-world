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

// A stalactite that loses its grip comes down whole, and the tip of it is the
// dangerous end. Vanilla spawns the column as falling blocks and gives the tip
// hurtsEntities(n, 40), where n is the column's own length with a floor of six
// — so even a short spike falls hard — and the damage is that per block of the
// drop, capped at forty.
const (
	stalactiteHurtFloor = 6  // the shortest column still hits like six
	stalactiteHurtMax   = 40 // FallingBlockEntity's cap
)

// stalactiteFallDamage is what a tip deals after falling `fallen` blocks from
// a column `length` long.
// FallingBlockEntity.causeFallDamage counts ceil(fallDistance - 1): the
// cells fallen less the one the block starts from.
func stalactiteFallDamage(length, fallen int) float64 {
	distance := fallen - 1
	if distance <= 0 {
		return 0
	}
	per := float64(max(length, stalactiteHurtFloor))
	return math.Min(math.Floor(float64(distance)*per), stalactiteHurtMax)
}

// dropStalactite is PointedDripstoneBlock.spawnFallingStalactite: every cell
// of the hanging column is let go at once, and the tip carries the column's
// length with it so it knows how hard it lands.
func (h *hub) dropStalactite(players map[int32]*tracked, dim int, pos blockPos) {
	length := 0
	for y := pos.y; h.inWorldY(y); y-- {
		st := h.worldFor(dim).Block(pos.x, y, pos.z)
		thick, up, _, ok := dripstoneParts(st)
		if !ok || up {
			break // not a stalactite any more
		}
		length++
		h.scheduleIn(dim, blockPos{pos.x, y, pos.z}, 1)
		if thick == dripTip || thick == dripTipMerge { // the tip: the column ends here
			break
		}
	}
	if length > 0 {
		h.stalactiteLen[simPos{dim: dim, blockPos: pos}] = length
	}
}

// stalactiteLanded hurts whatever the tip came down on.
func (h *hub) stalactiteLanded(players map[int32]*tracked, dim int, pos blockPos, length, fallen int) {
	dmg := stalactiteFallDamage(length, fallen)
	if dmg <= 0 {
		return
	}
	inCell := func(x, y, z float64) bool {
		return int(math.Floor(x)) == pos.x && int(math.Floor(z)) == pos.z &&
			y > float64(pos.y)-1 && y < float64(pos.y)+1
	}
	for _, t := range players {
		if t.dim != dim || t.dead || t.gamemode == gmCreative || t.gamemode == gmSpectator {
			continue
		}
		if inCell(t.x, t.y, t.z) {
			h.damageOf(players, t, float32(dmg), dtFallingStalactite)
		}
	}
	for _, m := range h.mobs {
		if m.dim != dim || m.dying > 0 {
			continue
		}
		if inCell(m.x, m.y, m.z) {
			h.hurtMobOf(players, m, dmg, dtFallingStalactite)
		}
	}
}
