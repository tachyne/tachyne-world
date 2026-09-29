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

// shulkerBlockedAt is ShulkerBoxBlock.canOpen: the lid slides half a block
// out of the box's facing side (Shulker.getProgressDeltaAabb(1, facing, 0,
// 0.5), deflated by 1e-6), and Level.noCollision on that volume must hold:
// no block's collision shape in it — a bottom slab above an upward box
// shuts it, a top slab does not — and no entity that others collide with
// (a boat, a shulker).
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
	lo := [3]float64{float64(pos.x), float64(pos.y), float64(pos.z)}
	hi := [3]float64{lo[0] + 1, lo[1] + 1, lo[2] + 1}
	for i, v := range [3]int{dx, dy, dz} {
		switch {
		case v > 0:
			lo[i], hi[i] = hi[i], hi[i]+0.5
		case v < 0:
			lo[i], hi[i] = lo[i]-0.5, lo[i]
		}
	}
	for i := range lo {
		lo[i], hi[i] = lo[i]+1e-6, hi[i]-1e-6
	}
	w := h.worldFor(dim)
	n := blockPos{pos.x + dx, pos.y + dy, pos.z + dz}
	// The cell the lid moves into, and the one under it: a fence or wall
	// there stands half a block into the cell above.
	for _, c := range []blockPos{n, {n.x, n.y - 1, n.z}} {
		if shapeMeetsBox(w.At(c.x, c.y, c.z), c.x, c.y, c.z, lo, hi) {
			return true
		}
	}
	for _, v := range h.vehicles {
		if v.dim == dim && v.isBoat() && psBoxHits(lo, hi, v.x, v.y, v.z, boatHalfWidth*2, boatHeight) {
			return true
		}
	}
	for _, m := range h.mobs {
		if m.dim == dim && m.etype == entityShulker && m.dying == 0 {
			if b := m.box(); psBoxHits(lo, hi, m.x, m.y, m.z, b.w, b.h) {
				return true
			}
		}
	}
	return false
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
