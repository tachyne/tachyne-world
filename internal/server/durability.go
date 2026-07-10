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
	points, tough := 0.0, 0.0
	for _, a := range t.armor {
		if a.count == 0 {
			continue
		}
		if p, ok := armorInfo[a.item]; ok {
			points += float64(p.Points)
			tough += p.Toughness
		}
	}
	d := float64(dmg)
	if points > 0 {
		def := math.Min(20, math.Max(points/5, points-d/(2+tough/4)))
		d *= 1 - def/25
	}
	// Protection: 4% less per EPF point (1/level/piece), capped at 80% (vanilla).
	epf := 0
	for _, a := range t.armor {
		epf += a.enchLvl(enchProtection)
	}
	if epf > 0 {
		d *= 1 - 0.04*float64(min(epf, 20))
	}
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
