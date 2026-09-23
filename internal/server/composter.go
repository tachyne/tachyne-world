package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// The composter. Feed it plant matter and each piece has a chance to raise the
// pile a level; the first item always takes (vanilla lets level 0 through
// regardless of the roll). At level seven it sits a second and then turns
// ready, and a ready composter hands back one bone meal and empties.
//
// The chances are vanilla's five tiers: leaves and seeds are poor compost,
// crops and flowers middling, blocks of the same good, and cake best of all.

var composterBase = worldgen.BlockBase("composter") // levels 0..8 run upward

const (
	composterFull  = 7  // the pile is full but not yet composted
	composterReady = 8  // vanilla READY: right-click for bone meal
	composterDelay = 20 // ticks between full and ready
)

// compostChance is the item→chance table, keyed by name because ids move
// between versions.
var compostChance = func() map[int32]float64 {
	out := map[int32]float64{}
	for name, chance := range compostChances {
		if id, ok := itemByName[name]; ok {
			out[int32(id)] = chance
		}
	}
	return out
}()

// composterLevel reports whether a state is a composter, and how full it is.
func composterLevel(st uint32) (int, bool) {
	if st < composterBase || st > composterBase+composterReady {
		return 0, false
	}
	return int(st - composterBase), true
}

type evUseComposter struct {
	eid, slot int32
	x, y, z   int
}

func (evUseComposter) isHubEvent() {}

// useComposter is the right-click: empty a ready one, or feed it.
func (h *hub) useComposter(players map[int32]*tracked, t *tracked, pos blockPos) {
	state := h.worldFor(t.dim).At(pos.x, pos.y, pos.z)
	level, ok := composterLevel(state)
	if !ok {
		return
	}
	cx, cy, cz := float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5
	if level == composterReady {
		h.setBlockAt(players, t.dim, pos, composterBase)
		h.vib(t.dim, freqBlockChange, pos.x, pos.y, pos.z, t.p.eid) // ComposterBlock.empty
		h.spawnItemIn(players, t.dim, itemBoneMeal, 1, cx, cy+1, cz)
		h.playSoundDim(players, t.dim, "minecraft:block.composter.empty", sndBlock, cx, cy, cz, 1, 1)
		return
	}
	held := heldStack(t)
	chance, compostable := compostChance[held.item]
	if !compostable || level >= composterFull {
		return
	}
	if t.gamemode == gmSurvival {
		h.consumeHeld(t)
	}
	// Vanilla's roll: an empty composter always takes the first item, and
	// after that the item's own chance decides.
	if level != 0 && h.rng.Float64() >= chance {
		h.playSoundDim(players, t.dim, "minecraft:block.composter.fill", sndBlock, cx, cy, cz, 1, 1)
		return
	}
	h.setBlockAt(players, t.dim, pos, composterBase+uint32(level)+1)
	h.vib(t.dim, freqBlockChange, pos.x, pos.y, pos.z, t.p.eid) // ComposterBlock.addItem, on a level that actually rose
	h.playSoundDim(players, t.dim, "minecraft:block.composter.fill_success", sndBlock, cx, cy, cz, 1, 1)
	if level+1 == composterFull {
		h.scheduleIn(t.dim, pos, composterDelay) // it finishes composting a second later
	}
}

// tickComposter is the scheduled tick that turns a full pile into a ready one.
// Reports whether it handled the update, so it can sit in processUpdate's chain.
func (h *hub) tickComposter(players map[int32]*tracked, dim int, pos blockPos, state uint32) bool {
	level, ok := composterLevel(state)
	if !ok {
		return false
	}
	if level == composterFull {
		h.setBlockAt(players, dim, pos, composterBase+composterReady)
		h.playSoundDim(players, dim, "minecraft:block.composter.ready", sndBlock,
			float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
	}
	return true
}

// composterInsert is ComposterBlock's InputContainer: a hopper feeding a
// composter that is not yet full. The item takes the same chance it would
// from a hand — an empty composter always accepts the first one — and a
// composter at level 7 is waiting to become bone meal and takes nothing.
func (h *hub) composterInsert(target simPos, level int, one invStack) bool {
	if level >= composterFull {
		return false
	}
	chance, compostable := compostChance[one.item]
	if !compostable {
		return false
	}
	cx, cy, cz := float64(target.x)+0.5, float64(target.y)+0.5, float64(target.z)+0.5
	if level != 0 && h.rng.Float64() >= chance {
		h.playSoundDim(h.playersRef, target.dim, "minecraft:block.composter.fill", sndBlock, cx, cy, cz, 1, 1)
		return true // the item is spent either way, as it is from a hand
	}
	h.setBlockAt(h.playersRef, target.dim, target.blockPos, composterBase+uint32(level)+1)
	h.vib(target.dim, freqBlockChange, target.x, target.y, target.z, 0)
	h.playSoundDim(h.playersRef, target.dim, "minecraft:block.composter.fill_success", sndBlock, cx, cy, cz, 1, 1)
	if level+1 == composterFull {
		h.scheduleIn(target.dim, target.blockPos, composterDelay)
	}
	return true
}
