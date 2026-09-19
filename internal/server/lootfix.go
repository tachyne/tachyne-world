package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Block loot the generated tables cannot express (their pools lean on
// location_check and block_state_property), hand-ported from the vanilla
// tables: the two-tall grasses, snow layers and the chorus flower.

var (
	tallGrassLo, tallGrassHi = worldgen.BlockRange("tall_grass") // half=upper first, then lower
	largeFernLo, largeFernHi = worldgen.BlockRange("large_fern")
	itemShortGrass           = int32(itemByName["short_grass"])
	itemFernItem             = int32(itemByName["fern"])
	itemSnowLayer            = int32(itemByName["snow"])
)

// doublePlantOf reports a tall grass or large fern and its single-block
// counterpart (what shears cut it into).
func doublePlantOf(state uint32) (single int32, lower, ok bool) {
	switch {
	case state >= tallGrassLo && state <= tallGrassHi:
		return itemShortGrass, state == tallGrassHi, true
	case state >= largeFernLo && state <= largeFernHi:
		return itemFernItem, state == largeFernHi, true
	}
	return 0, false, false
}

// specialBlockDrops is the tool-aware loot for those blocks when a player
// breaks them: shears cut a two-tall plant into two of its small kind
// (blocks/tall_grass, blocks/large_fern), shears or Silk Touch lift snow
// layers as layers and anything else as one snowball a layer
// (blocks/snow), and a chorus flower drops nothing (blocks/chorus_flower).
// ok reports the block is one of these; the no-tool cases fall to rollDrops.
func (h *hub) specialBlockDrops(state uint32, held int32, silk bool) ([]drop, bool) {
	if single, _, ok := doublePlantOf(state); ok {
		if held == int32(itemShears) {
			return []drop{{single, 2}}, true
		}
		return h.rollDrops(state), true
	}
	if state >= snowLayer1 && state <= snowLayer1+7 {
		layers := int(state-snowLayer1) + 1
		if silk || held == int32(itemShears) {
			return []drop{{itemSnowLayer, layers}}, true
		}
		return []drop{{itemSnowball, layers}}, true
	}
	if isChorusFlower(state) {
		return nil, true
	}
	return nil, false
}

var (
	itemPrismarineShard    = int32(itemByName["prismarine_shard"])
	itemPrismarineCrystals = int32(itemByName["prismarine_crystals"])
	itemCodItem            = int32(itemByName["cod"])
	itemCookedCodItem      = int32(itemByName["cooked_cod"])
	itemWetSponge          = int32(itemByName["wet_sponge"])
	itemTideTemplate       = int32(itemByName["tide_armor_trim_smithing_template"])
)

// guardianLoot is entities/guardian and entities/elder_guardian: 0-2
// prismarine shards; then cod (cooked if it was burning) against
// prismarine crystals against nothing, at 2:2:1 for a guardian and 3:2:1
// for the elder; the elder adds a wet sponge for a player's kill and a
// tide armour trim template one time in five. The 2.5% player-kill fish
// gamble is not carried.
func (h *hub) guardianLoot(m *mob) []drop {
	var out []drop
	if n := h.rng.Intn(3); n > 0 {
		out = append(out, drop{itemPrismarineShard, n})
	}
	elder := m.etype == entityElderGuardian
	codW := 2
	if elder {
		codW = 3
	}
	r := h.rng.Intn(codW + 3)
	switch {
	case r < codW:
		fish := itemCodItem
		if m.burning {
			fish = itemCookedCodItem
		}
		out = append(out, drop{fish, 1})
	case r < codW+2:
		out = append(out, drop{itemPrismarineCrystals, 1})
	}
	if elder {
		if m.hitByPlayer {
			out = append(out, drop{itemWetSponge, 1})
		}
		if h.rng.Intn(5) == 0 {
			out = append(out, drop{itemTideTemplate, 1})
		}
	}
	return out
}
