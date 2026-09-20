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
	// The optional second ItemCost (a librarian's book beside the emeralds)
	// and the enchantments on the output — one for an enchanted book, several
	// for a piece of EnchantedItemForEmeralds gear. Both persist; the second
	// cost takes no demand or reputation adjustment (vanilla adjusts costA
	// only).
	cost2Item, cost2Count int32
	outEnchs              enchList
	// A treasure map's output: which map the stack shows and the name it
	// carries ("Ocean Explorer Map"). Both persist — the map is located once,
	// when the offer is rolled.
	outMapID int32
	outName  string
	// A rolled output's extra component: the blended dye on a leatherworker's
	// armour, the effect hidden in a farmer's stew, the potion a fletcher's
	// arrows are tipped with. All persist — every roll happens once.
	outColor  int32
	outStew   int8
	outPotion int8
}

// output is the stack an offer hands over.
func (o *mobOffer) output() invStack {
	st := invStack{item: o.trade.outItem, count: int(o.trade.outCount)}
	st.ench = o.outEnchs
	st.mapID, st.name = o.outMapID, o.outName
	st.color, st.stew, st.potion = o.outColor, o.outStew, o.outPotion
	return st
}

// offerFrom starts a live offer from a generated row. The row's listing-only
// columns are consumed here — the second item cost moves into the offer's own
// fields, and kind/aux are cleared — so a resolved offer carries its result
// and no longer remembers which listing produced it. That is what makes a
// rolled offer stable: nothing downstream can roll it again.
func offerFrom(t vTrade) mobOffer {
	o := mobOffer{trade: t, cost2Item: t.c2Item, cost2Count: t.c2Count}
	o.trade.kind, o.trade.aux, o.trade.c2Item, o.trade.c2Count = vTradeFixed, 0, 0, 0
	return o
}

// librarianProfession is the librarian's index (the tests name it).
var librarianProfession = func() int {
	for i, n := range professionNames {
		if n == "librarian" {
			return i
		}
	}
	return -1
}()

// rollBookOffer is VillagerTrades.EnchantBookForEmeralds.getOffer: a random
// #tradeable enchantment at a level drawn uniformly from its own range; the
// price is 2 + nextInt(5 + 10·level) + 3·level emeralds, doubled for a
// double-price enchantment and capped at 64, plus one book. With no tradeable
// enchantment at all vanilla sells a plain book for one emerald.
func (h *hub) rollBookOffer(t vTrade) mobOffer {
	o := offerFrom(t)
	var pool []int8
	for i := range enchDefs {
		if enchTradeAllowed(int8(i)) {
			pool = append(pool, int8(i))
		}
	}
	if len(pool) == 0 {
		o.trade.inCount, o.trade.outItem = 1, itemByName["book"]
		return o
	}
	id := pool[h.rng.Intn(len(pool))]
	lvl := 1 + h.rng.Intn(enchDefs[id].maxLevel)
	price := 2 + h.rng.Intn(5+lvl*10) + 3*lvl
	if enchDefs[id].flags&enchDoubleTradePrice != 0 {
		price *= 2
	}
	if price > 64 {
		price = 64
	}
	o.trade.inCount = int32(price)
	o.outEnchs = enchList{{id: id, lvl: int8(lvl)}}
	return o
}

// enchGearAllowed is EnchantmentTags.ON_TRADED_EQUIPMENT — the set a villager
// may put on the gear it sells.
func enchGearAllowed(id int8) bool { return enchDefs[id].flags&enchOnTradedEquipment != 0 }

// rollGearOffer is VillagerTrades.EnchantedItemForEmeralds.getOffer: the
// villager sells one piece of equipment enchanted as if at level 5..19, and the
// same roll tops its price up from the listing's base cost (capped at 64
// emeralds). The enchantments are rolled ONCE, when the tier unlocks, and
// persist with the offer — restocking never re-rolls them.
func (h *hub) rollGearOffer(t vTrade) mobOffer {
	lvl := 5 + h.rng.Intn(15)
	o := offerFrom(t)
	if cost := int(t.inCount) + lvl; cost < 64 {
		o.trade.inCount = int32(cost)
	} else {
		o.trade.inCount = 64
	}
	o.outEnchs = enchApplyList(enchSelect(h.rng, t.outItem, lvl, enchGearAllowed))
	return o
}

// explorerMapSearch is the radius vanilla's TreasureMapForEmeralds searches —
// findNearestMapStructure(..., 100, true), 100 chunks — in blocks.
const explorerMapSearch = 100 * 16

