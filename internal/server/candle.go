package server

import (
	"math"

	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Candles and candle cakes (vanilla AbstractCandleBlock / CandleBlock /
// CandleCakeBlock, plus the lighting half of FlintAndSteelItem and
// FireChargeItem). Flint and steel or a fire charge lights an unlit, dry
// candle, candle cake or campfire in place; an empty hand snuffs a burning
// candle (on a cake, only when the click lands on the candle half); any other
// click on a candle cake eats it — the cake takes its first bite and the
// candle pops out as an item.

const (
	sndFlintSteelUse    = "minecraft:item.flintandsteel.use"
	sndFireChargeUse    = "minecraft:item.firecharge.use"
	sndCandleExtinguish = "minecraft:block.candle.extinguish"
)

// candleCakeItems is the reverse of candleCakeBases: which candle a candle
// cake gives back. Each candle cake spans two states, lit then unlit.
var candleCakeItems = func() map[uint32]int32 {
	out := map[uint32]int32{}
	for item, base := range candleCakeBases {
		out[base] = item
	}
	return out
}()

// candleCakeOf reports whether a state is a candle cake and which candle it
// carries.
func candleCakeOf(s uint32) (int32, bool) {
	if it, ok := candleCakeItems[s]; ok {
		return it, true
	}
	if s > 0 {
		if it, ok := candleCakeItems[s-1]; ok {
			return it, true
		}
	}
	return 0, false
}

// canLightBlock is CampfireBlock/CandleBlock/CandleCakeBlock.canLight: an
// unlit candle, candle cake or campfire that is not under water.
func canLightBlock(s uint32) bool {
	if !isCampfireBlock(s) && !inRanges(candleRanges, s) {
		return false
	}
	info, ok := worldgen.InfoForState(s)
	if !ok {
		return false
	}
	return worldgen.GetProperty(info, s, "lit") == "false" && worldgen.GetProperty(info, s, "waterlogged") != "true"
}

// lightBlock sets a lightable block burning with the given sound.
func (h *hub) lightBlock(players map[int32]*tracked, dim int, pos blockPos, state uint32, sound string) bool {
	if !canLightBlock(state) {
		return false
	}
	info, _ := worldgen.InfoForState(state)
	lit := worldgen.SetProperty(info, state, "lit", "true")
	if lit == state {
		return false
	}
	h.setBlockAt(players, dim, pos, lit)
	h.playSoundDim(players, dim, sound, sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 0.8+h.rng.Float32()*0.4)
	return true
}

// extinguishCandle is AbstractCandleBlock.extinguish.
func (h *hub) extinguishCandle(players map[int32]*tracked, dim int, pos blockPos, state uint32) {
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return
	}
	unlit := worldgen.SetProperty(info, state, "lit", "false")
	if unlit == state {
		return
	}
	h.setBlockAt(players, dim, pos, unlit)
	h.playSoundDim(players, dim, sndCandleExtinguish, sndBlock,
		float64(pos.x)+0.5, float64(pos.y)+0.5, float64(pos.z)+0.5, 1, 1)
}

// evLightBlock: a player set fire to a candle/candle cake/campfire with flint
// and steel or a fire charge (the session already checked the item and the
// block; the hub re-checks against the live state).
type evLightBlock struct {
	eid     int32
	x, y, z int
	sound   string
}

func (evLightBlock) isHubEvent() {}

func (h *hub) onLightBlock(players map[int32]*tracked, e evLightBlock) {
	t := players[e.eid]
	if t == nil {
		return
	}
	pos := blockPos{e.x, e.y, e.z}
	h.lightBlock(players, t.dim, pos, h.worldFor(t.dim).At(e.x, e.y, e.z), e.sound)
}

// evUseCandle: a right-click on a candle or candle cake that was not a
// lighter — snuff it, or eat the cake.
type evUseCandle struct {
	eid     int32
	x, y, z int
	cy      float32 // click height within the block: above 0.5 hits the candle on a cake
}

func (evUseCandle) isHubEvent() {}

func (h *hub) useCandle(players map[int32]*tracked, e evUseCandle) {
	t := players[e.eid]
	if t == nil {
		return
	}
	pos := blockPos{e.x, e.y, e.z}
	state := h.worldFor(t.dim).At(e.x, e.y, e.z)
	info, ok := worldgen.InfoForState(state)
	if !ok {
		return
	}
	held := heldStack(t)
	empty := held.count == 0 || held.item == 0
	lit := worldgen.GetProperty(info, state, "lit") == "true"
	candle, isCake := candleCakeOf(state)
	if !isCake {
		if empty && lit {
			h.extinguishCandle(players, t.dim, pos, state)
		}
		return
	}
	if empty && lit && e.cy > 0.5 {
		h.extinguishCandle(players, t.dim, pos, state)
		return
	}
	// CandleCakeBlock.useWithoutItem: CakeBlock.eat on a whole cake, then the
	// candle drops.
	if t.gamemode == gmSurvival {
		if t.food >= maxFood {
			return
		}
		t.food = min(maxFood, t.food+cakeFood)
		t.saturation = float32(math.Min(float64(t.food), float64(t.saturation)+cakeSat))
		h.sendHealth(t)
	}
	h.setBlockAt(players, t.dim, pos, cakeBase+1)
	h.spawnItemIn(players, t.dim, candle, 1, float64(e.x)+0.5, float64(e.y)+0.5, float64(e.z)+0.5)
	h.incCustom(t, "eat_cake_slice", 1)
}
