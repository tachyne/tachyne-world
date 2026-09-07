package server

import (
	"github.com/tachyne/tachyne-world/internal/worldgen"
)

// Cauldrons — the vanilla interaction model: buckets fill/drain whole
// cauldrons, bottles move water a level at a time, banners wash a pattern
// layer off, and rain (or snow) slowly fills one left under open sky.

// Cauldron content kinds.
const (
	cauldronEmpty = iota
	cauldronWater
	cauldronLava
	cauldronSnow
)

var (
	cauldronState      = worldgen.BlockBase("cauldron")
	waterCauldronBase  = worldgen.BlockBase("water_cauldron") // +0..2 = level 1..3
	lavaCauldronState  = worldgen.BlockBase("lava_cauldron")
	powderCauldronBase = worldgen.BlockBase("powder_snow_cauldron")
)

// cauldronOf classifies a state as a cauldron kind + fill level (ok=false for
// non-cauldron states). Lava is always full (level 3).
func cauldronOf(st uint32) (kind, level int, ok bool) {
	switch {
	case st == cauldronState:
		return cauldronEmpty, 0, true
	case st >= waterCauldronBase && st <= waterCauldronBase+2:
		return cauldronWater, int(st-waterCauldronBase) + 1, true
	case st == lavaCauldronState:
		return cauldronLava, 3, true
	case st >= powderCauldronBase && st <= powderCauldronBase+2:
		return cauldronSnow, int(st-powderCauldronBase) + 1, true
	}
	return 0, 0, false
}

type evCauldron struct {
	eid, slot int32
	x, y, z   int
}

func (evCauldron) isHubEvent() {}

// useCauldron applies one right-click on a cauldron with whatever is held —
// the vanilla CauldronInteraction map.
func (h *hub) useCauldron(players map[int32]*tracked, t *tracked, slot int32, x, y, z int) {
	if int(slot) != t.p.heldSlot() || t.inv == nil {
		return
	}
	st := h.worldFor(t.dim).At(x, y, z)
	kind, level, ok := cauldronOf(st)
	if !ok {
		return
	}
	// Vanilla keeps two counters: FILL_CAULDRON for pouring in, USE_CAULDRON
	// for everything taken out or washed.
	setStat := func(state uint32, snd, stat string) {
		h.setBlockLive(players, t.dim, x, y, z, state)
		if snd != "" {
			h.playSoundDim(players, t.dim, snd, sndBlock, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
		}
		h.incCustom(t, stat, 1)
	}
	set := func(state uint32, snd string) { setStat(state, snd, "use_cauldron") }
	fill := func(state uint32, snd string) { setStat(state, snd, "fill_cauldron") }
	held := t.inv.slots[slot]
	switch held.item {
	case itemBucketH2O: // fills with water regardless of prior content
		fill(waterCauldronBase+2, "minecraft:item.bucket.empty")
		h.swapBucket(t, slot, itemBucket)
	case itemBucketLav:
		fill(lavaCauldronState, "minecraft:item.bucket.empty_lava")
		h.swapBucket(t, slot, itemBucket)
	case itemBucketSnow:
		fill(powderCauldronBase+2, "minecraft:item.bucket.empty_powder_snow")
		h.swapBucket(t, slot, itemBucket)
	case itemBucket:
		switch {
		case kind == cauldronWater && level == 3:
			set(cauldronState, "minecraft:item.bucket.fill")
			h.giveFilled(players, t, slot, itemBucketH2O)
		case kind == cauldronLava:
			set(cauldronState, "minecraft:item.bucket.fill_lava")
			h.giveFilled(players, t, slot, itemBucketLav)
		case kind == cauldronSnow && level == 3:
			set(cauldronState, "minecraft:item.bucket.fill_powder_snow")
			h.giveFilled(players, t, slot, itemBucketSnow)
		}
	case itemGlassBottle:
		if kind == cauldronWater {
			next := cauldronState
			if level > 1 {
				next = waterCauldronBase + uint32(level-2)
			}
			set(next, "minecraft:item.bottle.fill")
			h.giveFilledStack(players, t, slot, potionStack(potWater))
		}
	case itemPotion:
		if held.potion == potWater && (kind == cauldronEmpty || (kind == cauldronWater && level < 3)) {
			next := waterCauldronBase // empty → level 1
			if kind == cauldronWater {
				next = waterCauldronBase + uint32(level) // level+1
			}
			fill(next, "minecraft:item.bottle.empty")
			h.giveFilled(players, t, slot, itemGlassBottle)
		}
	default:
		// Washing dyed leather armour (CauldronInteraction DYED_ITEM): the
		// colour comes off for one water level.
		if kind == cauldronWater && isDyeable(held.item) && held.color != 0 {
			held.color = 0
			t.inv.slots[slot] = held
			h.sendSlot(t, int(slot))
			h.incCustom(t, "clean_armor", 1)
			next := cauldronState
			if level > 1 {
				next = waterCauldronBase + uint32(level-2)
			}
			set(next, "")
			return
		}
		// Washing a dyed shulker box back to plain (CauldronInteraction
		// SHULKER_BOX): one water level, contents kept.
		if kind == cauldronWater && isShulkerBoxItem(held.item) && held.item != int32(itemByName["shulker_box"]) {
			held.item = int32(itemByName["shulker_box"])
			t.inv.slots[slot] = held
			h.sendSlot(t, int(slot))
			h.incCustom(t, "clean_shulker_box", 1)
			next := cauldronState
			if level > 1 {
				next = waterCauldronBase + uint32(level-2)
			}
			set(next, "")
			return
		}
		// Washing: a patterned banner loses its TOP layer for one water level.
		if kind == cauldronWater && held.patCount() > 0 {
			held.pats[held.patCount()-1] = bannerLayer{}
			h.incCustom(t, "clean_banner", 1)
			t.inv.slots[slot] = held
			h.sendSlot(t, int(slot))
			next := cauldronState
			if level > 1 {
				next = waterCauldronBase + uint32(level-2)
			}
			set(next, "")
		}
	}
}

// cauldronPrecip is the rain/snow fill roll for a sky-exposed cauldron: rain
// tops up empty and water cauldrons at 5%, snow lays powder snow at 10%
// (vanilla shouldHandlePrecipitation + the per-block handlers).
func (h *hub) cauldronPrecip(players map[int32]*tracked, pos blockPos, st uint32, snowing bool) {
	kind, level, ok := cauldronOf(st)
	if !ok {
		return
	}
	switch {
	case !snowing && kind == cauldronEmpty && h.rng.Float32() < 0.05:
		h.setBlock(players, pos, waterCauldronBase)
	case !snowing && kind == cauldronWater && level < 3 && h.rng.Float32() < 0.05:
		h.setBlock(players, pos, waterCauldronBase+uint32(level))
	case snowing && kind == cauldronEmpty && h.rng.Float32() < 0.1:
		h.setBlock(players, pos, powderCauldronBase)
	case snowing && kind == cauldronSnow && level < 3 && h.rng.Float32() < 0.1:
		h.setBlock(players, pos, powderCauldronBase+uint32(level))
	}
}
