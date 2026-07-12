package server

import "github.com/tachyne/tachyne-world/internal/worldgen"

// Block drop tables — what a broken or destroyed block yields as item entities,
// with vanilla probabilities. Rolled on the hub goroutine (uses h.rng). This is
// the hand-written core for blocks we have item IDs for; a generated table from
// minecraft-data blockLoot is the follow-up so every block drops correctly.

// Item network IDs (1.21.5, from minecraft-data items.json).
var (
	itemWheatSeeds = itemByName["wheat_seeds"]
	itemFlint      = itemByName["flint"]
	itemGravel     = itemByName["gravel"]
	itemCobble     = itemByName["cobblestone"]
	itemDirt       = itemByName["dirt"]
	itemCoal       = itemByName["coal"]
	itemOakSapling = itemByName["oak_sapling"]
	itemStick      = itemByName["stick"]
	itemApple      = itemByName["apple"]
	itemDandelion  = itemByName["dandelion"]
	itemPoppy      = itemByName["poppy"]
	itemBeef       = itemByName["beef"] // raw beef (food)
	itemLeather    = itemByName["leather"]
)

type drop struct {
	item  int32
	count int
}

// rollDrops returns the items a destroyed block yields. Probabilistic entries use
// the hub RNG, so this must run on the hub goroutine. Special probabilistic drops
// are hand-written here; everything else falls through to the generated default
// drop table (loot_gen.go).
func (h *hub) rollDrops(state uint32) []drop {
	switch {
	case state == worldgen.ShortGrass || state == worldgen.Fern:
		if h.rng.Intn(8) == 0 { // 1/8 = 12.5% wheat seeds
			return []drop{{itemWheatSeeds, 1}}
		}
		return nil
	case state == worldgen.Gravel:
		if h.rng.Intn(10) == 0 { // 1/10 = 10% flint, else gravel
			return []drop{{itemFlint, 1}}
		}
		return []drop{{itemGravel, 1}}
	case isAnyLeaf(state):
		return h.leafDrops() // 5% sapling / 2% sticks / 0.5% apple
	}
	if item, ok := generatedDrop(state); ok { // generated default: block drops its item
		return []drop{{item, 1}}
	}
	return nil
}

// leafDrops rolls a broken/decayed leaf's loot independently.
func (h *hub) leafDrops() []drop {
	var ds []drop
	if h.rng.Intn(20) == 0 {
		ds = append(ds, drop{itemOakSapling, 1})
	}
	if h.rng.Intn(50) == 0 {
		ds = append(ds, drop{itemStick, 1 + h.rng.Intn(2)})
	}
	if h.rng.Intn(200) == 0 {
		ds = append(ds, drop{itemApple, 1})
	}
	return ds
}
