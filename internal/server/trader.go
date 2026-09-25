package server

import "math"

// The wandering trader (WanderingTraderSpawner, WanderingTrader, and the
// wandering_trader trade sets): every twenty minutes the world
// rolls for one — a quarter chance the first time, rising by a quarter
// each miss to three quarters — and one time in ten it appears within
// forty-eight blocks of a random player with two leashed trader llamas,
// nine trades drawn from vanilla's three pools (two it buys, two rare
// wares, five common ones), and forty minutes to live. At night it drinks
// invisibility; at dawn, milk.

const (
	traderTickDelay    = 1200  // DEFAULT_TICK_DELAY
	traderSpawnDelay   = 24000 // DEFAULT_SPAWN_DELAY
	traderChanceMin    = 25
	traderChanceMax    = 75
	traderChanceStep   = 25
	traderOneIn        = 10 // SPAWN_ONE_IN_X_CHANCE
	traderSpawnTries   = 10 // NUMBER_OF_SPAWN_ATTEMPTS
	traderSpawnRange   = 48
	traderLlamaRange   = 4
	traderDespawnTicks = 48000
	traderDrinkTicks   = 32 // a potion's / a milk bucket's use duration
	traderInvisSecs    = 180
	traderDrinkPotion  = 1
	traderDrinkMilk    = 2
)

// rollTraderOffers is WanderingTrader.updateTrades: add each of its
// trade_sets (wandering_trader/buying, uncommon, common — traderTradeSets)
// in turn, each drawing its amount of offers.
func (h *hub) rollTraderOffers(m *mob) {
	m.offers = nil
	for _, set := range traderTradeSets {
		h.addOffersFromTradeSet(m, set.trades, set.amount)
	}
}

// traderSpawnerTick is WanderingTraderSpawner.tick, run every 1200 ticks.
func (h *hub) traderSpawnerTick(players map[int32]*tracked) {
	if !h.rules.DoTraderSpawning {
		return
	}
	if h.rules.TraderSpawnDelay == 0 && h.rules.TraderSpawnChance == 0 {
		h.rules.TraderSpawnDelay, h.rules.TraderSpawnChance = traderSpawnDelay, traderChanceMin
	}
	h.rules.TraderSpawnDelay -= traderTickDelay
	if h.rules.TraderSpawnDelay > 0 {
		h.saveRules()
		return
	}
	h.rules.TraderSpawnDelay = traderSpawnDelay
	if !h.rules.DoMobSpawning {
		h.saveRules()
		return
	}
	n := h.rules.TraderSpawnChance
	h.rules.TraderSpawnChance = min(traderChanceMax, max(traderChanceMin, h.rules.TraderSpawnChance+traderChanceStep))
	if h.rng.Intn(100) <= n && h.traderSpawn(players) {
		h.rules.TraderSpawnChance = traderChanceMin
	}
	h.saveRules()
}

// traderSpawn is WanderingTraderSpawner.spawn: near a random player, one
// time in ten, with two leashed llamas.
func (h *hub) traderSpawn(players map[int32]*tracked) bool {
	t := h.randomPlayer(players)
	if t == nil {
		return true
	}
	if h.rng.Intn(traderOneIn) != 0 {
		return false
	}
	if t.dim != 0 {
		return false
	}
	x, z, ok := h.traderSpawnPosNear(int(math.Floor(t.x)), int(math.Floor(t.z)), traderSpawnRange)
	if !ok {
		return false
	}
	m := h.spawnSpecies(players, entityWanderingTrader, 0, float64(x)+0.5, float64(h.world.SurfaceY(x, z)), float64(z)+0.5)
	if m == nil {
		return false
	}
	h.rollTraderOffers(m)
	m.traderDespawn = traderDespawnTicks
	for i := 0; i < 2; i++ {
		if lx, lz, ok := h.traderSpawnPosNear(x, z, traderLlamaRange); ok {
			if l := h.spawnSpecies(players, entityTraderLlama, 0, float64(lx)+0.5, float64(h.world.SurfaceY(lx, lz)), float64(lz)+0.5); l != nil {
				l.traderDespawn = traderDespawnTicks
				h.setLeash(players, l, m.eid)
			}
		}
	}
	return true
}

// traderSpawnPosNear is findSpawnPositionNear: ten tries at a random
// column within n, standing room and all.
func (h *hub) traderSpawnPosNear(x, z, n int) (int, int, bool) {
	for i := 0; i < traderSpawnTries; i++ {
		nx := x + h.rng.Intn(2*n+1) - n
		nz := z + h.rng.Intn(2*n+1) - n
		if h.world.Spawnable(nx, nz) {
			return nx, nz, true
		}
	}
	return 0, 0, false
}

// traderLlamaKept is TraderLlama.canDespawn turned round: a llama that has
// been tamed, kept by name, or led off on someone else's lead is no longer
// the trader's and stays.
func (h *hub) traderLlamaKept(m *mob) bool {
	if m.etype != entityTraderLlama {
		return false
	}
	if m.tamed || m.persistent || m.rider != 0 {
		return true
	}
	if m.leash != 0 {
		if l := h.mobs[m.leash]; l == nil || l.etype != entityWanderingTrader {
			return true
		}
	}
	return false
}

// traderStep runs each mob update for a trader (and its llamas): the
// despawn clock, and the potion or milk by the time of day.
func (h *hub) traderStep(players map[int32]*tracked, m *mob) bool {
	// WanderingTrader.maybeDespawn holds the clock while a player has the
	// trade screen open, so a trader cannot vanish mid-transaction.
	if m.traderDespawn > 0 && h.tradingPartner(players, m) == nil && !h.traderLlamaKept(m) {
		m.traderDespawn -= mobMoveInterval
		if m.traderDespawn <= 0 {
			h.despawnMob(players, m)
			return true
		}
	}
	if m.etype != entityWanderingTrader {
		return false
	}
	if m.drinkTicks > 0 {
		m.drinkTicks -= mobMoveInterval
		m.vx, m.vz = 0, 0
		if m.drinkTicks > 0 {
			return true
		}
		m.held = 0
		h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, invStack{}, invStack{}, m.gear))
		switch m.traderDrink {
		case traderDrinkPotion:
			h.applyMobEffect(players, m, effInvisibility, 0, traderInvisSecs)
			h.playSoundDim(players, m.dim, "minecraft:entity.wandering_trader.disappeared", sndNeutral, m.x, m.y, m.z, 1, 0.9+h.rng.Float32()*0.2)
		case traderDrinkMilk:
			h.removeMobEffect(players, m, effInvisibility)
			h.playSoundDim(players, m.dim, "minecraft:entity.wandering_trader.reappeared", sndNeutral, m.x, m.y, m.z, 1, 0.9+h.rng.Float32()*0.2)
		}
		m.traderDrink = 0
		return true
	}
	invisible := m.hasEffect(effInvisibility) > 0
	switch {
	case !h.isDayTime() && !invisible: // isDarkOutside: the invisibility potion
		m.traderDrink, m.drinkTicks, m.held = traderDrinkPotion, traderDrinkTicks, itemPotion
		h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: itemPotion, count: 1, potion: potInvisibility}, invStack{}, m.gear))
		return true
	case h.isDayTime() && invisible: // isBrightOutside: the milk
		m.traderDrink, m.drinkTicks, m.held = traderDrinkMilk, traderDrinkTicks, int32(itemByName["milk_bucket"])
		h.toTracking(players, m.eid, m.dim, m.x, m.z, equipEv(m.eid, invStack{item: m.held, count: 1}, invStack{}, m.gear))
		return true
	}
	return false
}
