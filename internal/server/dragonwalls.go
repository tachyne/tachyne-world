package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// EnderDragon.checkWalls: every tick the dragon's head, neck and body boxes
// sweep through the terrain. Under mob_griefing each block they touch is
// removed outright (no drops) unless it is #dragon_immune — end stone,
// obsidian, bedrock, iron bars and the rest — and a puff of explosion
// particles marks the spot. Whatever it could not remove counts as a wall,
// and a dragon in a wall flies at four-fifths speed the next tick.

var (
	dragonImmuneRanges      = worldgen.BlockTag("dragon_immune")
	dragonTransparentRanges = worldgen.BlockTag("dragon_transparent")
)

// dragonCheckWalls clears the head, neck and body boxes and reports whether
// any of them met a block it could not break.
func (h *hub) dragonCheckWalls(players map[int32]*tracked, m *mob, sitting bool) bool {
	hit := false
	for _, p := range dragonPartsOf(m)[:3] { // head, neck, body: the wings and tail pass through terrain
		// A part's box is its width centred on it and its height upward
		// (a sitting dragon's lowered head is in the part's own height).
		x0, x1 := floorInt(p.x-p.w/2), floorInt(p.x+p.w/2)
		y0, y1 := floorInt(p.y), floorInt(p.y+p.h)
		z0, z1 := floorInt(p.z-p.w/2), floorInt(p.z+p.w/2)
		if h.dragonClearBox(players, m, x0, y0, z0, x1, y1, z1) {
			hit = true
		}
	}
	return hit
}

func (h *hub) dragonClearBox(players map[int32]*tracked, m *mob, x0, y0, z0, x1, y1, z1 int) bool {
	w := h.worldFor(m.dim)
	if w == nil {
		return false
	}
	hit, destroyed := false, false
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				st := w.At(x, y, z)
				if isAnyAir(st) || inRanges2(st, dragonTransparentRanges) {
					continue
				}
				if !h.rules.MobGriefing || inRanges2(st, dragonImmuneRanges) {
					hit = true
					continue
				}
				// Level.removeBlock(pos, false): the block's fluid stays behind.
				h.setBlockAt(players, m.dim, blockPos{x, y, z}, fluidLeftBehind(st))
				destroyed = true
			}
		}
	}
	if destroyed {
		// levelEvent 2008: the explosion puff, at a random cell of the box.
		h.levelEvent(players, m.dim, 2008, x0+h.rng.Intn(x1-x0+1), y0+h.rng.Intn(y1-y0+1), z0+h.rng.Intn(z1-z0+1), 0)
	}
	return hit
}

// fluidLeftBehind is FluidState.createLegacyBlock for a removed block: water
// where it was waterlogged (or was water), air otherwise.
func fluidLeftBehind(st uint32) uint32 {
	if info, ok := worldgen.InfoForState(st); ok && info.HasProperty("waterlogged") &&
		worldgen.GetProperty(info, st, "waterlogged") == "true" {
		return worldgen.Water
	}
	return worldgen.Air
}
