package server

import (
	"math"

	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
	attr "github.com/tachyne/tachyne-world/plugin/attribute"
)

// A container menu stays open only while it is valid (AbstractContainerMenu.
// stillValid, checked every ServerPlayer.tick): the block must still be
// there and the player within block-interaction range plus four of it.
// When that fails the server closes the menu (ServerPlayer.closeContainer
// → ClientboundContainerClosePacket) and takes back what the cursor and
// the crafting slots held. tachyne's menus stayed open over a broken
// chest or from across the map until the player closed them.

const menuReachSlack = 4.0 // stillValid: isWithinBlockInteractionRange(pos, 4.0)

// closeWindowServer closes the viewer's container from the server: the
// engine-side close (items reclaimed, viewers released) and the frame that
// closes the client's menu.
func (h *hub) closeWindowServer(players map[int32]*tracked, t *tracked) {
	if t.winKind == winPlayer || t.winID == 0 {
		return
	}
	id := t.winID
	h.closeWindow(players, t)
	t.p.trySendEv(attachproto.WindowCloseServer{ID: int32(id)})
}

// validateWindows closes every block-backed menu that is no longer valid,
// and a trade screen whose villager died or is out of reach
// (AbstractVillager.stillValid: alive, and within entity interaction
// range plus four).
func (h *hub) validateWindows(players map[int32]*tracked) {
	for _, t := range players {
		switch {
		case t.winKind == winPlayer || t.winKind == winHorse:
			continue
		case t.winKind == winTrade:
			m := h.mobs[t.tradeWith]
			if m == nil || m.dying > 0 || m.dim != t.dim || !h.withinEntityReach(t, m) {
				h.closeWindowServer(players, t)
			}
		case t.winPos != (simPos{}):
			if t.winPos.dim != t.dim || !h.menuBlockPresent(t) || !h.withinMenuReach(t) {
				h.closeWindowServer(players, t)
			}
		}
	}
}

// withinEntityReach is Player.isWithinEntityInteractionRange(entity, 4.0):
// the eye position to the mob's box, against entity_interaction_range + 4.
func (h *hub) withinEntityReach(t *tracked, m *mob) bool {
	reach := t.playerAttrs().Value(attr.EntityInteractionRange) + menuReachSlack
	b := m.box()
	ex, ey, ez := t.x, t.y+playerEyeHeightStand, t.z
	dx := math.Max(0, math.Max(m.x-b.w/2-ex, ex-(m.x+b.w/2)))
	dy := math.Max(0, math.Max(m.y-ey, ey-(m.y+b.h)))
	dz := math.Max(0, math.Max(m.z-b.w/2-ez, ez-(m.z+b.w/2)))
	return dx*dx+dy*dy+dz*dz <= reach*reach
}

// menuBlockPresent reports whether the block the menu views is still a
// block at all (a broken container is air).
func (h *hub) menuBlockPresent(t *tracked) bool {
	w := h.worldFor(t.winPos.dim)
	if w == nil {
		return false
	}
	return w.At(t.winPos.x, t.winPos.y, t.winPos.z) != worldgen.Air
}

// withinMenuReach is Player.isWithinBlockInteractionRange(pos, 4.0): the
// eye position to the block's box, against block_interaction_range + 4.
func (h *hub) withinMenuReach(t *tracked) bool {
	reach := t.playerAttrs().Value(attr.BlockInteractionRange) + menuReachSlack
	ex, ey, ez := t.x, t.y+playerEyeHeightStand, t.z
	bx, by, bz := float64(t.winPos.x), float64(t.winPos.y), float64(t.winPos.z)
	dx := math.Max(0, math.Max(bx-ex, ex-(bx+1)))
	dy := math.Max(0, math.Max(by-ey, ey-(by+1)))
	dz := math.Max(0, math.Max(bz-ez, ez-(bz+1)))
	return dx*dx+dy*dy+dz*dz <= reach*reach
}
