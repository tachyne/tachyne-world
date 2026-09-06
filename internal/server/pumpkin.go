package server

import (
	attachproto "github.com/tachyne/tachyne-common/attach"
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Carving a pumpkin (vanilla PumpkinBlock.useItemOn): shears on an uncarved
// pumpkin turn it into a carved one facing the clicked side (or away from
// the player when the top or bottom is clicked), four seeds pop out of the
// carved face, and the shears take a point of wear.

var itemPumpkinSeeds = itemByName["pumpkin_seeds"]

type evCarvePumpkin struct {
	eid     int32
	x, y, z int
	face    int32
	yaw     float32
}

func (evCarvePumpkin) isHubEvent() {}

func (h *hub) carvePumpkin(players map[int32]*tracked, e evCarvePumpkin) {
	t := players[e.eid]
	if t == nil {
		return
	}
	w := h.worldFor(t.dim)
	if w == nil || w.At(e.x, e.y, e.z) != pumpkinBlock {
		return
	}
	facing := faceName(e.face)
	if e.face < 2 { // top or bottom: the face points away from the carver
		facing = oppositeFacing(yawFacing(e.yaw))
	}
	info, ok := worldgen.InfoForState(carvedPumpkinBase)
	if !ok {
		return
	}
	pos := blockPos{e.x, e.y, e.z}
	cx, cy, cz := float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5
	h.playSoundDim(players, t.dim, "minecraft:block.pumpkin.carve", sndBlock, cx, cy, cz, 1, 1)
	h.setBlockAt(players, t.dim, pos, worldgen.SetProperty(info, carvedPumpkinBase, "facing", facing))
	dx, dz := facingDelta(facing)
	h.spawnItemIn(players, t.dim, itemPumpkinSeeds, 4, cx+float64(dx)*0.65, float64(e.y)+0.1, cz+float64(dz)*0.65)
	if t.gamemode == gmSurvival {
		h.applyToolWear(t, t.p.heldSlot(), 1)
	}
	h.incStat(t, attachproto.StatUsed, itemShears, 1)
}

// evLightOre: a right-click on a dark redstone ore (RedStoneOreBlock.interact).
type evLightOre struct {
	eid     int32
	x, y, z int
}

func (evLightOre) isHubEvent() {}

func (h *hub) lightOre(players map[int32]*tracked, e evLightOre) {
	t := players[e.eid]
	if t == nil {
		return
	}
	st := h.worldFor(t.dim).At(e.x, e.y, e.z)
	if isRedstoneOre(st) && !boolProp(st, "lit") {
		h.setBlockAt(players, t.dim, blockPos{e.x, e.y, e.z}, setBoolProp(st, "lit", true))
	}
}