// rollMapOffer is VillagerTrades.TreasureMapForEmeralds.getOffer: find the
// nearest site of the listing's structure to the villager, draw a map centred
// on it, mark it, and name it. The villager sells it for emeralds plus a
// compass. ok=false when the listing is not for this villager's type or
// nothing is within reach — vanilla returns no offer in both cases, and the
// caller draws another listing instead.
func (h *hub) rollMapOffer(m *mob, t vTrade) (mobOffer, bool) {
	if t.aux <= 0 || int(t.aux) > len(villagerMapListings) {
		return mobOffer{}, false
	}
	l := villagerMapListings[t.aux-1]
	if l.forTypes != 0 && l.forTypes&(1<<uint(h.villagerType(m))) == 0 {
		return mobOffer{}, false
	}
	w := h.worldFor(m.dim)
	if w == nil || h.maps == nil {
		return mobOffer{}, false
	}
	x, z, ok := w.Gen().LocateStructure(l.dest, floorInt(m.x), floorInt(m.z), explorerMapSearch)
	if !ok {
		return mobOffer{}, false
	}
	md := h.maps.create(x, z, 2, m.dim)
	md.Marks = append(md.Marks, mapMark{X: int32(x), Z: int32(z), Type: l.decor})
	h.maps.markDirty()
	o := offerFrom(t)
	o.outMapID, o.outName = md.ID, l.label
	return o, true
}

// rollDyedOffer is VillagerTrades.DyedArmorForEmeralds.getOffer: one leather
// piece dyed with a random dye, a second 30% of the time and a third 20% of
// that, blended the way a crafting grid blends them.
func (h *hub) rollDyedOffer(t vTrade) mobOffer {
	o := offerFrom(t)
	dyes := []int32{randomDyeRGB(h.rng)}
	if h.rng.Float32() > 0.7 {
		dyes = append(dyes, randomDyeRGB(h.rng))
	}
	if h.rng.Float32() > 0.8 {
		dyes = append(dyes, randomDyeRGB(h.rng))
	}
	o.outColor = blendDyes(0, dyes)
	return o
}

// randomDyeRGB is DyeItem.byColor(DyeColor.byId(nextInt(16))) as a colour.
func randomDyeRGB(r enchRand) int32 {
	return dyeRGB[int32(itemByName[dyeOrder[r.Intn(len(dyeOrder))]+"_dye"])]
}

// stewCodeFor resolves one villagerStewListings row to a suspicious-stew table
// code. Vanilla stores the effect and duration on the stack directly; tachyne
// stores an index into stewEffects, so a trade's pair has to appear there —
// TestFarmerSellsSuspiciousStew fails loudly if one does not.
func stewCodeFor(effect string, ticks int) (int8, bool) {
	id, ok := effectNames[effect]
	if !ok {
		return 0, false
	}
	for i, e := range stewEffects {
		if e.effect == id && int(e.secs*20+0.5) == ticks {
			return int8(i + 1), true
		}
	}
	return 0, false
}

// rollStewOffer is VillagerTrades.SuspiciousStewForEmerald.getOffer: one
// emerald for a bowl of stew hiding the listing's effect. Nothing is random
// about it — the effect is fixed per listing — but it resolves like the other
// rolled kinds so the stored offer carries the stew, not the listing.
func (h *hub) rollStewOffer(t vTrade) (mobOffer, bool) {
	if t.aux <= 0 || int(t.aux) > len(villagerStewListings) {
		return mobOffer{}, false
	}
	l := villagerStewListings[t.aux-1]
	code, ok := stewCodeFor(l.effect, l.ticks)
	if !ok {
		return mobOffer{}, false
	}
	o := offerFrom(t)
	o.outStew = code
	return o, true
}

// rollArrowOffer is VillagerTrades.TippedArrowForItemsAndEmeralds.getOffer:
// emeralds and plain arrows for the same number tipped with a potion drawn at
// random from everything the brewing stand can make.
func (h *hub) rollArrowOffer(t vTrade) (mobOffer, bool) {
	pool := brewablePotions()
	if len(pool) == 0 {
		return mobOffer{}, false
	}
	o := offerFrom(t)
	o.outPotion = pool[h.rng.Intn(len(pool))]
	return o, true
}

// brewablePotions is the set vanilla draws the tipped arrow from: every potion
// with effects that the brewing recipes can actually produce, in a stable
// order. That excludes Luck (no recipe brews it) and the effectless bases, the
// same two exclusions vanilla's isBrewablePotion + getEffects filter makes.
// Recomputed per call: brewMixes is filled in an init, so a package-level
// initializer here would read it empty.
func brewablePotions() []int8 {
	seen := map[int8]bool{}
	var out []int8
	for _, mx := range brewMixes {
		if seen[mx.to] || len(potionDefs[mx.to].effects) == 0 {
			continue
		}
		seen[mx.to] = true
		out = append(out, mx.to)
	}
	return out
}

