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

// dispenseCarvedPumpkin places the pumpkin facing back toward the
// dispenser and tries the snow, iron and copper golem patterns.
func (h *hub) dispenseCarvedPumpkin(players map[int32]*tracked, dim int, binState uint32, front blockPos) {
	facing := "north" // DirectionalPlaceContext.getHorizontalDirection for an up/down dispenser
	if f := stateFacing(binState); f != "up" && f != "down" {
		facing = f
	}
	info, _ := worldgen.InfoForState(carvedPumpkinBase)
	st := worldgen.SetProperty(info, carvedPumpkinBase, "facing", oppositeFacing(facing))
	h.setBlockAt(players, dim, front, st)
	if !h.checkGolemBuild(players, dim, front.x, front.y, front.z, st) {
		h.checkCopperGolemBuild(players, dim, front.x, front.y, front.z, st)
	}
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
	if heldStack(t).item != itemBrush || !h.brushArmadillo(players, m) {
		return false
	}
	if t.gamemode == gmSurvival {
		h.applyToolWear(t, t.p.heldSlot(), 16)
	}
	return true
}
