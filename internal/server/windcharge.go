package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// A wind charge's burst (vanilla AbstractWindCharge.explode → a TRIGGER
// explosion): power 1.2, breaks nothing, hurts nothing, but every block its
// blast rays reach gets its onExplosionHit — wooden doors, trapdoors and
// fence gates swing, buttons press, levers flip, bells ring, candles go out,
// beehives send their bees after whoever threw it. Iron doors and trapdoors
// ignore it (BlockSetType.canOpenByWindCharge), as does anything a redstone
// signal is holding.

const windChargeRadius = 1.2

var (
	ironDoorMin, ironDoorMax         = worldgen.BlockRange("iron_door")
	ironTrapdoorMin, ironTrapdoorMax = worldgen.BlockRange("iron_trapdoor")
)

func isIronDoor(s uint32) bool     { return s >= ironDoorMin && s <= ironDoorMax }
func isIronTrapdoor(s uint32) bool { return s >= ironTrapdoorMin && s <= ironTrapdoorMax }

// windBurst is the gust at the point of impact.
func (h *hub) windBurst(players map[int32]*tracked, dim int, cx, cy, cz float64, shooter int32) {
	h.playSoundDim(players, dim, "minecraft:entity.wind_charge.wind_burst", sndNeutral, cx, cy, cz, 1, 1)
	if dim == 0 {
		h.spawnParticles(players, particlePoof, cx, cy, cz, 0.4, 0.1, 12)
	}
	w := h.worldFor(dim)
	if w == nil {
		return
	}
	for pos := range h.blastPositions(w, cx, cy, cz, windChargeRadius) {
		h.triggerBlock(players, dim, pos, w.At(pos.x, pos.y, pos.z), shooter)
	}
}

// triggerBlock is one block's reaction to a triggering explosion.
func (h *hub) triggerBlock(players map[int32]*tracked, dim int, pos blockPos, st uint32, shooter int32) {
	info, ok := worldgen.InfoForState(st)
	if !ok {
		return
	}
	cx, cy, cz := float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5
	switch {
	case isBell(st):
		h.ringBell(players, dim, pos, -1)
	case inRanges(candleRanges, st):
		if boolProp(st, "lit") {
			h.extinguishCandle(players, dim, pos, st)
		}
	case isBeeHome(st):
		if t := players[shooter]; t != nil {
			h.angerBees(players, t, pos)
		}
	case isLever(st):
		if dim == 0 {
			h.toggleLever(players, pos, st)
		}
	case isButton(st):
		if dim == 0 {
			h.pressButton(players, pos, st)
		}
	case info.HasProperty("hinge"): // a door: the lower half swings both
		if isIronDoor(st) || boolProp(st, "powered") || worldgen.GetProperty(info, st, "half") != "lower" {
			return
		}
		open := !boolProp(st, "open")
		h.setBlockAt(players, dim, pos, setBoolProp(st, "open", open))
		w := h.worldFor(dim)
		if upper := w.At(pos.x, pos.y+1, pos.z); upper != st {
			if ui, ok := worldgen.InfoForState(upper); ok && ui.HasProperty("hinge") {
				h.setBlockAt(players, dim, blockPos{pos.x, pos.y + 1, pos.z}, setBoolProp(upper, "open", open))
			}
		}
		h.playSoundDim(players, dim, openSound("minecraft:block.wooden_door", open), sndBlock, cx, cy, cz, 1, 0.9+h.rng.Float32()*0.1)
	case info.HasProperty("in_wall"): // a fence gate
		if boolProp(st, "powered") {
			return
		}
		open := !boolProp(st, "open")
		h.setBlockAt(players, dim, pos, setBoolProp(st, "open", open))
		h.playSoundDim(players, dim, openSound("minecraft:block.fence_gate", open), sndBlock, cx, cy, cz, 1, 0.9+h.rng.Float32()*0.1)
	case info.HasProperty("open") && info.HasProperty("half"): // a trapdoor
		if isIronTrapdoor(st) || boolProp(st, "powered") {
			return
		}
		open := !boolProp(st, "open")
		h.setBlockAt(players, dim, pos, setBoolProp(st, "open", open))
		h.playSoundDim(players, dim, openSound("minecraft:block.wooden_trapdoor", open), sndBlock, cx, cy, cz, 1, 0.9+h.rng.Float32()*0.1)
	}
}

func openSound(base string, open bool) string {
	if open {
		return base + ".open"
	}
	return base + ".close"
}
