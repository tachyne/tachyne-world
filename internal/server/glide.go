package server

// Elytra wear. The elytra carries the `glider` component, and vanilla makes
// the flight pay for itself out of the wing's own durability:
// LivingEntity.updateFallFlying counts the ticks of a glide and spends one
// point every twentieth of them — a point a second.
//
// The wing is never destroyed in the air. canGlideUsing refuses a stack whose
// NEXT damage would break it, so the last point is a floor: at durability 1 an
// elytra stops being a glider, the flight ends, and what is left is an
// unusable pair of wings waiting for a phantom membrane rather than nothing at
// all.

// glideWearPeriod is updateFallFlying's cadence: the tick counter is charged a
// point whenever it comes round to a multiple of twenty.
const glideWearPeriod = 20

// chestArmorSlot is where an elytra is worn, in the armour array and — offset
// by the five crafting slots, as everywhere else — in window 0.
const chestArmorSlot = 1

// elytraSpent is ItemStack.nextDamageWillBreak: one more point of wear would
// destroy this wing, which is exactly when vanilla stops gliding on it.
func elytraSpent(s invStack) bool {
	max, ok := itemMaxDurability[s.item]
	return ok && s.dmg >= max-1
}

// tickGliding is the server half of updateFallFlying, run for every player
// each tick: it ends a glide the wing can no longer carry and charges the wing
// for the time already flown.
func (h *hub) tickGliding(players map[int32]*tracked) {
	for _, t := range players {
		if !t.fallFlying {
			t.glideTicks = 0
			continue
		}
		if !t.gliding() {
			// canGlide() said no — landed, wing gone, or the wing down to its
			// last point. Vanilla clears FLAG_FALL_FLYING here, and that flag
			// is how the client learns to stop flying.
			t.fallFlying, t.glideTicks = false, 0
			h.broadcastPlayerFlags(players, t)
			continue
		}
		if t.glideTicks++; t.glideTicks%glideWearPeriod == 0 {
			h.wearElytra(players, t)
		}
	}
}

// wearElytra spends one point on the wing being flown. The elytra sits in
// #enchantable/durability but not in the armour branch of Unbreaking's rules,
// so it is spared at the tool rate — lvl/(lvl+1) — not the armour one. It can
// never break here: the glide ends a point before that.
func (h *hub) wearElytra(players map[int32]*tracked, t *tracked) {
	if !isSurvival(t.gamemode) {
		return // hasInfiniteMaterials: creative flies for free
	}
	a := &t.armor[chestArmorSlot]
	h.advance(players, t, "item_durability_changed", advMatch{item: a.item})
	if lvl := a.enchLvl(enchUnbreaking); lvl > 0 && h.rng.Intn(lvl+1) > 0 {
		return
	}
	a.dmg++
	// The armour slots are only visible in window 0, so a player with a
	// container open picks the change up on the next full refresh.
	if t.winID == 0 {
		h.sendWinSlot(t, int16(5+chestArmorSlot), *a)
	}
	h.broadcastEquipment(players, t) // …and the bar everyone else sees
}
