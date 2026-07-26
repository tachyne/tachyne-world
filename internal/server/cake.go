package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Cake. Seven slices: each right-click by a player with room to eat takes a
// bite worth two food and a little saturation, and the seventh finishes it.
// A comparator beside one reads how much is left.
//
// A candle placed on an untouched cake turns it into the matching candle
// cake, which is a different block entirely — one per dye colour, plus plain.

var (
	cakeBase        = worldgen.BlockBase("cake") // bites 0..6 run upward from here
	candleCakeBases = func() map[int32]uint32 {
		out := map[int32]uint32{}
		if id, ok := itemByName["candle"]; ok {
			out[int32(id)] = worldgen.BlockBase("candle_cake")
		}
		for _, c := range dyeColors {
			if id, ok := itemByName[c+"_candle"]; ok {
				out[int32(id)] = worldgen.BlockBase(c + "_candle_cake")
			}
		}
		return out
	}()
)

const (
	cakeMaxBites = 6   // vanilla MAX_BITES: the seventh bite finishes it
	cakeFood     = 2   // food per slice
	cakeSat      = 0.1 // saturation per slice
)

// cakeBites reports whether a state is a cake, and how many bites it has lost.
func cakeBites(st uint32) (int, bool) {
	if st < cakeBase || st > cakeBase+cakeMaxBites {
		return 0, false
	}
	return int(st - cakeBase), true
}

// cakeSignal is the comparator read of a cake: (7 - bites) * 2, so a whole
// cake is 14 and the last slice is 2.
func cakeSignal(bites int) int { return (7 - bites) * 2 }

type evUseCake struct {
	eid     int32
	x, y, z int
}

func (evUseCake) isHubEvent() {}

// eatCake takes a bite — or, if the player is holding a candle and nobody has
// started on the cake yet, plants the candle in it instead.
func (h *hub) eatCake(players map[int32]*tracked, t *tracked, pos blockPos) {
	state := h.worldFor(t.dim).At(pos.x, pos.y, pos.z)
	bites, ok := cakeBites(state)
	if !ok {
		return
	}
	cx, cy, cz := float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5
	if base, isCandle := candleCakeBases[heldStack(t).item]; isCandle && bites == 0 {
		h.setBlockAt(players, t.dim, pos, base+1) // +1 = unlit, as a placed candle starts
		h.playSoundDim(players, t.dim, "minecraft:block.cake.add_candle", sndBlock, cx, cy, cz, 1, 1)
		if t.gamemode == gmSurvival {
			h.consumeHeld(t)
		}
		return
	}
	if t.gamemode == gmSurvival {
		if t.food >= maxFood {
			return // vanilla canEat(false): a full player cannot take a slice
		}
		t.food = min(maxFood, t.food+cakeFood)
		t.saturation = float32(math.Min(float64(t.food), float64(t.saturation)+cakeSat))
		h.sendHealth(t)
	}
	if bites < cakeMaxBites {
		h.setBlockAt(players, t.dim, pos, cakeBase+uint32(bites)+1)
	} else {
		h.setBlockAt(players, t.dim, pos, worldgen.Air)
	}
	h.incCustom(t, "eat_cake_slice", 1)
}
