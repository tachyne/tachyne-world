package server

import attachproto "github.com/tachyne/tachyne-common/attach"

// Milking, and the drink that comes of it.
//
// A bucket and a cow is about the most basic thing a survival player expects
// to work, and there was no `milk_bucket` path anywhere in the engine — the
// item id existed and nothing produced it or consumed it.
//
// Vanilla hangs milking off the cow base class (so cows and mooshrooms share
// it) and repeats it on the goat, gated on the animal being an adult. Milk
// itself is a plain drink with one job: it strips every status effect, which
// makes it the standard answer to a witch's poison.

var (
	itemMilkBucket   = itemByName["milk_bucket"]
	itemBowlEmpty    = itemByName["bowl"]
	itemMushroomStew = itemByName["mushroom_stew"]
)

// milkSound is the noise each milkable animal makes. Vanilla gives the goat a
// second, louder variant for its screaming form, which tachyne has no state
// for — plain goats only.
func milkSound(etype int) string {
	switch etype {
	case entityMooshroom:
		return "minecraft:entity.mooshroom.milk"
	case entityGoat:
		return "minecraft:entity.goat.milk"
	}
	return "minecraft:entity.cow.milk"
}

// tryMilk fills a held bucket from an adult cow, mooshroom or goat.
func (h *hub) tryMilk(players map[int32]*tracked, t *tracked, m *mob) bool {
	if heldStack(t).item != itemBucket || m.baby {
		return false
	}
	switch m.etype {
	case entityCow, entityMooshroom, entityGoat:
	default:
		return false
	}
	h.playSound(players, milkSound(m.etype), sndNeutral, m.x, m.y, m.z, 1, 1)
	h.giveFilled(players, t, int32(t.p.heldSlot()), itemMilkBucket)
	return true
}

// tryMilkStew is the mooshroom's other half: a bowl comes back as stew.
// Vanilla's brown mooshrooms can additionally be fed a flower to brew a
// SUSPICIOUS stew, which needs both per-mob stored effects and the stew's
// effect component carried through the render chain — neither exists yet, so
// every mooshroom gives the plain stew.
func (h *hub) tryMilkStew(players map[int32]*tracked, t *tracked, m *mob) bool {
	if heldStack(t).item != itemBowlEmpty || m.etype != entityMooshroom || m.baby {
		return false
	}
	h.playSound(players, "minecraft:entity.mooshroom.milk", sndNeutral, m.x, m.y, m.z, 1, 1)
	h.giveFilled(players, t, int32(t.p.heldSlot()), itemMushroomStew)
	return true
}

// drinkMilk empties a milk bucket: every status effect goes, and the player is
// left holding the bucket. Unlike food it can be drunk on a full stomach —
// which is the whole point of carrying it.
func (h *hub) drinkMilk(t *tracked, slot int) {
	s := &t.inv.slots[slot]
	if s.item != itemMilkBucket || s.count == 0 {
		return
	}
	h.clearEffects(t)
	h.incStat(t, attachproto.StatUsed, s.item, 1)
	if t.gamemode != gmCreative {
		s.item, s.count = itemBucket, 1
		h.sendSlot(t, slot)
	}
	t.p.trySendEv(soundEv("minecraft:entity.player.burp", sndPlayer, t.x, t.y, t.z, 1, 1))
}
