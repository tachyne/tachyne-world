package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Container open gates: some containers refuse to open when something is in
// the way. A chest needs headroom — ChestBlock.isChestBlockedAt is a redstone
// conductor in the cell above, or a cat sitting there — and a shulker box needs
// the half block its lid slides into. Ender chests and barrels have no such
// rule; a barrel opens with a block on top, which is the whole reason to use
// one.

// chestBlockedAt is ChestBlock.isChestBlockedAt.
func (h *hub) chestBlockedAt(dim int, pos blockPos) bool {
	if conducts(h.worldFor(dim).At(pos.x, pos.y+1, pos.z)) {
		return true
	}
	return h.catSittingOn(dim, pos)
}

// catSittingOn is ChestBlock.isCatSittingOnChest: any cat in the cell above
// that is in its sitting pose. Vanilla's box is the full cell, so a cat that
// has only partly stepped onto the lid still counts.
func (h *hub) catSittingOn(dim int, pos blockPos) bool {
	for _, m := range h.mobs {
		if m.etype != entityCat || m.dim != dim || !m.sitting {
			continue
		}
		if m.x < float64(pos.x) || m.x > float64(pos.x+1) ||
			m.z < float64(pos.z) || m.z > float64(pos.z+1) ||
			m.y < float64(pos.y+1) || m.y > float64(pos.y+2) {
			continue
		}
		return true
	}
	return false
}

// shulkerBlockedAt is ShulkerBoxBlock.canOpen: the lid slides half a block out
// of the box's facing side, and anything solid there holds it shut. Vanilla
// intersects that half-block volume with the world's collision shapes; we test
// the neighbouring cell for a full cube, which agrees except for the odd
// partial block sitting in the far half of that cell.
func (h *hub) shulkerBlockedAt(dim int, pos blockPos, state uint32) bool {
	// canOpen asks only while the lid is CLOSED: a box someone already has
	// open (or that is still moving) opens for the next player too.
	if lid := h.shulkerLids[simPos{dim: dim, blockPos: pos}]; lid != nil && lid.status != lidClosed {
		return false
	}
	d, ok := propDir(state, "facing")
	if !ok {
		return false
	}
	dx, dy, dz := d.delta()
	return fullCube(h.worldFor(dim).At(pos.x+dx, pos.y+dy, pos.z+dz))
}

// containerOpenBlocked answers for whichever container is at pos.
func (h *hub) containerOpenBlocked(dim int, pos blockPos, state uint32) bool {
	switch {
	case isShulkerBox(state):
		return h.shulkerBlockedAt(dim, pos, state)
	case isBarrel(state), state == worldgen.Air:
		return false
	case state >= chestStateMin && state <= chestStateMax, isTrappedChest(state):
		return h.chestBlockedAt(dim, pos) // TrappedChestBlock inherits the rule
	}
	return false
}
