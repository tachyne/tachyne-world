package server

// Villager trade leveling — reimplemented from the vanilla 1.21.5 Villager /
// VillagerData. A villager starts at tier 1 (novice) offering a couple of its
// profession's tier-1 trades; using its trades earns it XP, and crossing the
// tier thresholds promotes it (up to master) and unlocks a couple more trades
// from the new tier. The full offer table lives in villager_trades_gen.go.

// mobOffer is one active merchant offer: the generated trade plus how many times
// it's been used this restock (locked once uses reaches maxUses).
type mobOffer struct {
	trade vTrade
	uses  int32
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

// restockOffers refreshes a villager's trades (uses back to 0) — vanilla
// restocks at its workstation. Called on the daily wake so exhausted offers
// come back.
func (h *hub) restockOffers(m *mob) {
	for i := range m.offers {
		m.offers[i].uses = 0
	}
}
