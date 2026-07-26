package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Frogspawn is placed ON water rather than against a block face, so the
// client's own raycast — which ignores fluids — finds nothing and sends a
// plain use. The server has to do the water-surface hit itself, the way
// PlaceOnWaterBlockItem does: ray to the first water SOURCE, place in the
// block above it.
//
// A lily pad works today only because its item is an ordinary BlockItem in
// this engine's tables; frogspawn genuinely needed the surface hit.

var (
	itemFrogspawn  = itemByName["frogspawn"]
	frogspawnBlock = worldgen.BlockBase("frogspawn")
)

const frogspawnReach = 5.0

type evPlaceOnWater struct{ eid int32 }

func (evPlaceOnWater) isHubEvent() {}

// placeFrogspawn walks the look ray to the first water source and lays the
// spawn on top of it.
func (h *hub) placeFrogspawn(players map[int32]*tracked, t *tracked) {
	if t.dead || t.inv == nil || heldStack(t).item != itemFrogspawn {
		return
	}
	dx, dy, dz := lookVector(t.yaw, t.pitch)
	ox, oy, oz := t.x, t.y+1.5, t.z
	w := h.worldFor(t.dim)
	last := blockPos{int(math.Floor(ox)), int(math.Floor(oy)), int(math.Floor(oz))}
	for d := 0.0; d <= frogspawnReach; d += 0.1 {
		p := blockPos{int(math.Floor(ox + dx*d)), int(math.Floor(oy + dy*d)), int(math.Floor(oz + dz*d))}
		if p == last && d > 0 {
			continue
		}
		last = p
		st := w.At(p.x, p.y, p.z)
		switch {
		case st == worldgen.WaterBase:
			if w.At(p.x, p.y+1, p.z) != worldgen.Air {
				return // something is already sitting on the surface
			}
			h.setBlockLive(players, t.dim, p.x, p.y+1, p.z, frogspawnBlock)
			h.playSoundDim(players, t.dim, "minecraft:block.frogspawn.place", sndBlock,
				float64(p.x)+0.5, float64(p.y)+1.5, float64(p.z)+0.5, 1, 1)
			if t.gamemode == gmSurvival {
				h.consumeHeld(t)
			}
			return
		case worldgen.Collides(st):
			return // a solid before any water
		}
	}
}
