package server

// Suspicious stew. A bowl, a red and a brown mushroom and one flower make a
// stew that carries the flower's effect (vanilla's SuspiciousStewRecipe over
// the SuspiciousEffectHolder blocks — every small flower, the wither rose
// and the eyeblossoms, each with its own effect and duration from Blocks),
// and a brown mooshroom fed such a flower gives that stew from the next
// bowl instead of plain mushroom stew (MushroomCow.mobInteract). The effect
// rides the stack as a table index and is a secret — vanilla's tooltip
// hides it — so it never reaches the wire.

type stewEffect struct {
	flower string
	effect int32
	secs   float64
}

// stewEffects is the flower table in Blocks.java order; a stack's stew field
// is 1 + its index here (0 = none).
var stewEffects = []stewEffect{
	{"dandelion", effSaturation, 0.35},
	{"torchflower", effNightVision, 5},
	{"poppy", effNightVision, 5},
	{"blue_orchid", effSaturation, 0.35},
	{"allium", effFireRes, 3},
	{"azure_bluet", effBlindness, 11},
	{"red_tulip", effWeakness, 7},
	{"orange_tulip", effWeakness, 7},
	{"white_tulip", effWeakness, 7},
	{"pink_tulip", effWeakness, 7},
	{"oxeye_daisy", effRegen, 7},
	{"cornflower", effJumpBoost, 5},
	{"wither_rose", effWither, 7},
	{"lily_of_the_valley", effPoison, 11},
	{"open_eyeblossom", effBlindness, 11},
	{"closed_eyeblossom", effNausea, 7},
}

var (
	itemSuspiciousStew = int32(itemByName["suspicious_stew"])
	itemRedMushroom    = int32(itemByName["red_mushroom"])
	itemBrownMushroom  = int32(itemByName["brown_mushroom"])
	stewFlowerIndex    = func() map[int32]int8 {
		m := map[int32]int8{}
		for i, e := range stewEffects {
			m[int32(itemByName[e.flower])] = int8(i + 1)
		}
		return m
	}()
)

// stewIndexFor is SuspiciousEffectHolder.tryGet: the flower's stew code, 0
// when the item is no stew flower.
func stewIndexFor(item int32) int8 { return stewFlowerIndex[item] }

// stewCraftMatch is SuspiciousStewRecipe.matches: exactly a bowl, a red
// mushroom, a brown mushroom and one stew flower, in any cells.
func stewCraftMatch(grid []invStack) (invStack, bool) {
	var bowl, red, brown int
	var flower int8
	n := 0
	for _, s := range grid {
		if s.item == 0 || s.count <= 0 {
			continue
		}
		n++
		switch {
		case s.item == itemBowlEmpty:
			bowl++
		case s.item == itemRedMushroom:
			red++
		case s.item == itemBrownMushroom:
			brown++
		case stewIndexFor(s.item) != 0 && flower == 0:
			flower = stewIndexFor(s.item)
		default:
			return invStack{}, false
		}
	}
	if n != 4 || bowl != 1 || red != 1 || brown != 1 || flower == 0 {
		return invStack{}, false
	}
	return invStack{item: itemSuspiciousStew, count: 1, stew: flower}, true
}

// eatStew applies a suspicious stew's hidden effect.
func (h *hub) eatStew(players map[int32]*tracked, t *tracked, stew int8) {
	if stew <= 0 || int(stew) > len(stewEffects) {
		return
	}
	e := stewEffects[stew-1]
	secs := int(e.secs)
	if secs < 1 {
		secs = 1 // saturation's 0.35 s: the grant is instant, the tail rounds up
	}
	h.applyEffect(players, t, e.effect, 0, secs)
}

// tryFlowerMooshroom feeds a brown mooshroom a stew flower: it remembers
// the effect for its next bowl (one flower at a time; a full one just
// smokes in vanilla and takes nothing).
func (h *hub) tryFlowerMooshroom(players map[int32]*tracked, t *tracked, m *mob) bool {
	if m.etype != entityMooshroom || m.variant != mooshroomBrown || m.baby {
		return false
	}
	idx := stewIndexFor(heldStack(t).item)
	if idx == 0 {
		return false
	}
	if m.stew == 0 {
		if t.gamemode == gmSurvival {
			h.consumeHeld(t)
		}
		m.stew = idx
		h.playSound(players, "minecraft:entity.mooshroom.eat", sndNeutral, m.x, m.y, m.z, 2, 1)
	}
	return true
}