// typeItemOffer is VillagerTrades.EmeraldsForVillagerTypeItem.getOffer: the
// villager buys the item its own birth biome calls for (a fisherman's boat).
func (h *hub) typeItemOffer(m *mob, t vTrade) (mobOffer, bool) {
	if t.aux <= 0 || int(t.aux) > len(villagerTypeItems) {
		return mobOffer{}, false
	}
	row := villagerTypeItems[t.aux-1]
	typ := int(h.villagerType(m))
	if typ < 0 || typ >= len(row) || row[typ] == 0 {
		return mobOffer{}, false
	}
	o := offerFrom(t)
	o.trade.inItem = row[typ]
	return o, true
}

// defaultPriceMult100 is the priceMultiplier most listings carry (0.05), in
// hundredths. It stands in for an offer stored before the multiplier was
// per-listing, when every offer used this value.
const defaultPriceMult100 = 5

// priceMult is vanilla MerchantOffer.priceMultiplier for this offer: how hard
// demand pushes the price up and how far reputation pulls it down. The wire
// packet carries it too, so the client's displayed price matches what
// costCount charges.
func (o *mobOffer) priceMult() float64 {
	m := o.trade.mult100
	if m == 0 {
		m = defaultPriceMult100
	}
	return float64(m) / 100
}

// costCount is vanilla MerchantOffer.getModifiedCostCount: the base input count
// plus the demand markup, plus the special-price delta, clamped to [1, stack].
func (o *mobOffer) costCount() int {
	base := int(o.trade.inCount)
	bump := int(math.Floor(float64(base*int(o.demand)) * o.priceMult()))
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
	// Villager.addOffersFromItemListings: draw listings at random WITHOUT
	// replacement until the tier has offersPerTier OFFERS. Two things follow
	// from "offers", not "listings": a listing that yields none (a treasure
	// map for another villager type, or one whose structure is out of reach)
	// is skipped rather than costing a slot, and the librarian's enchanted
	// book competes for a slot like everything else instead of being extra.
	added := 0
	for _, i := range h.rng.Perm(len(pool)) {
		if added >= offersPerTier {
			break
		}
		o, ok := h.resolveOffer(m, pool[i])
		if !ok {
			continue
		}
		m.offers = append(m.offers, o)
		added++
	}
}

// resolveOffer turns a generated listing into a live offer, rolling whatever
// its kind rolls. ok=false is vanilla's null offer: the listing yields nothing
// for this villager (wrong type, structure out of reach), so the caller draws
// another one.
func (h *hub) resolveOffer(m *mob, t vTrade) (mobOffer, bool) {
	switch t.kind {
	case vTradeEnchantedGear:
		return h.rollGearOffer(t), true
	case vTradeDyedArmor:
		return h.rollDyedOffer(t), true
	case vTradeTreasureMap:
		return h.rollMapOffer(m, t)
	case vTradeStew:
		return h.rollStewOffer(t)
	case vTradeTippedArrow:
		return h.rollArrowOffer(t)
	case vTradeTypeItem:
		return h.typeItemOffer(m, t)
	case vTradeEnchantedBook:
		return h.rollBookOffer(t), true
	}
	return offerFrom(t), true
}

// awardTradeXP is Villager.rewardTradeXp's bookkeeping half: credit the trade
// and, if that crossed the next tier's threshold, ARM the level-up rather than
// applying it. Vanilla waits forty ticks (Villager.updateMerchantTimer) before
// the villager actually gains the tier, which is what the pause and the
// sparkle after a promoting trade are. It returns whether the trade armed one,
// because that is also what adds five to the experience orb.
func (h *hub) awardTradeXP(m *mob, xp int32) bool {
	m.tradeXP += int(xp)
	if !shouldIncreaseLevel(m) {
		return false
	}
	m.merchantTimer = merchantUpdateDelay
	m.levelUpPending = true
	return true
}

// merchantUpdateDelay is vanilla's updateMerchantTimer, in ticks.
const merchantUpdateDelay = 40

// shouldIncreaseLevel is Villager.shouldIncreaseLevel — ONE tier at a time,
// however much experience a single trade paid.
func shouldIncreaseLevel(m *mob) bool {
	return m.tradeLevel < maxTradeTier && m.tradeXP >= tierMinXP[m.tradeLevel+1]
}

