package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// The respawn anchor: the Nether's bed. Glowstone charges it (four charges
// max), a charged one claims your respawn point, respawning spends a charge —
// and anywhere it does not work, using one detonates exactly like a bed in the
// Nether does.
//
// Ported from RespawnAnchorBlock. The charge lives in the block's own state, so
// nothing here needs persisting: world.gob already carries it.

const anchorMaxCharge = 4 // RespawnAnchorBlock.MAX_CHARGES

var (
	anchorMin, anchorMax = worldgen.BlockRange("respawn_anchor")
	itemGlowstoneBlock   = itemByName["glowstone"] // the BLOCK; itemGlowstone is the dust
)

// anchorCharge returns a respawn anchor's charge, or -1 if the state is not one.
// The five states run in charge order from the block's base.
func anchorCharge(state uint32) int {
	if state < anchorMin || state > anchorMax {
		return -1
	}
	return int(state - anchorMin)
}

// anchorWithCharge is the same anchor at a different charge.
func anchorWithCharge(state uint32, charge int) uint32 {
	if anchorCharge(state) < 0 {
		return state
	}
	return anchorMin + uint32(min(max(charge, 0), anchorMaxCharge))
}

// evUseAnchor is a right-click on a respawn anchor.
type evUseAnchor struct {
	eid     int32
	slot    int32
	x, y, z int
}

func (evUseAnchor) isHubEvent() {}

// handleUseAnchor is RespawnAnchorBlock.useItemOn + useWithoutItem in one: a
// held glowstone charges an anchor that has room, otherwise a charged anchor
// claims the respawn point — or blows up where anchors do not work.
func (h *hub) handleUseAnchor(players map[int32]*tracked, t *tracked, pos blockPos) {
	if t.dead {
		return
	}
	state := h.worldFor(t.dim).Block(pos.x, pos.y, pos.z)
	charge := anchorCharge(state)
	if charge < 0 {
		return
	}
	if heldStack(t).item == itemGlowstoneBlock && charge < anchorMaxCharge {
		h.setBlockAt(players, t.dim, pos, anchorWithCharge(state, charge+1))
		h.advance(players, t, "item_used_on_block", advMatch{blockState: anchorWithCharge(state, charge+1), item: heldStack(t).item})
		if t.gamemode == gmSurvival {
			h.consumeHeld(t)
		}
		h.playSound(players, "minecraft:block.respawn_anchor.charge", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
		h.advance(players, t, "charge_respawn_anchor", advMatch{})
		return
	}
	if charge == 0 {
		return // an empty anchor does nothing at all, even where it works
	}
	if !anchorWorks(t.dim) {
		h.blowUpRespawnBlock(players, t, pos, false)
		return
	}
	if h.spawns != nil {
		h.spawns.set(t.p.name, pos, t.dim)
		t.p.trySendEv(chatEv("Respawn point set"))
		h.playSound(players, "minecraft:block.respawn_anchor.set_spawn", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	}
}
