package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Coral dies out of water.
//
// Every coral block, plant and fan has a dead twin, and vanilla converts one
// to the other about three seconds after the last water beside it goes. Until
// now coral mined out of a reef and replanted on land stayed brilliantly
// alive forever, which made the whole "keep it wet or lose the colour"
// mechanic — and silk touch's point — meaningless.

var coralFamilies = []string{"tube", "brain", "bubble", "fire", "horn"}

// coralDead maps every live coral state to the matching dead one. The two
// blocks share a state layout, so the offset within the range carries across
// (a wall fan keeps its facing, a waterlogged fan its waterlogging).
var coralDead = func() map[uint32]uint32 {
	out := map[uint32]uint32{}
	for _, fam := range coralFamilies {
		for _, shape := range []string{"_coral_block", "_coral", "_coral_fan", "_coral_wall_fan"} {
			live, liveMax, ok := worldgen.BlockRangeOK(fam + shape)
			if !ok {
				continue
			}
			dead, _, ok := worldgen.BlockRangeOK("dead_" + fam + shape)
			if !ok {
				continue
			}
			for s := live; s <= liveMax; s++ {
				out[s] = dead + (s - live)
			}
		}
	}
	return out
}()

const (
	coralDieMin = 60 // vanilla schedules the die tick 60 + rand(40) ticks out
	coralDieVar = 40
)

// coralTouchesWater is vanilla's scanForWater: the block's own waterlogging,
// or water in any of the six neighbours.
func coralTouchesWater(w interface {
	At(x, y, z int) uint32
}, pos blockPos, state uint32) bool {
	if info, ok := worldgen.InfoForState(state); ok &&
		worldgen.GetProperty(info, state, "waterlogged") == "true" {
		return true
	}
	for _, d := range supportNeighbours {
		if worldgen.IsWater(w.At(pos.x+d[0], pos.y+d[1], pos.z+d[2])) {
			return true
		}
	}
	return false
}

// scheduleCoralDeath arms the die tick for any coral around a change that has
// just been left high and dry.
func (h *hub) scheduleCoralDeath(dim int, pos blockPos) {
	w := h.worldFor(dim)
	if w == nil {
		return
	}
	for _, d := range append([][3]int{{0, 0, 0}}, supportNeighbours[:]...) {
		p := blockPos{pos.x + d[0], pos.y + d[1], pos.z + d[2]}
		st := w.At(p.x, p.y, p.z)
		if _, isCoral := coralDead[st]; !isCoral {
			continue
		}
		if coralTouchesWater(w, p, st) {
			continue
		}
		h.scheduleIn(dim, p, uint64(coralDieMin+h.rng.Intn(coralDieVar)))
	}
}

// tickCoral is the scheduled tick: still dry, still coral → it bleaches.
// Reports whether it handled the update.
func (h *hub) tickCoral(players map[int32]*tracked, dim int, pos blockPos, state uint32) bool {
	dead, isCoral := coralDead[state]
	if !isCoral {
		return false
	}
	if !coralTouchesWater(h.worldFor(dim), pos, state) {
		h.setBlockAt(players, dim, pos, dead)
	}
	return true
}
