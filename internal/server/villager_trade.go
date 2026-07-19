package server

import "math"

// Villager trade leveling — reimplemented from the vanilla 1.21.5 Villager /
// VillagerData. A villager starts at tier 1 (novice) offering a couple of its
// profession's tier-1 trades; using its trades earns it XP, and crossing the
// tier thresholds promotes it (up to master) and unlocks a couple more trades
// from the new tier. The full offer table lives in villager_trades_gen.go.

// mobOffer is one active merchant offer: the generated trade plus its live
// economy state — how many times it's been used this restock (locked once uses
// reaches maxUses), the accumulated demand (grows with heavy use, raises the
// price), and the per-viewer special-price delta (reputation + Hero of the
// Village discounts, recomputed when a player opens the merchant screen).
type mobOffer struct {
	trade        vTrade
	uses         int32
	demand       int32 // vanilla MerchantOffer.demand; persisted
	specialPrice int32 // vanilla specialPriceDiff; per-viewer, not persisted
}

// tradePriceMultiplier is vanilla MerchantOffer.priceMultiplier. Vanilla varies
// it per listing (0.05 for most emerald-cost trades, 0.2 for a few); we apply
// the common 0.05 to every offer — the wire packet carries it so the client's
// displayed price matches what costCount charges.
const tradePriceMultiplier = 0.05

// costCount is vanilla MerchantOffer.getModifiedCostCount: the base input count
// plus the demand markup, plus the special-price delta, clamped to [1, stack].
func (o *mobOffer) costCount() int {
	base := int(o.trade.inCount)
	bump := int(math.Floor(float64(base) * float64(o.demand) * tradePriceMultiplier))
	if bump < 0 {
		bump = 0
	}
	c := base + bump + int(o.specialPrice)
	if c < 1 {
		c = 1
	}
	if maxStack := stackCap(o.trade.inItem); c > maxStack {
		c = maxStack
	}
	return c
}

// tierMinXP is the trade XP a villager needs to REACH each tier (index = tier).
// Vanilla VillagerData.NEXT_LEVEL_XP: 0,10,70,150,250 for tiers 1..5.
var tierMinXP = [6]int{0, 0, 10, 70, 150, 250}

const maxTradeTier = 5

// offersPerTier is how many new trades unlock when a villager reaches a tier
// (vanilla adds up to 2 random trades per level).
const offersPerTier = 2

// initVillagerTrades sets a fresh villager's profession + tier-1 offers.
func (h *hub) initVillagerTrades(m *mob, profession int) {
	m.profession = profession % len(professionNames)
	m.tradeLevel = 1
	m.tradeXP = 0
	m.offers = nil
	h.unlockTier(m, 1)
}

// unlockTier appends up to offersPerTier trades from the villager's profession
// at the given tier (deterministic pick keyed off eid so it's stable per mob).
func (h *hub) unlockTier(m *mob, tier int) {
	pool := villagerTrades[m.profession][tier]
	if len(pool) == 0 {
		return
	}
	start := int(m.eid) % len(pool) // stable per-villager rotation
	for i := 0; i < offersPerTier && i < len(pool); i++ {
		m.offers = append(m.offers, mobOffer{trade: pool[(start+i)%len(pool)]})
	}
}

// awardTradeXP credits a completed trade and promotes the villager across any
// tier thresholds it crosses, unlocking the new tier's trades.
func (h *hub) awardTradeXP(m *mob, xp int32) {
	m.tradeXP += int(xp)
	for m.tradeLevel < maxTradeTier && m.tradeXP >= tierMinXP[m.tradeLevel+1] {
		m.tradeLevel++
		h.unlockTier(m, m.tradeLevel)
	}
}

// restockInterval is vanilla's minimum spacing between a villager's restocks
// (Villager.allowedToRestock: gameTime > lastRestock + 2400).
const restockInterval = 2400

// allowedToRestock is vanilla Villager.allowedToRestock: the first restock of
// the day is free, then at most one more, and only ≥2400 ticks after the last.
func (h *hub) allowedToRestock(m *mob) bool {
	if m.restocksToday == 0 {
		return true
	}
	return m.restocksToday < 2 && h.tick.Load() > m.lastRestockTick+restockInterval
}

// needsRestock is vanilla Villager.needsToRestock: any offer has been used.
func needsRestock(m *mob) bool {
	for i := range m.offers {
		if m.offers[i].uses > 0 {
			return true
		}
	}
	return false
}

// restockOffers refreshes a villager's trades — vanilla Villager.restock: bump
// each offer's demand from its usage, then reset uses to 0. Gated by
// allowedToRestock (≤2/day, ≥2400 ticks apart), so callers may invoke it freely.
func (h *hub) restockOffers(m *mob) {
	if !h.allowedToRestock(m) {
		return
	}
	for i := range m.offers {
		o := &m.offers[i]
		// vanilla MerchantOffer.updateDemand — heavy use raises demand, idle
		// days lower it (not clamped here; costCount floors the markup at 0).
		o.demand = o.demand + o.uses - (o.trade.maxUses - o.uses)
		o.uses = 0
	}
	m.restocksToday++
	m.lastRestockTick = h.tick.Load()
}
