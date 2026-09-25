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
		case t.winKind == winPlayer:
			continue
		case t.winKind == winHorse: // AbstractMountInventoryMenu.stillValid: alive, within entity reach + 4
			m := h.mobs[t.horseEID]
			if m == nil || m.dying > 0 || m.dim != t.dim || !h.withinEntityReach(t, m) {
				h.closeWindowServer(players, t)
			}
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
	b := m.box()
	return withinEntityRange(t, m.x, m.y, m.z, b.w, b.h, menuReachSlack)
}

// interactSlack is the buffer the server allows an attack or an entity
// interaction beyond the player's reach (handleAttack's isWithinAttackRange
// and handleInteract's isWithinEntityInteractionRange, both 3.0).
const interactSlack = 3.0

// withinEntityRange is Player.isWithinEntityInteractionRange(box, buffer):
// the eye position to an entity's box (centre x/z, feet y, width, height),
// against entity_interaction_range + buffer. For a melee blow with no
// attack_range component it is also isWithinAttackRange: AttackRange's
// default is that same attribute, with no hitbox margin.
func withinEntityRange(t *tracked, x, y, z, w, ht, buffer float64) bool {
	reach := t.playerAttrs().Value(attr.EntityInteractionRange) + buffer
	ex, ey, ez := t.x, t.y+t.eyeHeight(), t.z
	dx := math.Max(0, math.Max(x-w/2-ex, ex-(x+w/2)))
	dy := math.Max(0, math.Max(y-ey, ey-(y+ht)))
	dz := math.Max(0, math.Max(z-w/2-ez, ez-(z+w/2)))
	return dx*dx+dy*dy+dz*dz <= reach*reach
}

// mobInReach is withinEntityRange for a mob with the attack/interact slack.
func mobInReach(t *tracked, m *mob) bool {
	b := m.box()
	return t.dim == m.dim && withinEntityRange(t, m.x, m.y, m.z, b.w, b.h, interactSlack)
}

// creative range modifiers (ServerPlayer.updatePlayerAttributes): a creative
// player reaches half a block further for blocks and two for entities.
const (
	creativeBlockRangeSource  = "minecraft:creative_mode_block_range"
	creativeEntityRangeSource = "minecraft:creative_mode_entity_range"
)

// updatePlayerAttributes is ServerPlayer.updatePlayerAttributes' game-mode
// half, run every tick.
func (t *tracked) updatePlayerAttributes() {
	a := t.playerAttrs()
	br, er := a.Get(attr.BlockInteractionRange), a.Get(attr.EntityInteractionRange)
	if t.gamemode == gmCreative {
		br.AddModifier(attr.Modifier{Source: creativeBlockRangeSource, Amount: 0.5, Op: attr.AddValue})
		er.AddModifier(attr.Modifier{Source: creativeEntityRangeSource, Amount: 2, Op: attr.AddValue})
	} else {
		br.RemoveModifier(creativeBlockRangeSource)
		er.RemoveModifier(creativeEntityRangeSource)
	}
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
	return withinBlockReach(t, t.winPos.blockPos, menuReachSlack)
}

// withinBlockReach is Player.isWithinBlockInteractionRange(pos, slack): the
// eye position to the block's box, against block_interaction_range + slack.
func withinBlockReach(t *tracked, pos blockPos, slack float64) bool {
	reach := t.playerAttrs().Value(attr.BlockInteractionRange) + slack
	ex, ey, ez := t.x, t.y+t.eyeHeight(), t.z
	bx, by, bz := float64(pos.x), float64(pos.y), float64(pos.z)
	dx := math.Max(0, math.Max(bx-ex, ex-(bx+1)))
	dy := math.Max(0, math.Max(by-ey, ey-(by+1)))
	dz := math.Max(0, math.Max(bz-ez, ez-(bz+1)))
	return dx*dx+dy*dy+dz*dz <= reach*reach
}
