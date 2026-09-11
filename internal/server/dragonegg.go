package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// The dragon egg. Hit it or use it and it blinks away (DragonEggBlock.attack
// and useWithoutItem → teleport): up to a thousand tries at a cell within
// ±15 blocks sideways and ±7 up or down (two dice each way, so near cells
// are likelier), the first empty one inside the world border taking the
// egg. The portal particles of the blink are the client's own.

type evDragonEgg struct {
	eid     int32
	x, y, z int
}

func (evDragonEgg) isHubEvent() {}

func isDragonEgg(s uint32) bool { return s == worldgen.DragonEgg }

func (h *hub) onDragonEgg(players map[int32]*tracked, e evDragonEgg) {
	t := players[e.eid]
	if t == nil {
		return
	}
	pos := blockPos{e.x, e.y, e.z}
	if !isDragonEgg(h.worldFor(t.dim).At(pos.x, pos.y, pos.z)) {
		return
	}
	if to, ok := h.dragonEggTarget(t.dim, pos); ok {
		h.setBlockAt(players, t.dim, to, worldgen.DragonEgg)
		h.setBlockAt(players, t.dim, pos, worldgen.Air)
	}
}

// dragonEggTarget rolls the blink's destination.
func (h *hub) dragonEggTarget(dim int, from blockPos) (blockPos, bool) {
	w := h.worldFor(dim)
	size := h.border.sizeAt(h.tick.Load())
	for i := 0; i < 1000; i++ {
		to := blockPos{
			from.x + h.rng.Intn(16) - h.rng.Intn(16),
			from.y + h.rng.Intn(8) - h.rng.Intn(8),
			from.z + h.rng.Intn(16) - h.rng.Intn(16),
		}
		if !h.inWorldYIn(dim, to.y) || w.At(to.x, to.y, to.z) != worldgen.Air {
			continue
		}
		if dim == dimOverworld && h.border.distanceToBorder(float64(to.x)+0.5, float64(to.z)+0.5, size) < 0 {
			continue
		}
		return to, true
	}
	return blockPos{}, false
}
