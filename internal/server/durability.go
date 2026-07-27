package server

import "math"

// Durability + armor. Tools wear one point per mined block (hardness > 0) and
// per melee hit, breaking (vanishing) at their items.json max. Armor absorbs
// mob damage with the vanilla 1.9+ formula — reduction from total defense
// points, softened by how hard the hit is, capped at 80% — and every hit wears
// each worn piece. The damage value rides the minecraft:damage component so
// every client renders the normal durability bar.

// evToolWear: a survival player finished mining a real block — wear the tool
// that did it (the connection knows the held slot; the hub owns the stack).
type evToolWear struct {
	eid  int32
	slot int
}

func (evToolWear) isHubEvent() {}

// applyToolWear adds wear to a hotbar stack, destroying it when it runs out.
func (h *hub) applyToolWear(t *tracked, slot, n int) {
	if t.gamemode != gmSurvival || t.dead || t.inv == nil || slot < 0 || slot >= 9 {
		return
	}
	s := &t.inv.slots[slot]
	max, ok := itemMaxDurability[s.item]
	if !ok || s.count == 0 {
		return
	}
	if lvl := s.enchLvl(enchUnbreaking); lvl > 0 && h.rng.Intn(lvl+1) > 0 {
		return // unbreaking ate the wear (lvl/(lvl+1) chance, vanilla)
	}
	if s.dmg += n; s.dmg >= max {
		*s = invStack{} // the tool breaks
	}
	h.sendSlot(t, slot)
}

// armorReduce applies the vanilla armor formula to incoming damage:
//
//	damage * (1 - min(20, max(points/5, points - damage/(2+toughness/4))) / 25)
func (t *tracked) armorReduce(dmg float32) float32 {
	t.refreshArmorAttrs() // pick up a piece equipped since the last tick
	points, tough := t.armorPoints(), t.armorToughness()
	d := float64(dmg)
	if points > 0 {
		def := math.Min(20, math.Max(points/5, points-d/(2+tough/4)))
		d *= 1 - def/25
	}
	// The protection ENCHANTMENTS used to be folded in here. They are a
	// separate step in vanilla (armour absorption then magic absorption) and
	// they now run centrally in damageOf, which also reaches the damage that
	// skips armour but not protection — falling being the obvious one.
	return float32(d)
}

// wearArmor wears every equipped piece after a hit (vanilla: max(1, damage/4)
// durability each), destroying pieces that run out. The armor slots are only
// visible in window 0, so resync is skipped while a container is open (the
// next full window refresh covers it).
func (h *hub) wearArmor(players map[int32]*tracked, t *tracked, dmg float32) {
	n := int(dmg) / 4
	if n < 1 {
		n = 1
	}
	for i := range t.armor {
		a := &t.armor[i]
		if a.count == 0 {
			continue
		}
		max, ok := itemMaxDurability[a.item]
		if !ok {
			continue
		}
		if lvl := a.enchLvl(enchUnbreaking); lvl > 0 && h.rng.Intn(lvl+1) > 0 {
			continue // unbreaking spared this piece
		}
		if a.dmg += n; a.dmg >= max {
			*a = invStack{} // the piece shatters
		}
		if t.winID == 0 {
			h.sendWinSlot(t, int16(5+i), *a)
		}
	}
	h.broadcastEquipment(players, t) // shattered pieces vanish for onlookers too
}

// wearArmorSlot wears ONE armour piece by a fixed amount — what Thorns costs
// the piece that retaliated (vanilla ChangeItemDamage(2) on that slot alone,
// not on the whole set).
func (h *hub) wearArmorSlot(players map[int32]*tracked, t *tracked, slot, n int) {
	a := &t.armor[slot]
	if a.count == 0 {
		return
	}
	max, ok := itemMaxDurability[a.item]
	if !ok {
		return
	}
	if lvl := a.enchLvl(enchUnbreaking); lvl > 0 && h.rng.Intn(lvl+1) > 0 {
		return
	}
	if a.dmg += n; a.dmg >= max {
		*a = invStack{}
	}
	if t.winID == 0 {
		h.sendWinSlot(t, int16(5+slot), *a)
	}
	h.broadcastEquipment(players, t)
}

// mendingRepair spends experience on a damaged Mending item before it ever
// reaches the player's bar, and returns the experience left over.
//
// Mending was in the treasure pools, the fishing pools and the anvil's tables
// and was wired to nothing — the single most valuable enchantment in the game
// did not repair anything. Vanilla picks ONE damaged item carrying it at
// random from the held slots and the armour, mends two points per experience
// point, and recurses with whatever is left so a single orb can finish one
// item and start on the next.
func (h *hub) mendingRepair(t *tracked, xp int) int {
	if t.inv == nil {
		return xp
	}
	for xp > 0 {
		s := h.pickMendable(t)
		if s == nil {
			return xp
		}
		repair := min(xp*mendingPerPoint, s.dmg)
		s.dmg -= repair
		// Experience is spent in proportion to what it actually mended, so a
		// nearly-repaired item does not swallow a whole orb.
		spent := (repair + mendingPerPoint - 1) / mendingPerPoint
		if spent <= 0 {
			return xp
		}
		xp -= spent
	}
	return xp
}

// mendingPerPoint is vanilla's exchange rate: two durability per experience.
const mendingPerPoint = 2

// pickMendable chooses a damaged Mending item at random from the held slots
// and the worn armour, as vanilla's getRandomItemWith does.
func (h *hub) pickMendable(t *tracked) *invStack {
	var found []*invStack
	for i := range t.inv.slots[:9] {
		if s := &t.inv.slots[i]; s.count > 0 && s.dmg > 0 && s.enchLvl(enchMending) > 0 {
			found = append(found, s)
		}
	}
	for i := range t.armor {
		if s := &t.armor[i]; s.count > 0 && s.dmg > 0 && s.enchLvl(enchMending) > 0 {
			found = append(found, s)
		}
	}
	if len(found) == 0 {
		return nil
	}
	return found[h.rng.Intn(len(found))]
}
