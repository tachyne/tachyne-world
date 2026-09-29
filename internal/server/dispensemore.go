package server

import (
	"github.com/tachyne/tachyne-common/protocol"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The rest of vanilla's dispenser table (DispenseItemBehavior.bootStrap):
// chests onto pack animals, carved pumpkins that build golems, shulker
// boxes placed with their contents, glowstone charging a respawn anchor,
// and a brush combing an armadillo. The projectile and bucket entries sit
// in the dispense switch itself (bin.go).

// dispenseChest straps a chest onto a tamed, unchested donkey, mule or
// llama standing in the cell ahead. Reports whether one took it.
func (h *hub) dispenseChest(players map[int32]*tracked, dim int, front blockPos) bool {
	var target *mob
	h.grid().nearby(dim, float64(front.x)+0.5, float64(front.z)+0.5, 1.5, func(m *mob) {
		if target != nil || m.dying > 0 || m.chested || m.baby || !m.tamed || !chestedFamily(m.etype) || !mobInBlock(m, front) {
			return
		}
		target = m
	})
	if target == nil {
		return false
	}
	h.equipChest(players, target)
	return true
}

// dispenseCarvedPumpkin is the CARVED_PUMPKIN behaviour's placement: the
// pumpkin in its default state (facing north), which completes the golem
// canSpawnGolem found below it.
func (h *hub) dispenseCarvedPumpkin(players map[int32]*tracked, dim int, front blockPos) {
	st := carvedPumpkinBase
	if info, ok := worldgen.InfoForState(carvedPumpkinBase); ok {
		st = worldgen.SetProperty(info, carvedPumpkinBase, "facing", "north")
	}
	h.setBlockAt(players, dim, front, st)
	if !h.checkGolemBuild(players, dim, front.x, front.y, front.z, st) {
		h.checkCopperGolemBuild(players, dim, front.x, front.y, front.z, st)
	}
}

// canSpawnGolem is CarvedPumpkinBlock.canSpawnGolem for a pumpkin at p: a
// snow golem's two snow blocks, an iron golem's T (with its air), or a
// copper block waiting under the cell — the golems' bodies without the
// head, upright as the engine builds them.
func (h *hub) canSpawnGolem(dim int, p blockPos) bool {
	w := h.worldFor(dim)
	at := func(dx, dy, dz int) uint32 { return w.At(p.x+dx, p.y+dy, p.z+dz) }
	if at(0, -1, 0) == snowBlockState && at(0, -2, 0) == snowBlockState {
		return true
	}
	if at(0, -1, 0) == ironBlockState && at(0, -2, 0) == ironBlockState {
		for _, d := range [][2]int{{1, 0}, {0, 1}} {
			ax, az := d[0], d[1]
			if at(ax, -1, az) == ironBlockState && at(-ax, -1, -az) == ironBlockState &&
				at(ax, 0, az) == worldgen.Air && at(-ax, 0, -az) == worldgen.Air &&
				at(ax, -2, az) == worldgen.Air && at(-ax, -2, -az) == worldgen.Air {
				return true
			}
		}
	}
	return copperBlockStates[at(0, -1, 0)]
}

// canSpawnWither is WitherSkullBlock.canSpawnMob for a skull at p: not in
// peaceful, two above the world's floor, and the soul-sand T of the wither
// base under the top row p sits in (the other two skulls need not be there
// yet).
func (h *hub) canSpawnWither(dim int, p blockPos) bool {
	if h.rules.Difficulty == diffPeaceful || p.y < worldgen.MinY+2 {
		return false
	}
	w := h.worldFor(dim)
	air := func(s uint32) bool { return s == worldgen.Air || s == caveAirState || s == voidAirState }
	for _, ax := range [][2]int{{1, 0}, {0, 1}} {
	centre:
		for k := -1; k <= 1; k++ {
			cx, cz := p.x-k*ax[0], p.z-k*ax[1]
			for j := -1; j <= 1; j++ {
				x, z := cx+j*ax[0], cz+j*ax[1]
				if !soulFireBase(w.At(x, p.y-1, z)) || (j != 0 && !air(w.At(x, p.y-2, z))) {
					continue centre
				}
			}
			if soulFireBase(w.At(cx, p.y-2, cz)) {
				return true
			}
		}
	}
	return false
}

// dispenseShulkerBox places the box, contents and all, opening along the
// dispense direction or upward when the cell below is empty.
func (h *hub) dispenseShulkerBox(players map[int32]*tracked, dim int, binState uint32, front blockPos, st *invStack) {
	base, ok := protocol.BlockForItem(st.item)
	if !ok {
		return
	}
	info, ok := worldgen.InfoForState(base)
	if !ok {
		return
	}
	facing := stateFacing(binState)
	if h.worldFor(dim).At(front.x, front.y-1, front.z) != worldgen.Air {
		facing = "up"
	}
	placed := worldgen.SetProperty(info, base, "facing", facing)
	h.setBlockAt(players, dim, front, placed)
	if st.boxID != 0 {
		h.restoreShulkerBox(simPos{dim: dim, blockPos: front}, st.boxID)
	}
}

// dispenseGlowstone charges a respawn anchor in front. Reports whether it
// did (a full anchor, or no anchor, lets the block drop as an item).
func (h *hub) dispenseGlowstone(players map[int32]*tracked, dim int, front blockPos) bool {
	state := h.worldFor(dim).At(front.x, front.y, front.z)
	charge := anchorCharge(state)
	if charge < 0 || charge >= anchorMaxCharge {
		return false
	}
	h.setBlockAt(players, dim, front, anchorWithCharge(state, charge+1))
	h.playSoundDim(players, dim, "minecraft:block.respawn_anchor.charge", sndBlock,
		float64(front.x)+0.5, float64(front.y)+0.5, float64(front.z)+0.5, 1, 1)
	return true
}

// dispenseBrush combs the armadillo in the cell ahead.
func (h *hub) dispenseBrush(players map[int32]*tracked, dim int, front blockPos) bool {
	done := false
	h.grid().nearby(dim, float64(front.x)+0.5, float64(front.z)+0.5, 1.5, func(m *mob) {
		if done || m.etype != entityArmadillo || !mobInBlock(m, front) {
			return
		}
		done = h.brushArmadillo(players, m)
	})
	return done
}

// brushArmadillo is Armadillo.brushOffScute: a grown armadillo gives up a
// scute; a baby gives nothing.
func (h *hub) brushArmadillo(players map[int32]*tracked, m *mob) bool {
	if m.etype != entityArmadillo || m.baby || m.dying > 0 {
		return false
	}
	h.spawnItemIn(players, m.dim, int32(itemByName["armadillo_scute"]), 1, m.x, m.y+0.5, m.z)
	h.playSoundDim(players, m.dim, "minecraft:entity.armadillo.brush", sndNeutral, m.x, m.y, m.z, 1, 1)
	return true
}

// tryBrush is BrushItem.interactLivingEntity: a brush on an armadillo takes
// a scute and wears the brush by sixteen.
func (h *hub) tryBrush(players map[int32]*tracked, t *tracked, m *mob) bool {
	if usedStack(t).item != itemBrush || !h.brushArmadillo(players, m) {
		return false
	}
	if isSurvival(t.gamemode) {
		h.applyToolWear(t, t.useSlot(), 16)
	}
	return true
}
