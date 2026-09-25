package server

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
	// potion is what a set_potion function gave the stack (0 = none): a
	// stray's, bogged's or parched's tipped arrow.
	potion int8
	// fixed marks a roll Looting must not touch: an entry whose vanilla table
	// carries no enchanted_count_increase (a sheep's fleece, an elder
	// guardian's sponge and tide template, the guardians' rare fish).
	fixed bool
}

// rollDrops returns the items a destroyed block yields when no player tool is
// involved — a piston, a lost support, a decaying leaf, a banner or pot on its
// way to picking up its layers. Uses the hub RNG, so it must run on the hub
// goroutine.
//
// It rolls the block's own vanilla loot table with no tool, as
// Block.dropResources does. A few tables use functions the evaluator does not
// run, and only those are hand-written here. There used to be a flat
// "block drops one of its item" table as the fallback, built from a
// third-party summary; every block it covered now has its real table.
func (h *hub) rollDrops(state uint32) []drop {
	if ds := h.evalBlockLoot(lootCtx{state: state, rng: h.rng.Intn, randf: h.rng.Float64}); ds != nil {
		return ds
	}
	switch {
	case state >= snowLayer1 && state <= snowLayer1+7:
		return []drop{{item: itemSnowball, count: int(state-snowLayer1) + 1}} // blocks/snow: a snowball a layer
	case isChorusFlower(state):
		return nil // blocks/chorus_flower: nothing
	case isDecoratedPot(state):
		return []drop{{item: itemDecoratedPot, count: 1}} // blocks/decorated_pot, unbroken: itself
	}
	if _, _, ok := doublePlantOf(state); ok {
		// blocks/tall_grass, large_fern: seeds 1/8 from whichever half
		// breaks while the other still stands (the location_check; the
		// positional sites ask doublePlantDropBlocked first).
		if h.rng.Intn(8) == 0 {
			return []drop{{item: itemWheatSeeds, count: 1}}
		}
		return nil
	}
	return nil
}