// villagerMerchantTick is the Villager.customServerAiStep branch that runs the
// armed level-up: count the delay down (never while the villager is mid-trade,
// which the caller guarantees), then take the tier and sparkle. Vanilla grants
// the Regeneration whenever the timer expires, promotion or not.
func (h *hub) villagerMerchantTick(players map[int32]*tracked, m *mob) {
	if m.merchantTimer <= 0 {
		return
	}
	if m.merchantTimer -= mobMoveInterval; m.merchantTimer > 0 {
		return
	}
	m.merchantTimer = 0
	if m.levelUpPending {
		m.levelUpPending = false
		m.tradeLevel++
		h.unlockTier(m, m.tradeLevel)
		h.sendVillagerData(players, m) // the new tier's badge
	}
	h.applyMobEffect(players, m, effRegen, 0, 10) // REGENERATION 200 ticks, amp 0
}

const (
	// restockInterval is vanilla's minimum spacing between a villager's
	// restocks (Villager.allowedToRestock: gameTime > lastRestock + 2400).
	restockInterval = 2400
	// restockDayInterval is how long since the last restock counts as a new
	// trading day (Villager.shouldRestock: lastRestock + 12000).
	restockDayInterval = 12000
	// restocksPerDay is vanilla's daily cap.
	restocksPerDay = 2
)

// allowedToRestock is vanilla Villager.allowedToRestock: the first restock of
// the day is free, then at most one more, and only ≥2400 ticks after the last.
func (h *hub) allowedToRestock(m *mob) bool {
	if m.restocksToday == 0 {
		return true
	}
	return m.restocksToday < restocksPerDay && h.tick.Load() > m.lastRestockTick+restockInterval
}

// shouldRestock is vanilla Villager.shouldRestock, run when the villager
// reaches its job site: once 12000 ticks have passed since its last restock,
// or the day count has turned over since it last looked, it catches up the
// restocks it never took and its daily counter goes back to zero. Then the
// ordinary gate — at most two a day, 2400 ticks apart — decides.
//
// This replaced "reset the counter on waking", which left a bedless villager
// (no bed claimed, or one whose bed was broken) capped at two restocks for the
// rest of the world's life.
func (h *hub) shouldRestock(m *mob) bool {
	now, day := h.tick.Load(), h.dayTime.Load()/dayLengthTicks
	newDay := now > m.lastRestockTick+restockDayInterval ||
		(m.lastRestockDay > 0 && day > m.lastRestockDay)
	m.lastRestockDay = day
	if newDay {
		m.lastRestockTick = now
		h.catchUpDemand(m)
		m.restocksToday = 0
	}
	return h.allowedToRestock(m) && needsRestock(m)
}

// catchUpDemand is Villager.catchUpDemand: a villager that took fewer than its
// two restocks yesterday gets the missed ones applied at once — its offers
// unlock and their demand ages once per restock it skipped.
func (h *hub) catchUpDemand(m *mob) {
	n := restocksPerDay - m.restocksToday
	if n <= 0 {
		return
	}
	for i := range m.offers {
		m.offers[i].uses = 0
	}
	for i := 0; i < n; i++ {
		updateDemand(m)
	}
}

// updateDemand is Villager.updateDemand over every offer: vanilla
// MerchantOffer.updateDemand, where heavy use raises the demand and an idle
// day lowers it (not clamped here; costCount floors the markup at 0).
func updateDemand(m *mob) {
	for i := range m.offers {
		o := &m.offers[i]
		o.demand = o.demand + o.uses - (o.trade.maxUses - o.uses)
	}
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
// each offer's demand from its usage, then unlock every offer. Still gated by
// allowedToRestock so the existing callers may invoke it freely; shouldRestock
// is the fuller gate that also rolls the day over.
func (h *hub) restockOffers(m *mob) {
	if !h.allowedToRestock(m) {
		return
	}
	updateDemand(m)
	for i := range m.offers {
		m.offers[i].uses = 0
	}
	m.restocksToday++
	m.lastRestockTick = h.tick.Load()
}

// tradingPartner is the player whose trade screen this villager is serving,
// if any — vanilla's tradingPlayer, which pins the villager in place and
// turns its head (LookAndFollowTradingPlayerSink).
func (h *hub) tradingPartner(players map[int32]*tracked, m *mob) *tracked {
	if m.etype != entityVillager && m.etype != entityWanderingTrader {
		return nil
	}
	for _, t := range players {
		if t.winKind == winTrade && t.tradeWith == m.eid {
			return t
		}
	}
	return nil
}
